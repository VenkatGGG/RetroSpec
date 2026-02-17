package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

type RedisProducer struct {
	client            *redis.Client
	replayQueueName   string
	analysisQueueName string
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
