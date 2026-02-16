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
	"retrospec/services/orchestrator/internal/config"
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

	handler := api.NewHandler(db, cfg.ClusterPromoteMinSessions)
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
