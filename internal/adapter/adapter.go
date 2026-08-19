package adapter

import (
	"context"

	domain "github.com/arisone/redcapital/internal/domain/notification"
)

type Adapter interface {
	Deliver(ctx context.Context, notification domain.Notification) domain.DeliveryResult
}

type Registry struct{ adapters map[string]Adapter }

func NewRegistry(adapters map[string]Adapter) *Registry {
	copy := make(map[string]Adapter, len(adapters))
	for key, value := range adapters {
		copy[key] = value
	}
	return &Registry{adapters: copy}
}

func (r *Registry) Get(provider string) (Adapter, bool) {
	value, ok := r.adapters[provider]
	return value, ok
}
