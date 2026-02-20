package queue

import "context"

type ReplayJob struct {
	ProjectID       string `json:"projectId"`
	SessionID       string `json:"sessionId"`
	EventsObjectKey string `json:"eventsObjectKey"`
	MarkerOffsetsMs []int  `json:"markerOffsetsMs"`
	TriggerKind     string `json:"triggerKind"`
	Route           string `json:"route"`
	Site            string `json:"site"`
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

type QueueStats struct {
	ReplayStreamDepth   int64 `json:"replayStreamDepth"`
	ReplayPending       int64 `json:"replayPending"`
	ReplayRetryDepth    int64 `json:"replayRetryDepth"`
	ReplayFailedDepth   int64 `json:"replayFailedDepth"`
	AnalysisStreamDepth int64 `json:"analysisStreamDepth"`
	AnalysisPending     int64 `json:"analysisPending"`
	AnalysisRetryDepth  int64 `json:"analysisRetryDepth"`
	AnalysisFailedDepth int64 `json:"analysisFailedDepth"`
}

type DeadLetterQueueKind string

const (
	DeadLetterQueueReplay   DeadLetterQueueKind = "replay"
	DeadLetterQueueAnalysis DeadLetterQueueKind = "analysis"
)

type DeadLetterRedriveResult struct {
	QueueKind       DeadLetterQueueKind `json:"queueKind"`
	Requested       int                 `json:"requested"`
	Redriven        int                 `json:"redriven"`
	Skipped         int                 `json:"skipped"`
	RemainingFailed int64               `json:"remainingFailed"`
}

type DeadLetterEntry struct {
	FailedAt    string `json:"failedAt"`
	Error       string `json:"error"`
	Attempt     int    `json:"attempt"`
	ProjectID   string `json:"projectId"`
	SessionID   string `json:"sessionId"`
	TriggerKind string `json:"triggerKind"`
	Route       string `json:"route"`
	Site        string `json:"site"`
	Payload     string `json:"payload"`
	Raw         string `json:"raw"`
}

type DeadLetterListResult struct {
	QueueKind  DeadLetterQueueKind `json:"queueKind"`
	Scope      DeadLetterScope     `json:"scope"`
	Offset     int                 `json:"offset"`
	Limit      int                 `json:"limit"`
	Total      int64               `json:"total"`
	Entries    []DeadLetterEntry   `json:"entries"`
	Unparsable int64               `json:"unparsable"`
}

type DeadLetterScope string

const (
	DeadLetterScopeFailed        DeadLetterScope = "failed"
	DeadLetterScopeUnprocessable DeadLetterScope = "unprocessable"
)

type DeadLetterPurgeResult struct {
	QueueKind DeadLetterQueueKind `json:"queueKind"`
	Scope     DeadLetterScope     `json:"scope"`
	Requested int                 `json:"requested"`
	Deleted   int                 `json:"deleted"`
	Remaining int64               `json:"remaining"`
}

type Producer interface {
	EnqueueReplayJob(ctx context.Context, job ReplayJob) error
	EnqueueAnalysisJob(ctx context.Context, job AnalysisJob) error
	Close() error
}

type StatsProvider interface {
	QueueStats(ctx context.Context) (QueueStats, error)
}

type DeadLetterRedriver interface {
	RedriveDeadLetters(ctx context.Context, queueKind DeadLetterQueueKind, limit int) (DeadLetterRedriveResult, error)
}

type DeadLetterInspector interface {
	ListDeadLetters(ctx context.Context, queueKind DeadLetterQueueKind, scope DeadLetterScope, offset int, limit int) (DeadLetterListResult, error)
}

type DeadLetterPurger interface {
	PurgeDeadLetters(ctx context.Context, queueKind DeadLetterQueueKind, scope DeadLetterScope, limit int) (DeadLetterPurgeResult, error)
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

func (p *NoopProducer) QueueStats(_ context.Context) (QueueStats, error) {
	return QueueStats{}, nil
}

func (p *NoopProducer) RedriveDeadLetters(_ context.Context, queueKind DeadLetterQueueKind, limit int) (DeadLetterRedriveResult, error) {
	return DeadLetterRedriveResult{
		QueueKind: queueKind,
		Requested: limit,
	}, nil
}

func (p *NoopProducer) ListDeadLetters(_ context.Context, queueKind DeadLetterQueueKind, scope DeadLetterScope, offset int, limit int) (DeadLetterListResult, error) {
	if scope == "" {
		scope = DeadLetterScopeFailed
	}
	return DeadLetterListResult{
		QueueKind: queueKind,
		Scope:     scope,
		Offset:    offset,
		Limit:     limit,
	}, nil
}

func (p *NoopProducer) PurgeDeadLetters(_ context.Context, queueKind DeadLetterQueueKind, scope DeadLetterScope, limit int) (DeadLetterPurgeResult, error) {
	return DeadLetterPurgeResult{
		QueueKind: queueKind,
		Scope:     scope,
		Requested: limit,
	}, nil
}
