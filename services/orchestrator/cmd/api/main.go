package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"retrospec/services/orchestrator/internal/api"
	"retrospec/services/orchestrator/internal/artifacts"
	"retrospec/services/orchestrator/internal/config"
	"retrospec/services/orchestrator/internal/queue"
	"retrospec/services/orchestrator/internal/store"
)

func main() {
	cfg := config.Load()

	ctx := context.Background()
	db, err := store.NewPostgres(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("database connection failed: %v", err)
	}
	defer db.Close()

	replayQueue, err := queue.NewRedisProducer(cfg.RedisAddr, cfg.ReplayQueueName, cfg.AnalysisQueueName)
	if err != nil {
		log.Printf("replay queue unavailable (%v), continuing with noop producer", err)
		replayQueue = nil
	}

	var producer queue.Producer
	if replayQueue == nil {
		producer = queue.NewNoopProducer()
	} else {
		producer = replayQueue
	}
	defer producer.Close()

	var artifactStore artifacts.Store
	if cfg.S3Bucket == "" || cfg.S3AccessKey == "" || cfg.S3SecretKey == "" {
		log.Printf("artifact store disabled: missing s3 credentials or bucket")
		artifactStore = artifacts.NewNoopStore()
	} else {
		s3Store, err := artifacts.NewS3Store(
			ctx,
			cfg.S3Region,
			cfg.S3Endpoint,
			cfg.S3AccessKey,
			cfg.S3SecretKey,
			cfg.S3Bucket,
		)
		if err != nil {
			log.Printf("artifact store unavailable (%v), continuing with noop store", err)
			artifactStore = artifacts.NewNoopStore()
		} else {
			if cfg.S3LifecycleEnabled {
				lifecycleCtx, cancelLifecycle := context.WithTimeout(ctx, 10*time.Second)
				defer cancelLifecycle()

				err := s3Store.EnsureLifecyclePolicy(
					lifecycleCtx,
					cfg.S3LifecycleExpirationDays,
					cfg.S3LifecyclePrefixes,
				)
				if err != nil {
					log.Printf(
						"unable to apply s3 lifecycle policy bucket=%s days=%d err=%v",
						cfg.S3Bucket,
						cfg.S3LifecycleExpirationDays,
						err,
					)
				} else {
					log.Printf(
						"applied s3 lifecycle policy bucket=%s days=%d prefixes=%v",
						cfg.S3Bucket,
						cfg.S3LifecycleExpirationDays,
						cfg.S3LifecyclePrefixes,
					)
				}
			}
			artifactStore = s3Store
		}
	}
	defer artifactStore.Close()

	handler := api.NewHandler(
		db,
		producer,
		artifactStore,
		cfg.CORSAllowedOrigins,
		cfg.InternalAPIKey,
		cfg.IngestAPIKey,
		cfg.ClusterPromoteMinSessions,
		cfg.AutoPromoteOnIngest,
		cfg.RateLimitRequestsPerSec,
		cfg.RateLimitBurst,
		cfg.AlertWebhookURL,
		cfg.AlertAuthHeader,
		cfg.AlertMinClusterConfidence,
		cfg.AlertCooldownMinutes,
		cfg.QueueWarningPending,
		cfg.QueueWarningRetry,
		cfg.QueueCriticalPending,
		cfg.QueueCriticalRetry,
		cfg.QueueCriticalFailed,
		cfg.ArtifactTokenSecret,
		cfg.ArtifactTokenTTLSeconds,
		cfg.SessionRetentionDays,
	)
	router := handler.Router()

	server := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
	}

	shutdownCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	startMaintenanceLoops(
		shutdownCtx,
		db,
		artifactStore,
		time.Duration(cfg.AutoCleanupIntervalMinutes)*time.Minute,
		cfg.SessionRetentionDays,
	)

	go func() {
		log.Printf("orchestrator listening on %s", cfg.ListenAddr)
		if err := server.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("server failed: %v", err)
		}
	}()

	<-shutdownCtx.Done()
	ctxTimeout, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := server.Shutdown(ctxTimeout); err != nil {
		log.Printf("graceful shutdown failed: %v", err)
	}
}
