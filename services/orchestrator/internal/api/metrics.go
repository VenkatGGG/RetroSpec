package api

import (
	"context"
	"fmt"
	"net/http"
	"sync/atomic"
	"time"

	"retrospec/services/orchestrator/internal/queue"
)

type apiMetrics struct {
	startedAtUnix               int64
	queueStatsProvider          queue.StatsProvider
	ingestSessionsTotal         atomic.Int64
	replayQueueErrorsTotal      atomic.Int64
	analysisQueueErrorsTotal    atomic.Int64
	replayArtifactsTotal        atomic.Int64
	analysisReportsTotal        atomic.Int64
	cleanupRunsTotal            atomic.Int64
	cleanupEventObjectsTotal    atomic.Int64
	cleanupArtifactObjectsTotal atomic.Int64
	rateLimitedTotal            atomic.Int64
	queueMetricsErrorsTotal     atomic.Int64
}

func newAPIMetrics(queueStatsProvider queue.StatsProvider) *apiMetrics {
	return &apiMetrics{
		startedAtUnix:      time.Now().Unix(),
		queueStatsProvider: queueStatsProvider,
	}
}

func (m *apiMetrics) handleMetrics(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	w.WriteHeader(http.StatusOK)

	uptimeSeconds := time.Now().Unix() - m.startedAtUnix
	_, _ = fmt.Fprintf(w, "# HELP retrospec_uptime_seconds Process uptime in seconds.\n")
	_, _ = fmt.Fprintf(w, "# TYPE retrospec_uptime_seconds gauge\n")
	_, _ = fmt.Fprintf(w, "retrospec_uptime_seconds %d\n", uptimeSeconds)

	_, _ = fmt.Fprintf(w, "# HELP retrospec_ingest_sessions_total Number of accepted ingest sessions.\n")
	_, _ = fmt.Fprintf(w, "# TYPE retrospec_ingest_sessions_total counter\n")
	_, _ = fmt.Fprintf(w, "retrospec_ingest_sessions_total %d\n", m.ingestSessionsTotal.Load())

	_, _ = fmt.Fprintf(w, "# HELP retrospec_replay_queue_errors_total Replay enqueue failures.\n")
	_, _ = fmt.Fprintf(w, "# TYPE retrospec_replay_queue_errors_total counter\n")
	_, _ = fmt.Fprintf(w, "retrospec_replay_queue_errors_total %d\n", m.replayQueueErrorsTotal.Load())

	_, _ = fmt.Fprintf(w, "# HELP retrospec_analysis_queue_errors_total Analysis enqueue failures.\n")
	_, _ = fmt.Fprintf(w, "# TYPE retrospec_analysis_queue_errors_total counter\n")
	_, _ = fmt.Fprintf(w, "retrospec_analysis_queue_errors_total %d\n", m.analysisQueueErrorsTotal.Load())

	_, _ = fmt.Fprintf(w, "# HELP retrospec_replay_artifacts_total Reported replay artifacts from workers.\n")
	_, _ = fmt.Fprintf(w, "# TYPE retrospec_replay_artifacts_total counter\n")
	_, _ = fmt.Fprintf(w, "retrospec_replay_artifacts_total %d\n", m.replayArtifactsTotal.Load())

	_, _ = fmt.Fprintf(w, "# HELP retrospec_analysis_reports_total Reported session analysis cards from workers.\n")
	_, _ = fmt.Fprintf(w, "# TYPE retrospec_analysis_reports_total counter\n")
	_, _ = fmt.Fprintf(w, "retrospec_analysis_reports_total %d\n", m.analysisReportsTotal.Load())

	_, _ = fmt.Fprintf(w, "# HELP retrospec_cleanup_runs_total Cleanup runs executed.\n")
	_, _ = fmt.Fprintf(w, "# TYPE retrospec_cleanup_runs_total counter\n")
	_, _ = fmt.Fprintf(w, "retrospec_cleanup_runs_total %d\n", m.cleanupRunsTotal.Load())

	_, _ = fmt.Fprintf(w, "# HELP retrospec_cleanup_event_objects_total Event objects deleted by cleanup.\n")
	_, _ = fmt.Fprintf(w, "# TYPE retrospec_cleanup_event_objects_total counter\n")
	_, _ = fmt.Fprintf(w, "retrospec_cleanup_event_objects_total %d\n", m.cleanupEventObjectsTotal.Load())

	_, _ = fmt.Fprintf(w, "# HELP retrospec_cleanup_artifact_objects_total Replay artifact objects deleted by cleanup.\n")
	_, _ = fmt.Fprintf(w, "# TYPE retrospec_cleanup_artifact_objects_total counter\n")
	_, _ = fmt.Fprintf(w, "retrospec_cleanup_artifact_objects_total %d\n", m.cleanupArtifactObjectsTotal.Load())

	_, _ = fmt.Fprintf(w, "# HELP retrospec_rate_limited_total Requests rejected due to rate limiting.\n")
	_, _ = fmt.Fprintf(w, "# TYPE retrospec_rate_limited_total counter\n")
	_, _ = fmt.Fprintf(w, "retrospec_rate_limited_total %d\n", m.rateLimitedTotal.Load())

	if m.queueStatsProvider != nil {
		stats, err := m.loadQueueStats(r.Context())
		if err != nil {
			m.queueMetricsErrorsTotal.Add(1)
		} else {
			_, _ = fmt.Fprintf(w, "# HELP retrospec_replay_queue_stream_depth Replay stream entries waiting/retained in Redis stream.\n")
			_, _ = fmt.Fprintf(w, "# TYPE retrospec_replay_queue_stream_depth gauge\n")
			_, _ = fmt.Fprintf(w, "retrospec_replay_queue_stream_depth %d\n", stats.ReplayStreamDepth)

			_, _ = fmt.Fprintf(w, "# HELP retrospec_replay_queue_pending_total Replay stream pending entries for consumer group.\n")
			_, _ = fmt.Fprintf(w, "# TYPE retrospec_replay_queue_pending_total gauge\n")
			_, _ = fmt.Fprintf(w, "retrospec_replay_queue_pending_total %d\n", stats.ReplayPending)

			_, _ = fmt.Fprintf(w, "# HELP retrospec_replay_queue_retry_depth Replay retry zset depth.\n")
			_, _ = fmt.Fprintf(w, "# TYPE retrospec_replay_queue_retry_depth gauge\n")
			_, _ = fmt.Fprintf(w, "retrospec_replay_queue_retry_depth %d\n", stats.ReplayRetryDepth)

			_, _ = fmt.Fprintf(w, "# HELP retrospec_replay_queue_failed_depth Replay dead-letter list depth.\n")
			_, _ = fmt.Fprintf(w, "# TYPE retrospec_replay_queue_failed_depth gauge\n")
			_, _ = fmt.Fprintf(w, "retrospec_replay_queue_failed_depth %d\n", stats.ReplayFailedDepth)

			_, _ = fmt.Fprintf(w, "# HELP retrospec_analysis_queue_stream_depth Analysis stream entries waiting/retained in Redis stream.\n")
			_, _ = fmt.Fprintf(w, "# TYPE retrospec_analysis_queue_stream_depth gauge\n")
			_, _ = fmt.Fprintf(w, "retrospec_analysis_queue_stream_depth %d\n", stats.AnalysisStreamDepth)

			_, _ = fmt.Fprintf(w, "# HELP retrospec_analysis_queue_pending_total Analysis stream pending entries for consumer group.\n")
			_, _ = fmt.Fprintf(w, "# TYPE retrospec_analysis_queue_pending_total gauge\n")
			_, _ = fmt.Fprintf(w, "retrospec_analysis_queue_pending_total %d\n", stats.AnalysisPending)

			_, _ = fmt.Fprintf(w, "# HELP retrospec_analysis_queue_retry_depth Analysis retry zset depth.\n")
			_, _ = fmt.Fprintf(w, "# TYPE retrospec_analysis_queue_retry_depth gauge\n")
			_, _ = fmt.Fprintf(w, "retrospec_analysis_queue_retry_depth %d\n", stats.AnalysisRetryDepth)

			_, _ = fmt.Fprintf(w, "# HELP retrospec_analysis_queue_failed_depth Analysis dead-letter list depth.\n")
			_, _ = fmt.Fprintf(w, "# TYPE retrospec_analysis_queue_failed_depth gauge\n")
			_, _ = fmt.Fprintf(w, "retrospec_analysis_queue_failed_depth %d\n", stats.AnalysisFailedDepth)
		}
	}

	_, _ = fmt.Fprintf(w, "# HELP retrospec_queue_metrics_errors_total Queue metrics collection errors.\n")
	_, _ = fmt.Fprintf(w, "# TYPE retrospec_queue_metrics_errors_total counter\n")
	_, _ = fmt.Fprintf(w, "retrospec_queue_metrics_errors_total %d\n", m.queueMetricsErrorsTotal.Load())
}

func (m *apiMetrics) loadQueueStats(parent context.Context) (queue.QueueStats, error) {
	ctx := parent
	cancel := func() {}
	if ctx == nil {
		ctx = context.Background()
	}
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		ctx, cancel = context.WithTimeout(ctx, 1200*time.Millisecond)
	}
	defer cancel()

	return m.queueStatsProvider.QueueStats(ctx)
}
