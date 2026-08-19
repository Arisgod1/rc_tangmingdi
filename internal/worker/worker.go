package worker

import (
	"context"
	"errors"
	"log/slog"
	"time"

	domain "github.com/arisone/redcapital/internal/domain/notification"
	"github.com/arisone/redcapital/internal/registry"
)

type Worker struct {
	Registry  *registry.Registry
	PollEvery time.Duration
	Lease     time.Duration
	Log       *slog.Logger
}

func New(reg *registry.Registry, pollEvery, lease time.Duration, logger *slog.Logger) *Worker {
	if logger == nil {
		logger = slog.Default()
	}
	return &Worker{Registry: reg, PollEvery: pollEvery, Lease: lease, Log: logger}
}

func (w *Worker) Run(ctx context.Context) error {
	ticker := time.NewTicker(w.PollEvery)
	defer ticker.Stop()
	for {
		if err := w.runOnce(ctx); err != nil && !errors.Is(err, context.Canceled) {
			w.Log.Error("worker cycle failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (w *Worker) runOnce(ctx context.Context) error {
	now := time.Now()
	repo := w.Registry.Repository()
	if err := repo.RecoverExpired(ctx, now); err != nil {
		return err
	}
	return w.runCycle(ctx, repo)
}

func (w *Worker) runCycle(ctx context.Context, repo domain.Repository) error {
	for {
		now := time.Now()
		n, err := repo.ClaimDue(ctx, now, now.Add(w.Lease))
		if errors.Is(err, domain.ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		ad, ok := w.Registry.Adapter(n.Provider)
		var result domain.DeliveryResult
		if !ok {
			result = domain.DeliveryResult{Outcome: domain.DeliveryPermanent, Err: domain.ErrProviderUnavailable}
		} else {
			result = ad.Deliver(ctx, n)
		}
		domain.ApplyDeliveryResult(&n, result, time.Now())
		if n.Status == domain.StatusDead {
			w.Log.Warn("notification delivery failed", "notification_id", n.ID, "provider", n.Provider, "attempts", n.Attempts, "error", n.LastError)
		}
		if err := repo.UpdateAfterDelivery(ctx, n); err != nil {
			return err
		}
	}
}
