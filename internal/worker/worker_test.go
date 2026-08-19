package worker

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/arisone/redcapital/internal/adapter"
	"github.com/arisone/redcapital/internal/adapter/mock"
	sqliterepo "github.com/arisone/redcapital/internal/datasource/notification"
	domain "github.com/arisone/redcapital/internal/domain/notification"
)

func TestWorkerDeliversAndTracksAttempts(t *testing.T) {
	repo, err := sqliterepo.Open(context.Background(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()

	ctx := context.Background()
	now := time.Now()
	n, err := domain.New("mock", "registered", "key-1", []byte(`{"a":1}`), now)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := repo.CreateOrGet(ctx, n); err != nil {
		t.Fatal(err)
	}

	responses := []domain.DeliveryResult{
		{Outcome: domain.DeliveryRetryable, StatusCode: 500, Err: domain.ErrNotFound},
		{Outcome: domain.DeliverySucceeded, StatusCode: 200},
	}
	mockAdapter := &mock.Adapter{Responses: responses}
	registry := adapter.NewRegistry(map[string]adapter.Adapter{"mock": mockAdapter})
	worker := New(repo, registry, time.Millisecond, 30*time.Second, nil)

	if err := worker.runOnce(ctx); err != nil {
		t.Fatal(err)
	}
	afterRetry, err := repo.Get(ctx, n.ID)
	if err != nil {
		t.Fatal(err)
	}
	if afterRetry.Status != domain.StatusRetryWait || afterRetry.Attempts != 1 {
		t.Fatalf("expected RETRY_WAIT after first failure, got %+v", afterRetry)
	}
	due := time.Now().Add(-time.Second)
	if err := repo.BackdateNextAttempt(ctx, n.ID, due); err != nil {
		t.Fatal(err)
	}

	if err := worker.runOnce(ctx); err != nil {
		t.Fatal(err)
	}
	done, err := repo.Get(ctx, n.ID)
	if err != nil {
		t.Fatal(err)
	}
	if done.Status != domain.StatusSucceeded || done.Attempts != 2 {
		t.Fatalf("expected SUCCEEDED on second attempt, got %+v", done)
	}
}

func TestWorkerPermanentFailureGoesDead(t *testing.T) {
	repo, err := sqliterepo.Open(context.Background(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()

	ctx := context.Background()
	n, err := domain.New("mock", "registered", "key-1", []byte(`{"a":1}`), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := repo.CreateOrGet(ctx, n); err != nil {
		t.Fatal(err)
	}

	mockAdapter := &mock.Adapter{Responses: []domain.DeliveryResult{{Outcome: domain.DeliveryPermanent, StatusCode: 400}}}
	registry := adapter.NewRegistry(map[string]adapter.Adapter{"mock": mockAdapter})
	worker := New(repo, registry, time.Millisecond, 30*time.Second, nil)
	if err := worker.runOnce(ctx); err != nil {
		t.Fatal(err)
	}
	stored, err := repo.Get(ctx, n.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != domain.StatusDead || stored.LastError == "" {
		t.Fatalf("expected DEAD with error, got %+v", stored)
	}
}

func TestWorkerRestartRecoversExpiredLease(t *testing.T) {
	repo, err := sqliterepo.Open(context.Background(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()

	ctx := context.Background()
	n, err := domain.New("mock", "registered", "key-1", []byte(`{"a":1}`), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := repo.CreateOrGet(ctx, n); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if _, err := repo.ClaimDue(ctx, now, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := repo.BackdateLease(ctx, n.ID, now.Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}

	registry := adapter.NewRegistry(map[string]adapter.Adapter{
		"mock": &mock.Adapter{Responses: []domain.DeliveryResult{{Outcome: domain.DeliverySucceeded, StatusCode: 200}}},
	})
	worker := New(repo, registry, time.Millisecond, 30*time.Second, nil)
	if err := worker.runOnce(ctx); err != nil {
		t.Fatal(err)
	}
	stored, err := repo.Get(ctx, n.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != domain.StatusSucceeded {
		t.Fatalf("expired lease should be recovered and delivered, got %+v", stored)
	}
}
