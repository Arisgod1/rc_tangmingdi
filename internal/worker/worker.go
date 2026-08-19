package worker

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/arisone/redcapital/internal/adapter"
	domain "github.com/arisone/redcapital/internal/domain/notification"
)

type Worker struct {
	Repository adapterRepository
	Adapters   *adapter.Registry
	PollEvery  time.Duration
	Lease      time.Duration
	Log        *slog.Logger
}

type adapterRepository interface {
	ClaimDue(context.Context, time.Time, time.Time) (domain.Notification, error)
	RecoverExpired(context.Context, time.Time) error
	UpdateAfterDelivery(context.Context, domain.Notification) error
}

func New(repo adapterRepository, adapters *adapter.Registry, pollEvery, lease time.Duration, logger *slog.Logger) *Worker {
	if logger == nil {
		logger = slog.Default()
	}
	return &Worker{Repository: repo, Adapters: adapters, PollEvery: pollEvery, Lease: lease, Log: logger}
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
	if err := w.Repository.RecoverExpired(ctx, now); err != nil {
		return err
	}
	return w.runCycle(ctx)
}

func (w *Worker) runCycle(ctx context.Context) error {
	for {
		now := time.Now()
		n, err := w.Repository.ClaimDue(ctx, now, now.Add(w.Lease))
		if errors.Is(err, domain.ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		ad, ok := w.Adapters.Get(n.Provider)
		var result domain.DeliveryResult
		if !ok {
			result = domain.DeliveryResult{Outcome: domain.DeliveryPermanent, Err: domain.ErrProviderUnavailable}
		} else {
			result = ad.Deliver(ctx, n)
		}
		domain.ApplyDeliveryResult(&n, result, time.Now())
		if err := w.Repository.UpdateAfterDelivery(ctx, n); err != nil {
			return err
		}
	}
}
