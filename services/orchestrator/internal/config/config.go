package config

import (
	"fmt"
	"os"
)

type Config struct {
	ListenAddr                string
	DatabaseURL               string
	RedisAddr                 string
	ReplayQueueName           string
	S3Region                  string
	S3Endpoint                string
	S3AccessKey               string
	S3SecretKey               string
	S3Bucket                  string
	ClusterPromoteMinSessions int
}

func Load() Config {
	port := envOrDefault("ORCHESTRATOR_PORT", "8080")

	return Config{
		ListenAddr:                ":" + port,
		DatabaseURL:               databaseURL(),
		RedisAddr:                 redisAddr(),
		ReplayQueueName:           envOrDefault("REPLAY_QUEUE_NAME", "replay-jobs"),
		S3Region:                  envOrDefault("S3_REGION", "us-east-1"),
		S3Endpoint:                os.Getenv("S3_ENDPOINT"),
		S3AccessKey:               envOrDefault("S3_ACCESS_KEY", ""),
		S3SecretKey:               envOrDefault("S3_SECRET_KEY", ""),
		S3Bucket:                  envOrDefault("S3_BUCKET", ""),
		ClusterPromoteMinSessions: envOrDefaultInt("CLUSTER_PROMOTE_MIN_SESSIONS", 2),
	}
}

func databaseURL() string {
	if value := os.Getenv("DATABASE_URL"); value != "" {
		return value
	}

	host := envOrDefault("POSTGRES_HOST", "localhost")
	port := envOrDefault("POSTGRES_PORT", "5432")
	user := envOrDefault("POSTGRES_USER", "retrospec")
	password := envOrDefault("POSTGRES_PASSWORD", "retrospec")
	database := envOrDefault("POSTGRES_DB", "retrospec")

	return fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable", user, password, host, port, database)
}

func envOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func redisAddr() string {
	host := envOrDefault("REDIS_HOST", "localhost")
	port := envOrDefault("REDIS_PORT", "6379")
	return fmt.Sprintf("%s:%s", host, port)
}

func envOrDefaultInt(key string, fallback int) int {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}

	var parsed int
	if _, err := fmt.Sscanf(value, "%d", &parsed); err != nil {
		return fallback
	}
	return parsed
}
