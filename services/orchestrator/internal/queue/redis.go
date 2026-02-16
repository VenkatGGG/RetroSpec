package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

type RedisProducer struct {
	client    *redis.Client
	queueName string
}

func NewRedisProducer(addr, queueName string) (*RedisProducer, error) {
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

	return &RedisProducer{client: client, queueName: queueName}, nil
}

func (p *RedisProducer) EnqueueReplayJob(ctx context.Context, job ReplayJob) error {
	payload, err := json.Marshal(job)
	if err != nil {
		return err
	}

	if err := p.client.LPush(ctx, p.queueName, payload).Err(); err != nil {
		return fmt.Errorf("enqueue replay job: %w", err)
	}
	return nil
}

func (p *RedisProducer) Close() error {
	return p.client.Close()
}
