package config

import (
	"fmt"
	"os"
	"strings"
)

type Config struct {
	ListenAddr                 string
	DatabaseURL                string
	RedisAddr                  string
	ReplayQueueName            string
	AnalysisQueueName          string
	CORSAllowedOrigins         []string
	AdminAPIKey                string
	InternalAPIKey             string
	IngestAPIKey               string
	ArtifactTokenSecret        string
	ArtifactTokenTTLSeconds    int
	RateLimitRequestsPerSec    float64
	RateLimitBurst             int
	AutoCleanupIntervalMinutes int
	AutoPromoteIntervalMinutes int
	SessionRetentionDays       int
	S3Region                   string
	S3Endpoint                 string
	S3AccessKey                string
	S3SecretKey                string
	S3Bucket                   string
	ClusterPromoteMinSessions  int
}

func Load() Config {
	port := envOrDefault("ORCHESTRATOR_PORT", "8080")

	return Config{
		ListenAddr:                 ":" + port,
		DatabaseURL:                databaseURL(),
		RedisAddr:                  redisAddr(),
		ReplayQueueName:            envOrDefault("REPLAY_QUEUE_NAME", "replay-jobs"),
		AnalysisQueueName:          envOrDefault("ANALYSIS_QUEUE_NAME", "analysis-jobs"),
		CORSAllowedOrigins:         parseCSV(envOrDefault("CORS_ALLOWED_ORIGINS", "*")),
		AdminAPIKey:                os.Getenv("ADMIN_API_KEY"),
		InternalAPIKey:             os.Getenv("INTERNAL_API_KEY"),
		IngestAPIKey:               os.Getenv("INGEST_API_KEY"),
		ArtifactTokenSecret:        artifactTokenSecret(),
		ArtifactTokenTTLSeconds:    envOrDefaultInt("ARTIFACT_TOKEN_TTL_SECONDS", 300),
		RateLimitRequestsPerSec:    envOrDefaultFloat("RATE_LIMIT_REQUESTS_PER_SEC", 25),
		RateLimitBurst:             envOrDefaultInt("RATE_LIMIT_BURST", 50),
		AutoCleanupIntervalMinutes: envOrDefaultInt("AUTO_CLEANUP_INTERVAL_MINUTES", 0),
		AutoPromoteIntervalMinutes: envOrDefaultInt("AUTO_PROMOTE_INTERVAL_MINUTES", 0),
		SessionRetentionDays:       envOrDefaultInt("SESSION_RETENTION_DAYS", 7),
		S3Region:                   envOrDefault("S3_REGION", "us-east-1"),
		S3Endpoint:                 os.Getenv("S3_ENDPOINT"),
		S3AccessKey:                envOrDefault("S3_ACCESS_KEY", ""),
		S3SecretKey:                envOrDefault("S3_SECRET_KEY", ""),
		S3Bucket:                   envOrDefault("S3_BUCKET", ""),
		ClusterPromoteMinSessions:  envOrDefaultInt("CLUSTER_PROMOTE_MIN_SESSIONS", 2),
	}
}

func artifactTokenSecret() string {
	if value := strings.TrimSpace(os.Getenv("ARTIFACT_TOKEN_SECRET")); value != "" {
		return value
	}
	if value := strings.TrimSpace(os.Getenv("INTERNAL_API_KEY")); value != "" {
		return value
	}
	return ""
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

func parseCSV(value string) []string {
	values := strings.Split(value, ",")
	result := make([]string, 0, len(values))
	for _, item := range values {
		trimmed := strings.TrimSpace(item)
		if trimmed == "" {
			continue
		}
		result = append(result, trimmed)
	}

	if len(result) == 0 {
		return []string{"*"}
	}
	return result
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

func envOrDefaultFloat(key string, fallback float64) float64 {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}

	var parsed float64
	if _, err := fmt.Sscanf(value, "%f", &parsed); err != nil {
		return fallback
	}
	return parsed
}
