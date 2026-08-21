package notification

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	_ "modernc.org/sqlite"
)

const schema = `
CREATE TABLE IF NOT EXISTS notifications (
    id              TEXT PRIMARY KEY,
    provider        TEXT NOT NULL,
    event_type      TEXT NOT NULL,
    payload         BLOB NOT NULL,
    payload_hash    TEXT NOT NULL,
    idempotency_key TEXT NOT NULL,
    status          TEXT NOT NULL,
    attempts        INTEGER NOT NULL DEFAULT 0,
    next_attempt_at TEXT,
    lease_until     TEXT,
    last_error      TEXT NOT NULL DEFAULT '',
    created_at      TEXT NOT NULL,
    updated_at      TEXT NOT NULL,
    delivered_at    TEXT,
    UNIQUE (provider, idempotency_key)
);

CREATE INDEX IF NOT EXISTS idx_notifications_due
    ON notifications (status, next_attempt_at, created_at);
`

func Open(ctx context.Context, dbPath string) (*Repository, error) {
	path := strings.TrimSpace(dbPath)
	if path == "" {
		return nil, fmt.Errorf("db path is required")
	}
	if strings.Contains(path, "?") {
		return nil, fmt.Errorf("db path must not contain query parameters")
	}
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, err
	}
	if _, err := db.ExecContext(ctx, schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate notifications table: %w", err)
	}
	return &Repository{db: db}, nil
}
