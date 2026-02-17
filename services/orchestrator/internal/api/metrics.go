package api

import (
	"fmt"
	"net/http"
	"sync/atomic"
	"time"
)

type apiMetrics struct {
	startedAtUnix               int64
	ingestSessionsTotal         atomic.Int64
	replayQueueErrorsTotal      atomic.Int64
	analysisQueueErrorsTotal    atomic.Int64
	replayArtifactsTotal        atomic.Int64
	analysisReportsTotal        atomic.Int64
	cleanupRunsTotal            atomic.Int64
	cleanupEventObjectsTotal    atomic.Int64
	cleanupArtifactObjectsTotal atomic.Int64
	alertSentTotal              atomic.Int64
	alertErrorsTotal            atomic.Int64
	rateLimitedTotal            atomic.Int64
}

func newAPIMetrics() *apiMetrics {
	return &apiMetrics{
		startedAtUnix: time.Now().Unix(),
	}
}

func (m *apiMetrics) handleMetrics(w http.ResponseWriter, _ *http.Request) {
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

	_, _ = fmt.Fprintf(w, "# HELP retrospec_alert_sent_total Outbound issue alerts successfully sent.\n")
	_, _ = fmt.Fprintf(w, "# TYPE retrospec_alert_sent_total counter\n")
	_, _ = fmt.Fprintf(w, "retrospec_alert_sent_total %d\n", m.alertSentTotal.Load())

	_, _ = fmt.Fprintf(w, "# HELP retrospec_alert_errors_total Outbound issue alert send failures.\n")
	_, _ = fmt.Fprintf(w, "# TYPE retrospec_alert_errors_total counter\n")
	_, _ = fmt.Fprintf(w, "retrospec_alert_errors_total %d\n", m.alertErrorsTotal.Load())

	_, _ = fmt.Fprintf(w, "# HELP retrospec_rate_limited_total Requests rejected due to rate limiting.\n")
	_, _ = fmt.Fprintf(w, "# TYPE retrospec_rate_limited_total counter\n")
	_, _ = fmt.Fprintf(w, "retrospec_rate_limited_total %d\n", m.rateLimitedTotal.Load())
}
