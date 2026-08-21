package notification

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

func New(provider, eventType, idempotencyKey string, payload []byte, now time.Time) (Notification, error) {
	provider = strings.TrimSpace(provider)
	eventType = strings.TrimSpace(eventType)
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if provider == "" {
		return Notification{}, fmt.Errorf("provider is required")
	}
	if eventType == "" {
		return Notification{}, fmt.Errorf("event_type is required")
	}
	if idempotencyKey == "" {
		return Notification{}, fmt.Errorf("idempotency_key is required")
	}
	if len(payload) == 0 {
		return Notification{}, fmt.Errorf("payload is required")
	}
	sum := sha256.Sum256(payload)
	return Notification{
		ID:             uuid.NewString(),
		Provider:       provider,
		EventType:      eventType,
		Payload:        payload,
		PayloadHash:    hex.EncodeToString(sum[:]),
		IdempotencyKey: idempotencyKey,
		Status:         StatusPending,
		CreatedAt:      now,
		UpdatedAt:      now,
	}, nil
}
