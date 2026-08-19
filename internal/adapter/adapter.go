package adapter

import (
	"context"

	domain "github.com/arisone/redcapital/internal/domain/notification"
)

type Adapter interface {
	Deliver(ctx context.Context, notification domain.Notification) domain.DeliveryResult
}
