package artifacts

import (
	"context"
	"encoding/json"
	"errors"
)

var ErrNotConfigured = errors.New("artifact store not configured")

type Store interface {
	LoadJSON(ctx context.Context, objectKey string) (json.RawMessage, error)
	Close() error
}

type NoopStore struct{}

func NewNoopStore() *NoopStore {
	return &NoopStore{}
}

func (s *NoopStore) LoadJSON(_ context.Context, _ string) (json.RawMessage, error) {
	return nil, ErrNotConfigured
}

func (s *NoopStore) Close() error {
	return nil
}
