package notification

import (
	"errors"
	"time"
)

var (
	ErrNotFound            = errors.New("notification not found")
	ErrIdempotencyConflict = errors.New("idempotency key conflict")
	ErrProviderUnavailable = errors.New("provider adapter unavailable")
)

type Status string

const (
	StatusPending    Status = "PENDING"
	StatusDelivering Status = "DELIVERING"
	StatusSucceeded  Status = "SUCCEEDED"
	StatusDead       Status = "DEAD"
)

type DeliveryOutcome int

const (
	DeliverySucceeded DeliveryOutcome = iota
	DeliveryRetryable
	DeliveryPermanent
	DeliveryUnknown
)

type DeliveryResult struct {
	Outcome    DeliveryOutcome
	StatusCode int
	Err        error
}

type Notification struct {
	ID             string
	Provider       string
	EventType      string
	Payload        []byte
	PayloadHash    string
	IdempotencyKey string
	Status         Status
	Attempts       int
	NextAttemptAt  *time.Time
	LeaseUntil     *time.Time
	LastError      string
	CreatedAt      time.Time
	UpdatedAt      time.Time
	DeliveredAt    *time.Time
}
