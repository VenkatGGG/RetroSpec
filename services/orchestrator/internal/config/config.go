package config

import (
	"fmt"
	"os"
)

type Config struct {
	ListenAddr                string
	DatabaseURL               string
	ClusterPromoteMinSessions int
}

func Load() Config {
	port := envOrDefault("ORCHESTRATOR_PORT", "8080")

	return Config{
		ListenAddr:                ":" + port,
		DatabaseURL:               databaseURL(),
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
