package notification

import (
	"context"
	"database/sql"
	"errors"
	"time"

	domain "github.com/arisone/redcapital/internal/domain/notification"
)

const notificationColumns = "id, provider, event_type, payload, payload_hash, idempotency_key, status, attempts, next_attempt_at, lease_until, last_error, created_at, updated_at, delivered_at"

type Repository struct {
	db *sql.DB
}

func (r *Repository) Close() error {
	return r.db.Close()
}

func (r *Repository) CreateOrGet(ctx context.Context, n domain.Notification) (domain.Notification, bool, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Notification{}, false, err
	}
	defer tx.Rollback()

	res, err := tx.ExecContext(ctx,
		`INSERT OR IGNORE INTO notifications (`+notificationColumns+`)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, NULL, NULL, ?, ?, ?, NULL)`,
		n.ID, n.Provider, n.EventType, n.Payload, n.PayloadHash, n.IdempotencyKey,
		n.Status, n.Attempts, n.LastError, formatTime(n.CreatedAt), formatTime(n.UpdatedAt))
	if err != nil {
		return domain.Notification{}, false, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return domain.Notification{}, false, err
	}
	if affected == 1 {
		if err := tx.Commit(); err != nil {
			return domain.Notification{}, false, err
		}
		return n, false, nil
	}

	existing, err := queryOne(ctx, tx,
		`SELECT `+notificationColumns+` FROM notifications WHERE provider = ? AND idempotency_key = ?`,
		n.Provider, n.IdempotencyKey)
	if err != nil {
		return domain.Notification{}, false, err
	}
	if existing.PayloadHash == n.PayloadHash {
		if err := tx.Commit(); err != nil {
			return domain.Notification{}, false, err
		}
		return existing, true, nil
	}
	if err := tx.Commit(); err != nil {
		return domain.Notification{}, false, err
	}
	return domain.Notification{}, false, domain.ErrIdempotencyConflict
}

func (r *Repository) Get(ctx context.Context, id string) (domain.Notification, error) {
	return queryOne(ctx, r.db,
		`SELECT `+notificationColumns+` FROM notifications WHERE id = ?`, id)
}

func (r *Repository) RecoverExpired(ctx context.Context, now time.Time) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE notifications SET status = ?, lease_until = NULL, updated_at = ?
		 WHERE status = ? AND lease_until IS NOT NULL AND lease_until < ?`,
		domain.StatusPending, formatTime(now), domain.StatusDelivering, formatTime(now))
	return err
}

func (r *Repository) ClaimDue(ctx context.Context, now, leaseUntil time.Time) (domain.Notification, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Notification{}, err
	}
	defer tx.Rollback()

	n, err := queryOne(ctx, tx,
		`SELECT `+notificationColumns+` FROM notifications
		 WHERE status = ? AND (next_attempt_at IS NULL OR next_attempt_at <= ?)
		 ORDER BY created_at, id LIMIT 1`,
		domain.StatusPending, formatTime(now))
	if err != nil {
		return domain.Notification{}, err
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE notifications SET status = ?, lease_until = ?, updated_at = ? WHERE id = ?`,
		domain.StatusDelivering, formatTime(leaseUntil), formatTime(now), n.ID); err != nil {
		return domain.Notification{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.Notification{}, err
	}
	return n, nil
}

func (r *Repository) UpdateAfterDelivery(ctx context.Context, n domain.Notification) error {
	var nextAttemptAt, deliveredAt any
	if n.NextAttemptAt != nil {
		nextAttemptAt = formatTime(*n.NextAttemptAt)
	}
	if n.DeliveredAt != nil {
		deliveredAt = formatTime(*n.DeliveredAt)
	}
	_, err := r.db.ExecContext(ctx,
		`UPDATE notifications SET status = ?, attempts = ?, next_attempt_at = ?, lease_until = NULL,
		 last_error = ?, updated_at = ?, delivered_at = ? WHERE id = ?`,
		n.Status, n.Attempts, nextAttemptAt, n.LastError, formatTime(n.UpdatedAt), deliveredAt, n.ID)
	return err
}

type queryer interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

type rowScanner interface {
	Scan(dest ...any) error
}

func queryOne(ctx context.Context, q queryer, query string, args ...any) (domain.Notification, error) {
	n, err := scanNotification(q.QueryRowContext(ctx, query, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Notification{}, domain.ErrNotFound
	}
	return n, err
}

func scanNotification(row rowScanner) (domain.Notification, error) {
	var n domain.Notification
	var status, createdAt, updatedAt string
	var payload []byte
	var nextAttemptAt, leaseUntil, deliveredAt sql.NullString
	if err := row.Scan(&n.ID, &n.Provider, &n.EventType, &payload, &n.PayloadHash, &n.IdempotencyKey,
		&status, &n.Attempts, &nextAttemptAt, &leaseUntil, &n.LastError, &createdAt, &updatedAt, &deliveredAt); err != nil {
		return domain.Notification{}, err
	}
	n.Payload = payload
	n.Status = domain.Status(status)
	if parsed, err := time.Parse(time.RFC3339Nano, createdAt); err == nil {
		n.CreatedAt = parsed
	}
	if parsed, err := time.Parse(time.RFC3339Nano, updatedAt); err == nil {
		n.UpdatedAt = parsed
	}
	n.NextAttemptAt = parseNullableTime(nextAttemptAt)
	n.LeaseUntil = parseNullableTime(leaseUntil)
	n.DeliveredAt = parseNullableTime(deliveredAt)
	return n, nil
}

func formatTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}

func parseNullableTime(s sql.NullString) *time.Time {
	if !s.Valid || s.String == "" {
		return nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, s.String)
	if err != nil {
		return nil
	}
	return &parsed
}
