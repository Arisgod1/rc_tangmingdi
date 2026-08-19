package webhook

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	domain "github.com/arisone/redcapital/internal/domain/notification"
)

func TestDeliverClassifiesStatus(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		want       domain.DeliveryOutcome
	}{
		{name: "ok", statusCode: http.StatusOK, want: domain.DeliverySucceeded},
		{name: "created", statusCode: http.StatusCreated, want: domain.DeliverySucceeded},
		{name: "timeout", statusCode: http.StatusRequestTimeout, want: domain.DeliveryRetryable},
		{name: "rate limited", statusCode: http.StatusTooManyRequests, want: domain.DeliveryRetryable},
		{name: "server error", statusCode: http.StatusInternalServerError, want: domain.DeliveryRetryable},
		{name: "bad request", statusCode: http.StatusBadRequest, want: domain.DeliveryPermanent},
		{name: "unauthorized", statusCode: http.StatusUnauthorized, want: domain.DeliveryPermanent},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if got := r.Header.Get("Content-Type"); got != "application/json" {
					t.Errorf("unexpected content type %q", got)
				}
				if got := r.Header.Get("X-Api-Key"); got != "secret" {
					t.Errorf("unexpected api key %q", got)
				}
				w.WriteHeader(tt.statusCode)
			}))
			defer server.Close()

			adapter := New(server.URL, map[string]string{"Content-Type": "application/json", "X-Api-Key": "secret"}, 5*time.Second)
			n := domain.Notification{Payload: []byte(`{"event":"x"}`)}
			result := adapter.Deliver(context.Background(), n)
			if result.Outcome != tt.want {
				t.Fatalf("expected outcome %v, got %v", tt.want, result.Outcome)
			}
		})
	}
}

func TestDeliverNetworkFailureIsUnknown(t *testing.T) {
	adapter := New("http://127.0.0.1:1", nil, time.Second)
	result := adapter.Deliver(context.Background(), domain.Notification{Payload: []byte(`{}`)})
	if result.Outcome != domain.DeliveryUnknown {
		t.Fatalf("expected unknown, got %v", result.Outcome)
	}
}
