package queue

import "context"

type ReplayJob struct {
	ProjectID       string `json:"projectId"`
	SessionID       string `json:"sessionId"`
	EventsObjectKey string `json:"eventsObjectKey"`
	MarkerOffsetsMs []int  `json:"markerOffsetsMs"`
	TriggerKind     string `json:"triggerKind"`
}

type AnalysisJob struct {
	ProjectID       string   `json:"projectId"`
	SessionID       string   `json:"sessionId"`
	EventsObjectKey string   `json:"eventsObjectKey"`
	MarkerOffsetsMs []int    `json:"markerOffsetsMs"`
	MarkerHints     []string `json:"markerHints"`
	TriggerKind     string   `json:"triggerKind"`
	Route           string   `json:"route"`
	Site            string   `json:"site"`
}

type Producer interface {
	EnqueueReplayJob(ctx context.Context, job ReplayJob) error
	EnqueueAnalysisJob(ctx context.Context, job AnalysisJob) error
	Close() error
}

type NoopProducer struct{}

func NewNoopProducer() *NoopProducer {
	return &NoopProducer{}
}

func (p *NoopProducer) EnqueueReplayJob(_ context.Context, _ ReplayJob) error {
	return nil
}

func (p *NoopProducer) EnqueueAnalysisJob(_ context.Context, _ AnalysisJob) error {
	return nil
}

func (p *NoopProducer) Close() error {
	return nil
}
