package mock

import (
	"context"
	"sync"

	domain "github.com/arisone/redcapital/internal/domain/notification"
)

type Adapter struct {
	mu        sync.Mutex
	Responses []domain.DeliveryResult
	Calls     int
}

func (a *Adapter) Deliver(_ context.Context, _ domain.Notification) domain.DeliveryResult {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.Calls++
	if len(a.Responses) == 0 {
		return domain.DeliveryResult{Outcome: domain.DeliverySucceeded, StatusCode: 200}
	}
	result := a.Responses[0]
	a.Responses = a.Responses[1:]
	return result
}
