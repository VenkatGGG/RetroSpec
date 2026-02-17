package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
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

func (p *RedisProducer) ensureStreamQueues(ctx context.Context) error {
	p.ensureMu.Lock()
	if p.queuesEnsured {
		p.ensureMu.Unlock()
		return nil
	}
	p.ensureMu.Unlock()

	if err := p.ensureStreamQueue(ctx, p.replayQueueName); err != nil {
		return fmt.Errorf("ensure replay queue stream: %w", err)
	}
	if err := p.ensureStreamQueue(ctx, p.analysisQueueName); err != nil {
		return fmt.Errorf("ensure analysis queue stream: %w", err)
	}

	p.ensureMu.Lock()
	p.queuesEnsured = true
	p.ensureMu.Unlock()
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
