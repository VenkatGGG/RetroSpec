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
