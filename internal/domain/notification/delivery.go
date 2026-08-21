package notification

import (
	"fmt"
	"time"
)

func ApplyDeliveryResult(n *Notification, result DeliveryResult, now time.Time) {
	n.Attempts++
	n.UpdatedAt = now
	n.LeaseUntil = nil
	n.NextAttemptAt = nil
	switch result.Outcome {
	case DeliverySucceeded:
		n.Status = StatusSucceeded
		n.LastError = ""
		n.DeliveredAt = &now
	default:
		n.Status = StatusDead
		n.DeliveredAt = nil
		n.LastError = deliveryErrorMessage(result)
	}
}

func deliveryErrorMessage(result DeliveryResult) string {
	if result.Err != nil {
		return result.Err.Error()
	}
	switch result.Outcome {
	case DeliveryRetryable:
		if result.StatusCode > 0 {
			return fmt.Sprintf("retryable provider response (HTTP %d)", result.StatusCode)
		}
		return "retryable provider response"
	case DeliveryPermanent:
		if result.StatusCode > 0 {
			return fmt.Sprintf("permanent provider response (HTTP %d)", result.StatusCode)
		}
		return "permanent provider failure"
	case DeliveryUnknown:
		return "delivery result unknown"
	default:
		return "delivery failed"
	}
}
