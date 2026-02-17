package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"retrospec/services/orchestrator/internal/queue"
)

type stubQueueStatsProvider struct {
	stats queue.QueueStats
	err   error
}

func (s stubQueueStatsProvider) QueueStats(context.Context) (queue.QueueStats, error) {
	if s.err != nil {
		return queue.QueueStats{}, s.err
	}
	return s.stats, nil
}

type stubQueueRedriver struct {
	result    queue.DeadLetterRedriveResult
	err       error
	queueKind queue.DeadLetterQueueKind
	limit     int
}

func (s *stubQueueRedriver) RedriveDeadLetters(_ context.Context, queueKind queue.DeadLetterQueueKind, limit int) (queue.DeadLetterRedriveResult, error) {
	s.queueKind = queueKind
	s.limit = limit
	if s.err != nil {
		return queue.DeadLetterRedriveResult{}, s.err
	}
	return s.result, nil
}

type stubQueueDeadLetterInspector struct {
	result    queue.DeadLetterListResult
	err       error
	queueKind queue.DeadLetterQueueKind
	limit     int
}

func (s *stubQueueDeadLetterInspector) ListDeadLetters(_ context.Context, queueKind queue.DeadLetterQueueKind, limit int) (queue.DeadLetterListResult, error) {
	s.queueKind = queueKind
	s.limit = limit
	if s.err != nil {
		return queue.DeadLetterListResult{}, s.err
	}
	return s.result, nil
}

func TestGetQueueHealthUnavailableProvider(t *testing.T) {
	handler := &Handler{}
	request := httptest.NewRequest(http.MethodGet, "/v1/admin/queue-health", nil)
	recorder := httptest.NewRecorder()

	handler.getQueueHealth(recorder, request)

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected status %d, got %d", http.StatusServiceUnavailable, recorder.Code)
	}
}

func TestGetQueueHealthCriticalStatus(t *testing.T) {
	handler := &Handler{
		queueStatsProvider: stubQueueStatsProvider{
			stats: queue.QueueStats{
				ReplayFailedDepth: 2,
			},
		},
	}

	request := httptest.NewRequest(http.MethodGet, "/v1/admin/queue-health", nil)
	recorder := httptest.NewRecorder()
	handler.getQueueHealth(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, recorder.Code)
	}

	var body map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if body["status"] != "critical" {
		t.Fatalf("expected critical status, got %v", body["status"])
	}
}

func TestGetQueueHealthProviderError(t *testing.T) {
	handler := &Handler{
		queueStatsProvider: stubQueueStatsProvider{
			err: errors.New("redis unavailable"),
		},
	}

	request := httptest.NewRequest(http.MethodGet, "/v1/admin/queue-health", nil)
	recorder := httptest.NewRecorder()
	handler.getQueueHealth(recorder, request)

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected status %d, got %d", http.StatusServiceUnavailable, recorder.Code)
	}
}

func TestGetQueueHealthWarningStatusByThreshold(t *testing.T) {
	handler := &Handler{
		queueStatsProvider: stubQueueStatsProvider{
			stats: queue.QueueStats{
				ReplayPending: 5,
			},
		},
		queueWarningPending:  5,
		queueWarningRetry:    1,
		queueCriticalPending: 50,
		queueCriticalRetry:   10,
		queueCriticalFailed:  1,
	}

	request := httptest.NewRequest(http.MethodGet, "/v1/admin/queue-health", nil)
	recorder := httptest.NewRecorder()
	handler.getQueueHealth(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, recorder.Code)
	}

	var body map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if body["status"] != "warning" {
		t.Fatalf("expected warning status, got %v", body["status"])
	}
}

func TestMetricsIncludesQueueDepthGauges(t *testing.T) {
	metrics := newAPIMetrics(stubQueueStatsProvider{
		stats: queue.QueueStats{
			ReplayStreamDepth:   7,
			ReplayPending:       1,
			ReplayRetryDepth:    2,
			ReplayFailedDepth:   0,
			AnalysisStreamDepth: 3,
			AnalysisPending:     0,
			AnalysisRetryDepth:  1,
			AnalysisFailedDepth: 1,
		},
	})

	request := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	recorder := httptest.NewRecorder()
	metrics.handleMetrics(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, recorder.Code)
	}

	payload := recorder.Body.String()
	expectedLines := []string{
		"retrospec_replay_queue_stream_depth 7",
		"retrospec_replay_queue_pending_total 1",
		"retrospec_replay_queue_retry_depth 2",
		"retrospec_analysis_queue_stream_depth 3",
		"retrospec_analysis_queue_retry_depth 1",
		"retrospec_analysis_queue_failed_depth 1",
		"retrospec_queue_metrics_errors_total 0",
	}
	for _, expected := range expectedLines {
		if !strings.Contains(payload, expected) {
			t.Fatalf("expected metrics payload to contain %q", expected)
		}
	}
}

func TestMetricsQueueProviderErrorIncrementsCounter(t *testing.T) {
	metrics := newAPIMetrics(stubQueueStatsProvider{
		err: errors.New("queue stats failed"),
	})

	request := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	recorder := httptest.NewRecorder()
	metrics.handleMetrics(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, recorder.Code)
	}

	payload := recorder.Body.String()
	if !strings.Contains(payload, "retrospec_queue_metrics_errors_total 1") {
		t.Fatalf("expected queue metrics error counter to increment, payload=%s", payload)
	}
}

func TestRedriveDeadLettersUnavailableProvider(t *testing.T) {
	handler := &Handler{}
	request := httptest.NewRequest(http.MethodPost, "/v1/admin/queue-redrive", strings.NewReader(`{"queue":"replay","limit":1}`))
	recorder := httptest.NewRecorder()

	handler.redriveDeadLetters(recorder, request)

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected status %d, got %d", http.StatusServiceUnavailable, recorder.Code)
	}
}

func TestRedriveDeadLettersRejectsInvalidQueueKind(t *testing.T) {
	redriver := &stubQueueRedriver{}
	handler := &Handler{
		queueRedriver: redriver,
	}
	request := httptest.NewRequest(http.MethodPost, "/v1/admin/queue-redrive", strings.NewReader(`{"queue":"invalid","limit":1}`))
	recorder := httptest.NewRecorder()

	handler.redriveDeadLetters(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, recorder.Code)
	}
}

func TestRedriveDeadLettersSuccess(t *testing.T) {
	redriver := &stubQueueRedriver{
		result: queue.DeadLetterRedriveResult{
			QueueKind:       queue.DeadLetterQueueAnalysis,
			Requested:       7,
			Redriven:        5,
			Skipped:         1,
			RemainingFailed: 9,
		},
	}
	handler := &Handler{
		queueRedriver: redriver,
	}
	request := httptest.NewRequest(http.MethodPost, "/v1/admin/queue-redrive", strings.NewReader(`{"queue":"analysis","limit":7}`))
	recorder := httptest.NewRecorder()

	handler.redriveDeadLetters(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, recorder.Code)
	}
	if redriver.queueKind != queue.DeadLetterQueueAnalysis {
		t.Fatalf("expected analysis queue kind, got %s", redriver.queueKind)
	}
	if redriver.limit != 7 {
		t.Fatalf("expected limit=7, got %d", redriver.limit)
	}

	var body struct {
		Result queue.DeadLetterRedriveResult `json:"result"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if body.Result.Redriven != 5 || body.Result.Skipped != 1 || body.Result.RemainingFailed != 9 {
		t.Fatalf("unexpected response body: %+v", body.Result)
	}
}

func TestGetQueueDeadLettersUnavailableProvider(t *testing.T) {
	handler := &Handler{}
	request := httptest.NewRequest(http.MethodGet, "/v1/admin/queue-dead-letters?queue=replay", nil)
	recorder := httptest.NewRecorder()

	handler.getQueueDeadLetters(recorder, request)

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected status %d, got %d", http.StatusServiceUnavailable, recorder.Code)
	}
}

func TestGetQueueDeadLettersRejectsInvalidQueueKind(t *testing.T) {
	inspector := &stubQueueDeadLetterInspector{}
	handler := &Handler{
		queueDeadLetterInspector: inspector,
	}
	request := httptest.NewRequest(http.MethodGet, "/v1/admin/queue-dead-letters?queue=invalid", nil)
	recorder := httptest.NewRecorder()

	handler.getQueueDeadLetters(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, recorder.Code)
	}
}

func TestGetQueueDeadLettersSuccess(t *testing.T) {
	inspector := &stubQueueDeadLetterInspector{
		result: queue.DeadLetterListResult{
			QueueKind: queue.DeadLetterQueueReplay,
			Limit:     10,
			Total:     12,
			Entries: []queue.DeadLetterEntry{
				{
					FailedAt:    "2026-02-17T10:00:00Z",
					Error:       "render timeout",
					Attempt:     3,
					ProjectID:   "proj_a",
					SessionID:   "sess_1",
					TriggerKind: "js_exception",
					Payload:     "{\"projectId\":\"proj_a\",\"sessionId\":\"sess_1\"}",
				},
			},
			Unparsable: 1,
		},
	}
	handler := &Handler{
		queueDeadLetterInspector: inspector,
	}
	request := httptest.NewRequest(http.MethodGet, "/v1/admin/queue-dead-letters?queue=replay&limit=10", nil)
	recorder := httptest.NewRecorder()

	handler.getQueueDeadLetters(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, recorder.Code)
	}
	if inspector.queueKind != queue.DeadLetterQueueReplay {
		t.Fatalf("expected replay queue kind, got %s", inspector.queueKind)
	}
	if inspector.limit != 10 {
		t.Fatalf("expected limit=10, got %d", inspector.limit)
	}

	var body struct {
		Result queue.DeadLetterListResult `json:"result"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if body.Result.Total != 12 || len(body.Result.Entries) != 1 || body.Result.Unparsable != 1 {
		t.Fatalf("unexpected response body: %+v", body.Result)
	}
}
