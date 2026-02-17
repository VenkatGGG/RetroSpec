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
