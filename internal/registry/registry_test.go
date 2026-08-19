package registry

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/arisone/redcapital/internal/adapter"
	"github.com/arisone/redcapital/internal/adapter/mock"
	"github.com/arisone/redcapital/internal/config"
)

func TestNewFromDepsAdapterLookup(t *testing.T) {
	adapters := map[string]adapter.Adapter{
		"mock": &mock.Adapter{},
	}
	reg := NewFromDeps(nil, adapters)

	if _, ok := reg.Adapter("mock"); !ok {
		t.Fatal("expected mock adapter")
	}
	if _, ok := reg.Adapter("missing"); ok {
		t.Fatal("expected missing adapter lookup to fail")
	}
}

func TestNewFromDepsCopiesAdapters(t *testing.T) {
	adapters := map[string]adapter.Adapter{
		"mock": &mock.Adapter{},
	}
	reg := NewFromDeps(nil, adapters)
	adapters["webhook"] = &mock.Adapter{}

	if _, ok := reg.Adapter("webhook"); ok {
		t.Fatal("registry should not see adapters added after construction")
	}
}

func TestNewBuildsConfiguredAdapters(t *testing.T) {
	cfg := config.Config{
		DBPath:         filepath.Join(t.TempDir(), "test.db"),
		WebhookURL:     "http://example.com",
		WebhookTimeout: time.Second,
	}
	reg, err := New(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer reg.Close()

	if _, ok := reg.Adapter("mock"); !ok {
		t.Fatal("expected mock adapter")
	}
	if _, ok := reg.Adapter("webhook"); !ok {
		t.Fatal("expected webhook adapter")
	}
}

func TestNewWithoutWebhook(t *testing.T) {
	cfg := config.Config{
		DBPath: filepath.Join(t.TempDir(), "test.db"),
	}
	reg, err := New(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer reg.Close()

	if _, ok := reg.Adapter("webhook"); ok {
		t.Fatal("webhook adapter should be absent without WEBHOOK_URL")
	}
}

func TestCloseFromDepsIsNoop(t *testing.T) {
	reg := NewFromDeps(nil, nil)
	if err := reg.Close(); err != nil {
		t.Fatalf("expected noop close, got %v", err)
	}
}
