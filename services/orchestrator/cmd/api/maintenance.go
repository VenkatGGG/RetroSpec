package main

import (
	"context"
	"errors"
	"log"
	"time"

	"retrospec/services/orchestrator/internal/artifacts"
	"retrospec/services/orchestrator/internal/store"
)

func startMaintenanceLoops(
	ctx context.Context,
	db *store.Postgres,
	artifactStore artifacts.Store,
	cleanupInterval time.Duration,
	promoteInterval time.Duration,
	retentionDays int,
	promoteMinSessions int,
) {
	if cleanupInterval > 0 {
		go runCleanupLoop(ctx, db, artifactStore, cleanupInterval, retentionDays)
	}
	if promoteInterval > 0 {
		go runPromotionLoop(ctx, db, promoteInterval, promoteMinSessions)
	}
}

func runCleanupLoop(
	ctx context.Context,
	db *store.Postgres,
	artifactStore artifacts.Store,
	interval time.Duration,
	retentionDays int,
) {
	runCleanupCycle(ctx, db, artifactStore, retentionDays)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			runCleanupCycle(ctx, db, artifactStore, retentionDays)
		}
	}
}

func runPromotionLoop(
	ctx context.Context,
	db *store.Postgres,
	interval time.Duration,
	minSessions int,
) {
	runPromotionCycle(ctx, db, minSessions)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			runPromotionCycle(ctx, db, minSessions)
		}
	}
}

func runCleanupCycle(
	ctx context.Context,
	db *store.Postgres,
	artifactStore artifacts.Store,
	retentionDays int,
) {
	cycleCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()

	projects, err := db.ListProjects(cycleCtx)
	if err != nil {
		log.Printf("auto-cleanup failed loading projects: %v", err)
		return
	}

	totalSessions := 0
	totalEventObjects := 0
	totalArtifactObjects := 0
	totalFailures := 0

	for _, project := range projects {
		result, err := db.CleanupExpiredData(cycleCtx, project.ID, retentionDays)
		if err != nil {
			log.Printf("auto-cleanup failed project=%s err=%v", project.ID, err)
			totalFailures++
			continue
		}

		for _, objectKey := range result.DeletedEventObjectKeys {
			err := artifactStore.DeleteObject(cycleCtx, objectKey)
			if err != nil && !errors.Is(err, artifacts.ErrNotConfigured) {
				totalFailures++
				log.Printf("auto-cleanup failed deleting event object key=%s err=%v", objectKey, err)
			}
		}
		for _, objectKey := range result.DeletedArtifactObjectKeys {
			err := artifactStore.DeleteObject(cycleCtx, objectKey)
			if err != nil && !errors.Is(err, artifacts.ErrNotConfigured) {
				totalFailures++
				log.Printf("auto-cleanup failed deleting artifact object key=%s err=%v", objectKey, err)
			}
		}

		totalSessions += result.DeletedSessions
		totalEventObjects += result.DeletedEventObjects
		totalArtifactObjects += result.DeletedArtifactObjects
	}

	log.Printf(
		"auto-cleanup completed sessions=%d eventObjects=%d artifactObjects=%d failures=%d",
		totalSessions,
		totalEventObjects,
		totalArtifactObjects,
		totalFailures,
	)
}

func runPromotionCycle(ctx context.Context, db *store.Postgres, minSessions int) {
	cycleCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()

	projects, err := db.ListProjects(cycleCtx)
	if err != nil {
		log.Printf("auto-promote failed loading projects: %v", err)
		return
	}

	totalPromoted := 0
	totalFailures := 0

	for _, project := range projects {
		result, err := db.PromoteClusters(cycleCtx, project.ID, minSessions)
		if err != nil {
			log.Printf("auto-promote failed project=%s err=%v", project.ID, err)
			totalFailures++
			continue
		}
		totalPromoted += len(result.Promoted)
	}

	log.Printf("auto-promote completed promoted=%d failures=%d", totalPromoted, totalFailures)
}
