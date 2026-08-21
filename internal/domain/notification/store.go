package notification

import (
	"context"
	"time"
)

type Repository interface {
	CreateOrGet(ctx context.Context, n Notification) (Notification, bool, error)
	Get(ctx context.Context, id string) (Notification, error)
	RecoverExpired(ctx context.Context, now time.Time) error
	ClaimDue(ctx context.Context, now, leaseUntil time.Time) (Notification, error)
	UpdateAfterDelivery(ctx context.Context, n Notification) error
}
