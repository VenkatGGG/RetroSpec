package queue

import "context"

type ReplayJob struct {
	SessionID       string `json:"sessionId"`
	EventsObjectKey string `json:"eventsObjectKey"`
	MarkerOffsetsMs []int  `json:"markerOffsetsMs"`
	TriggerKind     string `json:"triggerKind"`
}

type Producer interface {
	EnqueueReplayJob(ctx context.Context, job ReplayJob) error
	Close() error
}

type NoopProducer struct{}

func NewNoopProducer() *NoopProducer {
	return &NoopProducer{}
}

func (p *NoopProducer) EnqueueReplayJob(_ context.Context, _ ReplayJob) error {
	return nil
}

func (p *NoopProducer) Close() error {
	return nil
}
