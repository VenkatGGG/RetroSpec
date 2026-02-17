package artifacts

import (
	"context"
	"encoding/json"
	"errors"
)

var ErrNotConfigured = errors.New("artifact store not configured")

type Store interface {
	StoreJSON(ctx context.Context, objectKey string, payload json.RawMessage) error
	LoadJSON(ctx context.Context, objectKey string) (json.RawMessage, error)
	LoadObject(ctx context.Context, objectKey string) ([]byte, string, error)
	DeleteObject(ctx context.Context, objectKey string) error
	Close() error
}

type LifecycleConfigurer interface {
	EnsureLifecyclePolicy(ctx context.Context, expirationDays int, prefixes []string) error
}

type NoopStore struct{}

func NewNoopStore() *NoopStore {
	return &NoopStore{}
}

func (s *NoopStore) StoreJSON(_ context.Context, _ string, _ json.RawMessage) error {
	return ErrNotConfigured
}

func (s *NoopStore) LoadJSON(_ context.Context, _ string) (json.RawMessage, error) {
	return nil, ErrNotConfigured
}

func (s *NoopStore) LoadObject(_ context.Context, _ string) ([]byte, string, error) {
	return nil, "", ErrNotConfigured
}

func (s *NoopStore) DeleteObject(_ context.Context, _ string) error {
	return ErrNotConfigured
}

func (s *NoopStore) Close() error {
	return nil
}

func (s *NoopStore) EnsureLifecyclePolicy(_ context.Context, _ int, _ []string) error {
	return ErrNotConfigured
}
