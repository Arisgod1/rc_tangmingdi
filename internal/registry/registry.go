package registry

import (
	"context"
	"maps"

	"github.com/arisone/redcapital/internal/adapter"
	"github.com/arisone/redcapital/internal/adapter/mock"
	"github.com/arisone/redcapital/internal/adapter/webhook"
	"github.com/arisone/redcapital/internal/config"
	sqliterepo "github.com/arisone/redcapital/internal/datasource/notification"
	domain "github.com/arisone/redcapital/internal/domain/notification"
)

type Registry struct {
	repository domain.Repository
	adapters   map[string]adapter.Adapter
	closeFn    func() error
}

func New(ctx context.Context, cfg config.Config) (*Registry, error) {
	repo, err := sqliterepo.Open(ctx, cfg.DBPath)
	if err != nil {
		return nil, err
	}
	return &Registry{
		repository: repo,
		adapters:   buildAdapters(cfg),
		closeFn:    repo.Close,
	}, nil
}

func NewFromDeps(repo domain.Repository, adapters map[string]adapter.Adapter) *Registry {
	cloned := make(map[string]adapter.Adapter, len(adapters))
	maps.Copy(cloned, adapters)
	return &Registry{repository: repo, adapters: cloned}
}

func (r *Registry) Repository() domain.Repository {
	return r.repository
}

func (r *Registry) Adapter(provider string) (adapter.Adapter, bool) {
	value, ok := r.adapters[provider]
	return value, ok
}

func (r *Registry) Close() error {
	if r.closeFn == nil {
		return nil
	}
	return r.closeFn()
}

func buildAdapters(cfg config.Config) map[string]adapter.Adapter {
	adapters := map[string]adapter.Adapter{
		"mock": &mock.Adapter{},
	}
	if cfg.WebhookURL != "" {
		adapters["webhook"] = webhook.New(cfg.WebhookURL, cfg.WebhookHeaders, cfg.WebhookTimeout)
	}
	return adapters
}
