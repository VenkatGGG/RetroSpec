package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func TestRedisProducerMigratesLegacyListToStream(t *testing.T) {
	mr := miniredis.RunT(t)
	ctx := context.Background()
	replayQueue := "replay-jobs"
	analysisQueue := "analysis-jobs"

	seedClient := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() {
		_ = seedClient.Close()
	})

	legacyOldest := `{"sessionId":"legacy-1"}`
	legacyNewest := `{"sessionId":"legacy-2"}`
	if err := seedClient.LPush(ctx, replayQueue, legacyOldest).Err(); err != nil {
		t.Fatalf("seed lpush failed: %v", err)
	}
	if err := seedClient.LPush(ctx, replayQueue, legacyNewest).Err(); err != nil {
		t.Fatalf("seed lpush failed: %v", err)
	}

	producer, err := NewRedisProducer(mr.Addr(), replayQueue, analysisQueue)
	if err != nil {
		t.Fatalf("new producer failed: %v", err)
	}
	t.Cleanup(func() {
		_ = producer.Close()
	})

	if err := producer.EnqueueReplayJob(ctx, ReplayJob{
		ProjectID:       "proj_test",
		SessionID:       "session_new",
		EventsObjectKey: "events/new.json",
		MarkerOffsetsMs: []int{1200},
		TriggerKind:     "js_exception",
	}); err != nil {
		t.Fatalf("enqueue replay job failed: %v", err)
	}

	queueType, err := seedClient.Type(ctx, replayQueue).Result()
	if err != nil {
		t.Fatalf("type lookup failed: %v", err)
	}
	if queueType != "stream" {
		t.Fatalf("expected stream key type, got %s", queueType)
	}

	rows, err := seedClient.XRange(ctx, replayQueue, "-", "+").Result()
	if err != nil {
		t.Fatalf("xrange failed: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("expected 3 stream rows, got %d", len(rows))
	}

	firstPayload := rows[0].Values["payload"]
	secondPayload := rows[1].Values["payload"]
	thirdPayload := rows[2].Values["payload"]
	if firstPayload != legacyOldest {
		t.Fatalf("expected oldest legacy payload first, got %v", firstPayload)
	}
	if secondPayload != legacyNewest {
		t.Fatalf("expected newest legacy payload second, got %v", secondPayload)
	}
	thirdPayloadString, ok := thirdPayload.(string)
	if !ok {
		t.Fatalf("expected third payload to be string, got %T", thirdPayload)
	}
	if !strings.Contains(thirdPayloadString, `"sessionId":"session_new"`) {
		t.Fatalf("expected third payload to include new session id, got %s", thirdPayloadString)
	}
}

func TestRedisProducerQueueStatsSnapshot(t *testing.T) {
	mr := miniredis.RunT(t)
	ctx := context.Background()
	replayQueue := "replay-jobs"
	analysisQueue := "analysis-jobs"

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() {
		_ = client.Close()
	})

	producer, err := NewRedisProducer(mr.Addr(), replayQueue, analysisQueue)
	if err != nil {
		t.Fatalf("new producer failed: %v", err)
	}
	t.Cleanup(func() {
		_ = producer.Close()
	})

	for index := 0; index < 3; index += 1 {
		if err := client.XAdd(ctx, &redis.XAddArgs{
			Stream: replayQueue,
			Values: map[string]any{"payload": `{"sessionId":"replay"}`},
		}).Err(); err != nil {
			t.Fatalf("seed replay xadd failed: %v", err)
		}
	}
	if err := client.XGroupCreateMkStream(ctx, replayQueue, replayQueue+":group", "0").Err(); err != nil &&
		!strings.Contains(err.Error(), "BUSYGROUP") {
		t.Fatalf("seed replay group failed: %v", err)
	}
	replayRead, err := client.XReadGroup(ctx, &redis.XReadGroupArgs{
		Group:    replayQueue + ":group",
		Consumer: "test-consumer",
		Streams:  []string{replayQueue, ">"},
		Count:    1,
		Block:    0,
	}).Result()
	if err != nil {
		t.Fatalf("seed replay pending read failed: %v", err)
	}
	if len(replayRead) == 0 {
		t.Fatalf("expected replay read result")
	}
	if err := client.ZAdd(ctx, replayQueue+":retry", redis.Z{Score: 1000, Member: "{}"}).Err(); err != nil {
		t.Fatalf("seed replay retry failed: %v", err)
	}
	if err := client.LPush(ctx, replayQueue+":failed", "failed-replay").Err(); err != nil {
		t.Fatalf("seed replay failed failed: %v", err)
	}

	if err := client.XAdd(ctx, &redis.XAddArgs{
		Stream: analysisQueue,
		Values: map[string]any{"payload": `{"sessionId":"analysis"}`},
	}).Err(); err != nil {
		t.Fatalf("seed analysis xadd failed: %v", err)
	}
	if err := client.XGroupCreateMkStream(ctx, analysisQueue, analysisQueue+":group", "0").Err(); err != nil &&
		!strings.Contains(err.Error(), "BUSYGROUP") {
		t.Fatalf("seed analysis group failed: %v", err)
	}
	analysisRead, err := client.XReadGroup(ctx, &redis.XReadGroupArgs{
		Group:    analysisQueue + ":group",
		Consumer: "test-consumer",
		Streams:  []string{analysisQueue, ">"},
		Count:    1,
		Block:    0,
	}).Result()
	if err != nil {
		t.Fatalf("seed analysis pending read failed: %v", err)
	}
	if len(analysisRead) == 0 {
		t.Fatalf("expected analysis read result")
	}
	if err := client.ZAdd(ctx, analysisQueue+":retry", redis.Z{Score: 1000, Member: "{}"}).Err(); err != nil {
		t.Fatalf("seed analysis retry failed: %v", err)
	}
	if err := client.LPush(ctx, analysisQueue+":failed", "failed-analysis").Err(); err != nil {
		t.Fatalf("seed analysis failed failed: %v", err)
	}

	stats, err := producer.QueueStats(ctx)
	if err != nil {
		t.Fatalf("queue stats failed: %v", err)
	}

	if stats.ReplayStreamDepth != 3 ||
		stats.ReplayPending != 1 ||
		stats.ReplayRetryDepth != 1 ||
		stats.ReplayFailedDepth != 1 {
		t.Fatalf("unexpected replay stats: %+v", stats)
	}
	if stats.AnalysisStreamDepth != 1 ||
		stats.AnalysisPending != 1 ||
		stats.AnalysisRetryDepth != 1 ||
		stats.AnalysisFailedDepth != 1 {
		t.Fatalf("unexpected analysis stats: %+v", stats)
	}
}

func TestRedisProducerRedriveDeadLetters(t *testing.T) {
	mr := miniredis.RunT(t)
	ctx := context.Background()
	replayQueue := "replay-jobs"
	analysisQueue := "analysis-jobs"

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() {
		_ = client.Close()
	})

	producer, err := NewRedisProducer(mr.Addr(), replayQueue, analysisQueue)
	if err != nil {
		t.Fatalf("new producer failed: %v", err)
	}
	t.Cleanup(func() {
		_ = producer.Close()
	})

	oldPayload := `{"projectId":"proj_test","sessionId":"session-old","eventsObjectKey":"events/old.json","markerOffsetsMs":[1200],"triggerKind":"js_exception","attempt":3}`
	newPayload := `{"projectId":"proj_test","sessionId":"session-new","eventsObjectKey":"events/new.json","markerOffsetsMs":[2400],"triggerKind":"js_exception","attempt":3}`
	oldEntry, err := marshalFailedEntry(oldPayload, "old failed")
	if err != nil {
		t.Fatalf("marshal old failed entry: %v", err)
	}
	newEntry, err := marshalFailedEntry(newPayload, "new failed")
	if err != nil {
		t.Fatalf("marshal new failed entry: %v", err)
	}
	if err := client.LPush(ctx, replayQueue+":failed", oldEntry).Err(); err != nil {
		t.Fatalf("seed old replay failed entry: %v", err)
	}
	if err := client.LPush(ctx, replayQueue+":failed", newEntry).Err(); err != nil {
		t.Fatalf("seed new replay failed entry: %v", err)
	}

	firstResult, err := producer.RedriveDeadLetters(ctx, DeadLetterQueueReplay, 1)
	if err != nil {
		t.Fatalf("first redrive failed: %v", err)
	}
	if firstResult.Redriven != 1 || firstResult.Skipped != 0 || firstResult.RemainingFailed != 1 {
		t.Fatalf("unexpected first redrive result: %+v", firstResult)
	}

	firstRows, err := client.XRange(ctx, replayQueue, "-", "+").Result()
	if err != nil {
		t.Fatalf("read replay queue stream failed: %v", err)
	}
	if len(firstRows) != 1 {
		t.Fatalf("expected 1 stream row after first redrive, got %d", len(firstRows))
	}
	if payload, _ := firstRows[0].Values["payload"].(string); payload != oldPayload {
		t.Fatalf("expected oldest payload first, got %v", firstRows[0].Values["payload"])
	}

	secondResult, err := producer.RedriveDeadLetters(ctx, DeadLetterQueueReplay, 10)
	if err != nil {
		t.Fatalf("second redrive failed: %v", err)
	}
	if secondResult.Redriven != 1 || secondResult.Skipped != 0 || secondResult.RemainingFailed != 0 {
		t.Fatalf("unexpected second redrive result: %+v", secondResult)
	}

	allRows, err := client.XRange(ctx, replayQueue, "-", "+").Result()
	if err != nil {
		t.Fatalf("read replay queue stream failed: %v", err)
	}
	if len(allRows) != 2 {
		t.Fatalf("expected 2 stream rows after second redrive, got %d", len(allRows))
	}
	if payload, _ := allRows[1].Values["payload"].(string); payload != newPayload {
		t.Fatalf("expected newest payload second, got %v", allRows[1].Values["payload"])
	}
}

func TestRedisProducerRedriveDeadLettersSkipsUnprocessable(t *testing.T) {
	mr := miniredis.RunT(t)
	ctx := context.Background()
	replayQueue := "replay-jobs"
	analysisQueue := "analysis-jobs"

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() {
		_ = client.Close()
	})

	producer, err := NewRedisProducer(mr.Addr(), replayQueue, analysisQueue)
	if err != nil {
		t.Fatalf("new producer failed: %v", err)
	}
	t.Cleanup(func() {
		_ = producer.Close()
	})

	if err := client.LPush(ctx, replayQueue+":failed", "this-is-not-json").Err(); err != nil {
		t.Fatalf("seed malformed replay failed entry: %v", err)
	}

	result, err := producer.RedriveDeadLetters(ctx, DeadLetterQueueReplay, 5)
	if err != nil {
		t.Fatalf("redrive failed: %v", err)
	}
	if result.Redriven != 0 || result.Skipped != 1 || result.RemainingFailed != 0 {
		t.Fatalf("unexpected redrive result: %+v", result)
	}

	unprocessableDepth, err := client.LLen(ctx, replayQueue+":failed:unprocessable").Result()
	if err != nil {
		t.Fatalf("read unprocessable queue failed: %v", err)
	}
	if unprocessableDepth != 1 {
		t.Fatalf("expected 1 unprocessable entry, got %d", unprocessableDepth)
	}
}

func marshalFailedEntry(payload string, details string) (string, error) {
	entry := map[string]any{
		"failedAt": "2026-01-01T00:00:00Z",
		"error":    details,
		"attempt":  3,
		"payload":  payload,
	}
	encoded, err := json.Marshal(entry)
	if err != nil {
		return "", fmt.Errorf("marshal failed entry: %w", err)
	}
	return string(encoded), nil
}
