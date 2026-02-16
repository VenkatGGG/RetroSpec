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

	replayQueue, err := queue.NewRedisProducer(cfg.RedisAddr, cfg.ReplayQueueName)
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
			artifactStore = s3Store
		}
	}
	defer artifactStore.Close()

	handler := api.NewHandler(db, producer, artifactStore, cfg.ClusterPromoteMinSessions)
	router := handler.Router()

	server := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
	}

	shutdownCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

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
