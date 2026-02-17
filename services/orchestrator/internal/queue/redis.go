package queue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

type RedisProducer struct {
	client            *redis.Client
	replayQueueName   string
	analysisQueueName string
	ensureMu          sync.Mutex
	queuesEnsured     bool
}

func NewRedisProducer(addr, replayQueueName, analysisQueueName string) (*RedisProducer, error) {
	client := redis.NewClient(&redis.Options{
		Addr:         addr,
		DialTimeout:  5 * time.Second,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		return nil, err
	}

	return &RedisProducer{
		client:            client,
		replayQueueName:   replayQueueName,
		analysisQueueName: analysisQueueName,
	}, nil
}

func (p *RedisProducer) EnqueueReplayJob(ctx context.Context, job ReplayJob) error {
	if err := p.ensureStreamQueues(ctx); err != nil {
		return err
	}

	payload, err := json.Marshal(job)
	if err != nil {
		return err
	}

	if err := p.client.XAdd(ctx, &redis.XAddArgs{
		Stream: p.replayQueueName,
		Values: map[string]any{
			"payload": string(payload),
		},
	}).Err(); err != nil {
		return fmt.Errorf("enqueue replay job: %w", err)
	}
	return nil
}

func (p *RedisProducer) EnqueueAnalysisJob(ctx context.Context, job AnalysisJob) error {
	if err := p.ensureStreamQueues(ctx); err != nil {
		return err
	}

	payload, err := json.Marshal(job)
	if err != nil {
		return err
	}

	if err := p.client.XAdd(ctx, &redis.XAddArgs{
		Stream: p.analysisQueueName,
		Values: map[string]any{
			"payload": string(payload),
		},
	}).Err(); err != nil {
		return fmt.Errorf("enqueue analysis job: %w", err)
	}
	return nil
}

func (p *RedisProducer) Close() error {
	return p.client.Close()
}

func (p *RedisProducer) QueueStats(ctx context.Context) (QueueStats, error) {
	if err := p.ensureStreamQueues(ctx); err != nil {
		return QueueStats{}, err
	}

	replay, err := p.queueStatsFor(ctx, p.replayQueueName)
	if err != nil {
		return QueueStats{}, fmt.Errorf("replay queue stats: %w", err)
	}
	analysis, err := p.queueStatsFor(ctx, p.analysisQueueName)
	if err != nil {
		return QueueStats{}, fmt.Errorf("analysis queue stats: %w", err)
	}

	return QueueStats{
		ReplayStreamDepth:   replay.streamDepth,
		ReplayPending:       replay.pendingDepth,
		ReplayRetryDepth:    replay.retryDepth,
		ReplayFailedDepth:   replay.failedDepth,
		AnalysisStreamDepth: analysis.streamDepth,
		AnalysisPending:     analysis.pendingDepth,
		AnalysisRetryDepth:  analysis.retryDepth,
		AnalysisFailedDepth: analysis.failedDepth,
	}, nil
}

func (p *RedisProducer) RedriveDeadLetters(ctx context.Context, queueKind DeadLetterQueueKind, limit int) (DeadLetterRedriveResult, error) {
	if err := p.ensureStreamQueues(ctx); err != nil {
		return DeadLetterRedriveResult{}, err
	}

	queueName, err := p.deadLetterQueueName(queueKind)
	if err != nil {
		return DeadLetterRedriveResult{}, err
	}

	normalizedLimit := limit
	if normalizedLimit < 1 {
		normalizedLimit = 1
	}
	if normalizedLimit > 500 {
		normalizedLimit = 500
	}

	failedKey := queueName + ":failed"
	unprocessableKey := failedKey + ":unprocessable"
	result := DeadLetterRedriveResult{
		QueueKind: queueKind,
		Requested: normalizedLimit,
	}

	for processed := 0; processed < normalizedLimit; processed++ {
		failedEntry, err := p.client.RPop(ctx, failedKey).Result()
		if errors.Is(err, redis.Nil) {
			break
		}
		if err != nil {
			return result, fmt.Errorf("pop dead-letter entry: %w", err)
		}

		payload, ok := extractDeadLetterPayload(failedEntry)
		if !ok {
			if err := p.client.LPush(ctx, unprocessableKey, failedEntry).Err(); err != nil {
				return result, fmt.Errorf("store unprocessable dead-letter entry: %w", err)
			}
			result.Skipped++
			continue
		}

		if err := p.client.XAdd(ctx, &redis.XAddArgs{
			Stream: queueName,
			Values: map[string]any{
				"payload": payload,
			},
		}).Err(); err != nil {
			if restoreErr := p.client.RPush(ctx, failedKey, failedEntry).Err(); restoreErr != nil {
				return result, fmt.Errorf("redrive enqueue failed: %v (restore failed: %w)", err, restoreErr)
			}
			return result, fmt.Errorf("redrive enqueue failed: %w", err)
		}

		result.Redriven++
	}

	remainingFailed, err := p.client.LLen(ctx, failedKey).Result()
	if err != nil && err != redis.Nil {
		return result, fmt.Errorf("remaining dead-letter depth: %w", err)
	}
	if errors.Is(err, redis.Nil) {
		remainingFailed = 0
	}
	result.RemainingFailed = remainingFailed

	return result, nil
}

func (p *RedisProducer) ListDeadLetters(ctx context.Context, queueKind DeadLetterQueueKind, scope DeadLetterScope, offset int, limit int) (DeadLetterListResult, error) {
	if err := p.ensureStreamQueues(ctx); err != nil {
		return DeadLetterListResult{}, err
	}

	queueName, err := p.deadLetterQueueName(queueKind)
	if err != nil {
		return DeadLetterListResult{}, err
	}

	normalizedLimit := limit
	if normalizedLimit < 1 {
		normalizedLimit = 25
	}
	if normalizedLimit > 200 {
		normalizedLimit = 200
	}
	normalizedOffset := offset
	if normalizedOffset < 0 {
		normalizedOffset = 0
	}

	scopeKey, err := deadLetterScopeKey(queueName, scope)
	if err != nil {
		return DeadLetterListResult{}, err
	}
	rows, err := p.client.LRange(
		ctx,
		scopeKey,
		int64(normalizedOffset),
		int64(normalizedOffset+normalizedLimit-1),
	).Result()
	if err != nil && err != redis.Nil {
		return DeadLetterListResult{}, fmt.Errorf("list dead-letter entries: %w", err)
	}
	if errors.Is(err, redis.Nil) {
		rows = []string{}
	}

	total, err := p.client.LLen(ctx, scopeKey).Result()
	if err != nil && err != redis.Nil {
		return DeadLetterListResult{}, fmt.Errorf("dead-letter depth: %w", err)
	}
	if errors.Is(err, redis.Nil) {
		total = 0
	}

	unparsable := int64(0)
	if scope == DeadLetterScopeUnprocessable {
		unparsable = total
	} else {
		unparsable, err = p.client.LLen(ctx, queueName+":failed:unprocessable").Result()
		if err != nil && err != redis.Nil {
			return DeadLetterListResult{}, fmt.Errorf("dead-letter unparsable depth: %w", err)
		}
		if errors.Is(err, redis.Nil) {
			unparsable = 0
		}
	}

	entries := make([]DeadLetterEntry, 0, len(rows))
	for _, raw := range rows {
		entries = append(entries, parseDeadLetterEntry(raw))
	}

	return DeadLetterListResult{
		QueueKind:  queueKind,
		Scope:      scope,
		Offset:     normalizedOffset,
		Limit:      normalizedLimit,
		Total:      total,
		Entries:    entries,
		Unparsable: unparsable,
	}, nil
}

func (p *RedisProducer) PurgeDeadLetters(ctx context.Context, queueKind DeadLetterQueueKind, scope DeadLetterScope, limit int) (DeadLetterPurgeResult, error) {
	if err := p.ensureStreamQueues(ctx); err != nil {
		return DeadLetterPurgeResult{}, err
	}

	queueName, err := p.deadLetterQueueName(queueKind)
	if err != nil {
		return DeadLetterPurgeResult{}, err
	}

	key, err := deadLetterScopeKey(queueName, scope)
	if err != nil {
		return DeadLetterPurgeResult{}, err
	}

	normalizedLimit := limit
	if normalizedLimit < 1 {
		normalizedLimit = 1
	}
	if normalizedLimit > 500 {
		normalizedLimit = 500
	}

	deleted := 0
	for attempt := 0; attempt < normalizedLimit; attempt++ {
		_, popErr := p.client.RPop(ctx, key).Result()
		if errors.Is(popErr, redis.Nil) {
			break
		}
		if popErr != nil {
			return DeadLetterPurgeResult{}, fmt.Errorf("purge dead-letter entry: %w", popErr)
		}
		deleted++
	}

	remaining, err := p.client.LLen(ctx, key).Result()
	if err != nil && err != redis.Nil {
		return DeadLetterPurgeResult{}, fmt.Errorf("dead-letter remaining depth: %w", err)
	}
	if errors.Is(err, redis.Nil) {
		remaining = 0
	}

	return DeadLetterPurgeResult{
		QueueKind: queueKind,
		Scope:     scope,
		Requested: normalizedLimit,
		Deleted:   deleted,
		Remaining: remaining,
	}, nil
}

func (p *RedisProducer) deadLetterQueueName(queueKind DeadLetterQueueKind) (string, error) {
	switch queueKind {
	case DeadLetterQueueReplay:
		return p.replayQueueName, nil
	case DeadLetterQueueAnalysis:
		return p.analysisQueueName, nil
	default:
		return "", fmt.Errorf("unsupported queue kind %q", queueKind)
	}
}

func deadLetterScopeKey(queueName string, scope DeadLetterScope) (string, error) {
	switch scope {
	case DeadLetterScopeFailed:
		return queueName + ":failed", nil
	case DeadLetterScopeUnprocessable:
		return queueName + ":failed:unprocessable", nil
	default:
		return "", fmt.Errorf("unsupported dead-letter scope %q", scope)
	}
}

func extractDeadLetterPayload(failedEntry string) (string, bool) {
	trimmed := strings.TrimSpace(failedEntry)
	if trimmed == "" {
		return "", false
	}

	envelope := struct {
		Payload string `json:"payload"`
	}{}
	if err := json.Unmarshal([]byte(trimmed), &envelope); err == nil {
		payload := strings.TrimSpace(envelope.Payload)
		if payload != "" {
			return payload, true
		}
	}

	payloadCandidate := map[string]any{}
	if err := json.Unmarshal([]byte(trimmed), &payloadCandidate); err != nil {
		return "", false
	}
	_, hasProjectID := payloadCandidate["projectId"]
	_, hasSessionID := payloadCandidate["sessionId"]
	if hasProjectID && hasSessionID {
		return trimmed, true
	}
	return "", false
}

func parseDeadLetterEntry(raw string) DeadLetterEntry {
	entry := DeadLetterEntry{
		Payload: strings.TrimSpace(raw),
		Raw:     raw,
	}
	if strings.TrimSpace(raw) == "" {
		return entry
	}

	envelope := struct {
		FailedAt string `json:"failedAt"`
		Error    string `json:"error"`
		Attempt  int    `json:"attempt"`
		Payload  string `json:"payload"`
	}{}
	if err := json.Unmarshal([]byte(raw), &envelope); err == nil {
		entry.FailedAt = strings.TrimSpace(envelope.FailedAt)
		entry.Error = strings.TrimSpace(envelope.Error)
		entry.Attempt = envelope.Attempt
		payload := strings.TrimSpace(envelope.Payload)
		if payload != "" {
			entry.Payload = payload
			_ = fillDeadLetterJobMetadata(&entry, payload)
			return entry
		}
	}

	_ = fillDeadLetterJobMetadata(&entry, entry.Payload)
	return entry
}

func fillDeadLetterJobMetadata(entry *DeadLetterEntry, payload string) bool {
	trimmed := strings.TrimSpace(payload)
	if trimmed == "" {
		return false
	}

	payloadCandidate := map[string]any{}
	if err := json.Unmarshal([]byte(trimmed), &payloadCandidate); err != nil {
		return false
	}

	projectID := mapString(payloadCandidate, "projectId")
	sessionID := mapString(payloadCandidate, "sessionId")
	triggerKind := mapString(payloadCandidate, "triggerKind")
	route := mapString(payloadCandidate, "route")
	site := mapString(payloadCandidate, "site")
	if projectID == "" && sessionID == "" && triggerKind == "" && route == "" && site == "" {
		return false
	}

	entry.ProjectID = projectID
	entry.SessionID = sessionID
	entry.TriggerKind = triggerKind
	entry.Route = route
	entry.Site = site
	return true
}

func mapString(values map[string]any, key string) string {
	value, ok := values[key]
	if !ok {
		return ""
	}
	stringValue, ok := value.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(stringValue)
}

func (p *RedisProducer) ensureStreamQueues(ctx context.Context) error {
	p.ensureMu.Lock()
	defer p.ensureMu.Unlock()

	if p.queuesEnsured {
		return nil
	}

	if err := p.ensureStreamQueue(ctx, p.replayQueueName); err != nil {
		return fmt.Errorf("ensure replay queue stream: %w", err)
	}
	if err := p.ensureStreamQueue(ctx, p.analysisQueueName); err != nil {
		return fmt.Errorf("ensure analysis queue stream: %w", err)
	}

	p.queuesEnsured = true
	return nil
}

func (p *RedisProducer) ensureStreamQueue(ctx context.Context, queueName string) error {
	queueType, err := p.client.Type(ctx, queueName).Result()
	if err != nil {
		return err
	}

	switch queueType {
	case "none", "stream":
		return nil
	case "list":
		legacyQueueName := fmt.Sprintf("%s:legacy:list:%d", queueName, time.Now().UTC().UnixNano())
		if err := p.client.Rename(ctx, queueName, legacyQueueName).Err(); err != nil {
			if err == redis.Nil {
				return nil
			}
			return fmt.Errorf("rename legacy queue: %w", err)
		}

		migrated := 0
		for {
			payload, err := p.client.RPop(ctx, legacyQueueName).Result()
			if err == redis.Nil {
				break
			}
			if err != nil {
				return fmt.Errorf("read legacy queue: %w", err)
			}
			if err := p.client.XAdd(ctx, &redis.XAddArgs{
				Stream: queueName,
				Values: map[string]any{
					"payload": payload,
				},
			}).Err(); err != nil {
				return fmt.Errorf("append migrated stream entry: %w", err)
			}
			migrated++
		}

		if err := p.client.Del(ctx, legacyQueueName).Err(); err != nil {
			return fmt.Errorf("cleanup legacy queue key: %w", err)
		}
		if migrated > 0 {
			log.Printf("migrated legacy list queue to stream queue=%s entries=%d", queueName, migrated)
		}
		return nil
	default:
		return fmt.Errorf("unsupported redis key type=%s", queueType)
	}
}

type queueDepth struct {
	streamDepth  int64
	pendingDepth int64
	retryDepth   int64
	failedDepth  int64
}

func (p *RedisProducer) queueStatsFor(ctx context.Context, queueName string) (queueDepth, error) {
	streamDepth, err := p.client.XLen(ctx, queueName).Result()
	if err != nil && err != redis.Nil {
		return queueDepth{}, err
	}
	if errors.Is(err, redis.Nil) {
		streamDepth = 0
	}

	pendingDepth := int64(0)
	groupName := fmt.Sprintf("%s:group", queueName)
	pendingResult, pendingErr := p.client.XPending(ctx, queueName, groupName).Result()
	if pendingErr == nil {
		pendingDepth = pendingResult.Count
	} else {
		details := pendingErr.Error()
		if details != "NOGROUP No such key '"+queueName+"' or consumer group '"+groupName+"'" &&
			details != "ERR The XGROUP subcommand requires the key to exist" {
			return queueDepth{}, pendingErr
		}
	}

	retryDepth, err := p.client.ZCard(ctx, queueName+":retry").Result()
	if err != nil && err != redis.Nil {
		return queueDepth{}, err
	}
	if errors.Is(err, redis.Nil) {
		retryDepth = 0
	}

	failedDepth, err := p.client.LLen(ctx, queueName+":failed").Result()
	if err != nil && err != redis.Nil {
		return queueDepth{}, err
	}
	if errors.Is(err, redis.Nil) {
		failedDepth = 0
	}

	return queueDepth{
		streamDepth:  streamDepth,
		pendingDepth: pendingDepth,
		retryDepth:   retryDepth,
		failedDepth:  failedDepth,
	}, nil
}
