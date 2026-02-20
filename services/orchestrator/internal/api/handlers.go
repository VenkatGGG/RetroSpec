package api

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"retrospec/services/orchestrator/internal/artifacts"
	"retrospec/services/orchestrator/internal/queue"
	"retrospec/services/orchestrator/internal/store"
)

type Handler struct {
	artifactStore            artifacts.Store
	replayProducer           queue.Producer
	queueStatsProvider       queue.StatsProvider
	corsAllowedOrigins       []string
	internalAPIKey           string
	ingestAPIKey             string
	store                    *store.Postgres
	clusterPromoteMinSession int
	rateLimiter              *apiRateLimiter
	metrics                  *apiMetrics
	artifactTokenSecret      string
	artifactTokenTTL         time.Duration
	sessionRetentionDays     int
}

type requestContextKey string

const (
	defaultProjectID        = "proj_default"
	projectIDContextKey     = requestContextKey("project_id")
	keyAuthenticatedContext = requestContextKey("key_authenticated")
)

func NewHandler(
	store *store.Postgres,
	replayProducer queue.Producer,
	artifactStore artifacts.Store,
	corsAllowedOrigins []string,
	internalAPIKey string,
	ingestAPIKey string,
	clusterPromoteMinSession int,
	rateLimitRequestsPerSec float64,
	rateLimitBurst int,
	artifactTokenSecret string,
	artifactTokenTTLSeconds int,
	sessionRetentionDays int,
) *Handler {
	var queueStatsProvider queue.StatsProvider
	if provider, ok := replayProducer.(queue.StatsProvider); ok {
		queueStatsProvider = provider
	}

	metrics := newAPIMetrics(queueStatsProvider)

	return &Handler{
		store:                    store,
		replayProducer:           replayProducer,
		queueStatsProvider:       queueStatsProvider,
		artifactStore:            artifactStore,
		corsAllowedOrigins:       corsAllowedOrigins,
		internalAPIKey:           internalAPIKey,
		ingestAPIKey:             ingestAPIKey,
		clusterPromoteMinSession: clusterPromoteMinSession,
		rateLimiter: newAPIRateLimiter(rateLimitRequestsPerSec, rateLimitBurst, func() {
			metrics.rateLimitedTotal.Add(1)
		}),
		metrics:              metrics,
		artifactTokenSecret:  strings.TrimSpace(artifactTokenSecret),
		artifactTokenTTL:     time.Duration(maxInt(60, artifactTokenTTLSeconds)) * time.Second,
		sessionRetentionDays: sessionRetentionDays,
	}
}

func (h *Handler) Router() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(15 * time.Second))
	if h.rateLimiter != nil {
		r.Use(h.rateLimiter.Middleware)
	}
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   h.corsAllowedOrigins,
		AllowedMethods:   []string{"GET", "POST", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-Retrospec-Key", "X-Retrospec-Internal"},
		ExposedHeaders:   []string{"Link"},
		AllowCredentials: false,
		MaxAge:           300,
	}))

	r.Get("/healthz", h.healthz)
	r.Get("/metrics", h.metrics.handleMetrics)
	r.Route("/v1", func(r chi.Router) {
		r.With(h.requireInternalAccess).Post("/internal/replay-results", h.reportReplayResult)
		r.With(h.requireInternalAccess).Post("/internal/analysis-reports", h.reportAnalysisResult)
		r.With(h.withProjectContextAllowQueryKey).Get("/sessions/{sessionID}/artifacts/{artifactType}", h.getSessionArtifact)

		r.Group(func(r chi.Router) {
			r.Use(h.withProjectContext)

			r.With(h.requireWriteAccess).Post("/artifacts/session-events", h.uploadSessionEvents)
			r.With(h.requireWriteAccess).Post("/ingest/session", h.ingestSession)
			r.With(h.requireWriteAccess).Post("/issues/promote", h.promoteIssues)
			r.Get("/issues/stats", h.listIssueStats)
			r.Get("/issues", h.listIssues)
			r.Get("/issues/{clusterKey}/sessions", h.listIssueSessions)
			r.Get("/sessions/{sessionID}", h.getSession)
			r.Get("/sessions/{sessionID}/events", h.getSessionEvents)
			r.With(h.requireKeyAccess).Get("/sessions/{sessionID}/artifacts/{artifactType}/token", h.createArtifactToken)
			r.With(h.requireWriteAccess).Post("/maintenance/cleanup", h.cleanupExpiredData)
		})
	})

	return r
}

func (h *Handler) healthz(w http.ResponseWriter, r *http.Request) {
	if err := h.store.Health(r.Context()); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "down"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) ingestSession(w http.ResponseWriter, r *http.Request) {
	payload := store.IngestPayload{}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid payload"})
		return
	}

	projectID := h.projectIDFromContext(r.Context())
	stored, err := h.store.Ingest(r.Context(), projectID, payload)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "ingest failed"})
		return
	}

	analysisQueueError := ""
	if job, ok := buildAnalysisJob(stored); ok {
		if _, err := h.store.UpsertSessionReportCard(
			r.Context(),
			stored.ProjectID,
			stored.ID,
			"pending",
			"",
			"",
			"",
			"",
			"",
			0,
			time.Now().UTC(),
		); err != nil {
			analysisQueueError = err.Error()
			h.metrics.analysisQueueErrorsTotal.Add(1)
			log.Printf("analysis report init failed session=%s err=%v", stored.ID, err)
		}
		if err := h.replayProducer.EnqueueAnalysisJob(r.Context(), job); err != nil {
			analysisQueueError = err.Error()
			h.metrics.analysisQueueErrorsTotal.Add(1)
			log.Printf("analysis job enqueue failed session=%s err=%v", stored.ID, err)
		}
	}
	h.metrics.ingestSessionsTotal.Add(1)

	writeJSON(w, http.StatusAccepted, map[string]any{
		"session":            stored,
		"analysisQueueError": analysisQueueError,
	})
}

type uploadSessionEventsRequest struct {
	SessionID string          `json:"sessionId"`
	Site      string          `json:"site"`
	Events    json.RawMessage `json:"events"`
}

type reportReplayResultRequest struct {
	ProjectID    string                 `json:"projectId"`
	SessionID    string                 `json:"sessionId"`
	ArtifactType string                 `json:"artifactType"`
	ArtifactKey  string                 `json:"artifactKey"`
	TriggerKind  string                 `json:"triggerKind"`
	Status       string                 `json:"status"`
	GeneratedAt  string                 `json:"generatedAt"`
	Windows      []store.ArtifactWindow `json:"windows"`
}

type reportAnalysisResultRequest struct {
	ProjectID          string   `json:"projectId"`
	SessionID          string   `json:"sessionId"`
	Status             string   `json:"status"`
	Symptom            string   `json:"symptom"`
	TechnicalRootCause string   `json:"technicalRootCause"`
	SuggestedFix       string   `json:"suggestedFix"`
	TextSummary        string   `json:"textSummary"`
	VisualSummary      string   `json:"visualSummary"`
	Confidence         *float64 `json:"confidence"`
	GeneratedAt        string   `json:"generatedAt"`
}

func (h *Handler) reportReplayResult(w http.ResponseWriter, r *http.Request) {
	payload := reportReplayResultRequest{}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid payload"})
		return
	}

	projectID := strings.TrimSpace(payload.ProjectID)
	sessionID := strings.TrimSpace(payload.SessionID)
	artifactKey := strings.TrimSpace(payload.ArtifactKey)
	status := strings.TrimSpace(payload.Status)
	if status == "" {
		status = "ready"
	}
	if projectID == "" || sessionID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "projectId and sessionId are required"})
		return
	}
	if artifactKey == "" && status == "ready" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "artifactKey is required for ready artifacts"})
		return
	}

	if _, err := h.loadSession(r.Context(), projectID, sessionID); err != nil {
		writeLookupError(w, err)
		return
	}

	generatedAt := time.Now().UTC()
	if candidate := strings.TrimSpace(payload.GeneratedAt); candidate != "" {
		parsedTime, err := time.Parse(time.RFC3339Nano, candidate)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "generatedAt must be RFC3339 timestamp"})
			return
		}
		generatedAt = parsedTime
	}

	artifact, err := h.store.UpsertSessionArtifact(
		r.Context(),
		projectID,
		payload.ArtifactType,
		sessionID,
		artifactKey,
		payload.TriggerKind,
		status,
		payload.Windows,
		generatedAt,
	)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "artifact upsert failed"})
		return
	}

	writeJSON(w, http.StatusAccepted, map[string]any{
		"artifact": artifact,
	})
	h.metrics.replayArtifactsTotal.Add(1)
}

func (h *Handler) reportAnalysisResult(w http.ResponseWriter, r *http.Request) {
	payload := reportAnalysisResultRequest{}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid payload"})
		return
	}

	projectID := strings.TrimSpace(payload.ProjectID)
	sessionID := strings.TrimSpace(payload.SessionID)
	if projectID == "" || sessionID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "projectId and sessionId are required"})
		return
	}

	session, err := h.loadSession(r.Context(), projectID, sessionID)
	if err != nil {
		writeLookupError(w, err)
		return
	}

	generatedAt := time.Now().UTC()
	if candidate := strings.TrimSpace(payload.GeneratedAt); candidate != "" {
		parsedTime, err := time.Parse(time.RFC3339Nano, candidate)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "generatedAt must be RFC3339 timestamp"})
			return
		}
		generatedAt = parsedTime
	}

	existing := session.ReportCard
	status := firstNonEmpty(payload.Status, valueOrEmpty(existing, func(card *store.SessionReportCard) string {
		return card.Status
	}))
	symptom := firstNonEmpty(payload.Symptom, valueOrEmpty(existing, func(card *store.SessionReportCard) string {
		return card.Symptom
	}))
	technicalRootCause := firstNonEmpty(payload.TechnicalRootCause, valueOrEmpty(existing, func(card *store.SessionReportCard) string {
		return card.TechnicalRootCause
	}))
	suggestedFix := firstNonEmpty(payload.SuggestedFix, valueOrEmpty(existing, func(card *store.SessionReportCard) string {
		return card.SuggestedFix
	}))
	textSummary := firstNonEmpty(payload.TextSummary, valueOrEmpty(existing, func(card *store.SessionReportCard) string {
		return card.TextSummary
	}))
	visualSummary := firstNonEmpty(payload.VisualSummary, valueOrEmpty(existing, func(card *store.SessionReportCard) string {
		return card.VisualSummary
	}))
	confidence := 0.0
	if existing != nil {
		confidence = existing.Confidence
	}
	if payload.Confidence != nil {
		confidence = *payload.Confidence
	}

	report, err := h.store.UpsertSessionReportCard(
		r.Context(),
		projectID,
		sessionID,
		status,
		symptom,
		technicalRootCause,
		suggestedFix,
		textSummary,
		visualSummary,
		confidence,
		generatedAt,
	)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "report upsert failed"})
		return
	}

	replayQueueError := ""
	if report.Status == "pending" {
		if job, ok := buildReplayJob(session); ok {
			if err := h.replayProducer.EnqueueReplayJob(r.Context(), job); err != nil {
				replayQueueError = err.Error()
				h.metrics.replayQueueErrorsTotal.Add(1)
				log.Printf("replay job enqueue failed session=%s err=%v", session.ID, err)
			}
		}
	} else {
		if _, err := h.store.PromoteClusters(r.Context(), projectID, h.clusterPromoteMinSession); err != nil {
			log.Printf("post-analysis cluster promote failed session=%s err=%v", sessionID, err)
		}
	}

	writeJSON(w, http.StatusAccepted, map[string]any{
		"report":           report,
		"replayQueueError": replayQueueError,
	})
	h.metrics.analysisReportsTotal.Add(1)
}

func (h *Handler) uploadSessionEvents(w http.ResponseWriter, r *http.Request) {
	payload := uploadSessionEventsRequest{}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid payload"})
		return
	}

	if len(payload.Events) == 0 || !json.Valid(payload.Events) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "events must be valid json"})
		return
	}
	sanitizedEvents, err := sanitizeEventPayload(payload.Events)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "events payload could not be sanitized"})
		return
	}

	sessionID := payload.SessionID
	if strings.TrimSpace(sessionID) == "" {
		sessionID = uuid.NewString()
	}

	projectID := h.projectIDFromContext(r.Context())
	siteKey := slugSite(payload.Site)
	objectKey := time.Now().UTC().Format("2006/01/02")
	objectKey = "session-events/" + projectID + "/" + objectKey + "/" + siteKey + "/" + sessionID + ".json"

	if err := h.artifactStore.StoreJSON(r.Context(), objectKey, sanitizedEvents); err != nil {
		if errors.Is(err, artifacts.ErrNotConfigured) {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "artifact store unavailable"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "artifact upload failed"})
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"projectId":       projectID,
		"sessionId":       sessionID,
		"eventsObjectKey": objectKey,
	})
}

func (h *Handler) promoteIssues(w http.ResponseWriter, r *http.Request) {
	projectID := h.projectIDFromContext(r.Context())
	result, err := h.store.PromoteClusters(r.Context(), projectID, h.clusterPromoteMinSession)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "promote failed"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"promoted": result.Promoted,
	})
}

func (h *Handler) listIssues(w http.ResponseWriter, r *http.Request) {
	stateFilter := strings.TrimSpace(r.URL.Query().Get("state"))
	if stateFilter != "" && stateFilter != "active" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "state must be one of: active or empty"})
		return
	}

	projectID := h.projectIDFromContext(r.Context())
	clusters, err := h.store.ListIssueClusters(r.Context(), projectID, stateFilter)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "list failed"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"issues": clusters,
		"state":  stateFilter,
	})
}

func (h *Handler) listIssueSessions(w http.ResponseWriter, r *http.Request) {
	clusterKey := strings.TrimSpace(chi.URLParam(r, "clusterKey"))
	if clusterKey == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "clusterKey is required"})
		return
	}

	limit := 30
	if candidate := strings.TrimSpace(r.URL.Query().Get("limit")); candidate != "" {
		parsed, err := strconv.Atoi(candidate)
		if err != nil || parsed < 1 || parsed > 200 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "limit must be an integer between 1 and 200"})
			return
		}
		limit = parsed
	}
	reportStatus := strings.TrimSpace(r.URL.Query().Get("reportStatus"))
	if reportStatus != "" && reportStatus != "pending" && reportStatus != "ready" && reportStatus != "failed" && reportStatus != "discarded" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "reportStatus must be one of: pending, ready, failed, discarded"})
		return
	}
	minConfidence := 0.0
	if candidate := strings.TrimSpace(r.URL.Query().Get("minConfidence")); candidate != "" {
		parsed, err := strconv.ParseFloat(candidate, 64)
		if err != nil || parsed < 0 || parsed > 1 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "minConfidence must be a number between 0 and 1"})
			return
		}
		minConfidence = parsed
	}

	projectID := h.projectIDFromContext(r.Context())
	sessions, err := h.store.ListIssueClusterSessions(
		r.Context(),
		projectID,
		clusterKey,
		reportStatus,
		minConfidence,
		limit,
	)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "cluster sessions lookup failed"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"clusterKey": clusterKey,
		"limit":      limit,
		"filters": map[string]any{
			"reportStatus":  reportStatus,
			"minConfidence": minConfidence,
		},
		"sessions": sessions,
	})
}

func (h *Handler) listIssueStats(w http.ResponseWriter, r *http.Request) {
	lookbackHours := 24
	if candidate := strings.TrimSpace(r.URL.Query().Get("hours")); candidate != "" {
		parsed, err := strconv.Atoi(candidate)
		if err != nil || parsed < 1 || parsed > 24*30 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "hours must be an integer between 1 and 720"})
			return
		}
		lookbackHours = parsed
	}

	projectID := h.projectIDFromContext(r.Context())
	stats, err := h.store.ListIssueKindStats(r.Context(), projectID, time.Duration(lookbackHours)*time.Hour)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "stats lookup failed"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"lookbackHours": lookbackHours,
		"stats":         stats,
	})
}

func (h *Handler) cleanupExpiredData(w http.ResponseWriter, r *http.Request) {
	projectID := h.projectIDFromContext(r.Context())
	result, err := h.store.CleanupExpiredData(r.Context(), projectID, h.sessionRetentionDays)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "cleanup failed"})
		return
	}

	for _, objectKey := range result.DeletedEventObjectKeys {
		err := h.artifactStore.DeleteObject(r.Context(), objectKey)
		if err != nil && !errors.Is(err, artifacts.ErrNotConfigured) {
			result.FailedEventObjectDelete++
			log.Printf("failed to delete event object key=%s err=%v", objectKey, err)
		}
	}
	for _, objectKey := range result.DeletedArtifactObjectKeys {
		err := h.artifactStore.DeleteObject(r.Context(), objectKey)
		if err != nil && !errors.Is(err, artifacts.ErrNotConfigured) {
			result.FailedArtifactObjectDelete++
			log.Printf("failed to delete artifact object key=%s err=%v", objectKey, err)
		}
	}
	h.metrics.cleanupRunsTotal.Add(1)
	h.metrics.cleanupEventObjectsTotal.Add(int64(result.DeletedEventObjects))
	h.metrics.cleanupArtifactObjectsTotal.Add(int64(result.DeletedArtifactObjects))

	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) getSession(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "sessionID")
	session, err := h.loadSession(r.Context(), h.projectIDFromContext(r.Context()), sessionID)
	if err != nil {
		writeLookupError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"session": session})
}

func (h *Handler) getSessionEvents(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "sessionID")
	session, err := h.loadSession(r.Context(), h.projectIDFromContext(r.Context()), sessionID)
	if err != nil {
		writeLookupError(w, err)
		return
	}

	if session.EventsObjectKey == "" {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "session has no events object key"})
		return
	}

	eventsJSON, err := h.artifactStore.LoadJSON(r.Context(), session.EventsObjectKey)
	if err != nil {
		if errors.Is(err, artifacts.ErrNotConfigured) {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "artifact store unavailable"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "unable to load session events"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"sessionId":       session.ID,
		"eventsObjectKey": session.EventsObjectKey,
		"events":          eventsJSON,
	})
}

func (h *Handler) createArtifactToken(w http.ResponseWriter, r *http.Request) {
	if !h.hasArtifactTokenSecret() {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "artifact token secret not configured"})
		return
	}

	sessionID := chi.URLParam(r, "sessionID")
	artifactType := strings.TrimSpace(chi.URLParam(r, "artifactType"))
	if artifactType == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "artifactType is required"})
		return
	}

	projectID := h.projectIDFromContext(r.Context())
	session, err := h.loadSession(r.Context(), projectID, sessionID)
	if err != nil {
		writeLookupError(w, err)
		return
	}

	artifact, found := findArtifactByType(session.Artifacts, artifactType)
	if !found || strings.TrimSpace(artifact.ArtifactKey) == "" {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "artifact not found"})
		return
	}

	expiresAt := time.Now().UTC().Add(h.artifactTokenTTL)
	token, err := h.signArtifactToken(projectID, sessionID, artifactType, expiresAt)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "token generation failed"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"token":     token,
		"expiresAt": expiresAt.Format(time.RFC3339),
	})
}

func (h *Handler) getSessionArtifact(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "sessionID")
	artifactType := strings.TrimSpace(chi.URLParam(r, "artifactType"))
	if artifactType == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "artifactType is required"})
		return
	}

	projectID := h.projectIDFromContext(r.Context())
	artifactToken := strings.TrimSpace(r.URL.Query().Get("artifactToken"))
	if artifactToken != "" {
		claims, err := h.verifyArtifactToken(artifactToken)
		if err != nil {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid artifact token"})
			return
		}
		if claims.SessionID != sessionID || claims.ArtifactType != artifactType {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "artifact token scope mismatch"})
			return
		}
		projectID = claims.ProjectID
	} else if strings.TrimSpace(h.ingestAPIKey) != "" && !h.keyAuthenticatedFromContext(r.Context()) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	session, err := h.loadSession(r.Context(), projectID, sessionID)
	if err != nil {
		writeLookupError(w, err)
		return
	}

	artifact, found := findArtifactByType(session.Artifacts, artifactType)
	if !found || strings.TrimSpace(artifact.ArtifactKey) == "" {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "artifact not found"})
		return
	}

	content, contentType, err := h.artifactStore.LoadObject(r.Context(), artifact.ArtifactKey)
	if err != nil {
		if errors.Is(err, artifacts.ErrNotConfigured) {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "artifact store unavailable"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "unable to load artifact"})
		return
	}

	if contentType == "" {
		contentType = "application/octet-stream"
		if artifactType == "analysis_json" {
			contentType = "application/json"
		}
	}

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "private, max-age=60")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(content)
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func (h *Handler) loadSession(ctx context.Context, projectID, sessionID string) (store.Session, error) {
	return h.store.GetSession(ctx, projectID, sessionID)
}

func writeLookupError(w http.ResponseWriter, err error) {
	if errors.Is(err, pgx.ErrNoRows) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "session not found"})
		return
	}
	writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "lookup failed"})
}

func findArtifactByType(artifacts []store.SessionArtifact, artifactType string) (store.SessionArtifact, bool) {
	for _, artifact := range artifacts {
		if artifact.ArtifactType == artifactType {
			return artifact, true
		}
	}
	return store.SessionArtifact{}, false
}

func buildReplayJob(session store.Session) (queue.ReplayJob, bool) {
	if session.EventsObjectKey == "" || len(session.Markers) == 0 {
		return queue.ReplayJob{}, false
	}

	offsets := make([]int, 0, len(session.Markers))
	triggerKind := "ui_no_effect"

	for _, marker := range session.Markers {
		offsets = append(offsets, marker.ReplayOffsetMs)
		triggerKind = strongerTrigger(triggerKind, marker.Kind)
	}

	return queue.ReplayJob{
		ProjectID:       session.ProjectID,
		SessionID:       session.ID,
		EventsObjectKey: session.EventsObjectKey,
		MarkerOffsetsMs: offsets,
		TriggerKind:     triggerKind,
		Route:           session.Route,
		Site:            session.Site,
	}, true
}

func buildAnalysisJob(session store.Session) (queue.AnalysisJob, bool) {
	if session.EventsObjectKey == "" || len(session.Markers) == 0 {
		return queue.AnalysisJob{}, false
	}

	offsets := make([]int, 0, len(session.Markers))
	hints := make([]string, 0, len(session.Markers))
	triggerKind := "ui_no_effect"

	for _, marker := range session.Markers {
		offsets = append(offsets, marker.ReplayOffsetMs)
		triggerKind = strongerTrigger(triggerKind, marker.Kind)
		hint := strings.TrimSpace(marker.Label)
		if evidence := strings.TrimSpace(marker.Evidence); evidence != "" {
			if hint != "" {
				hint = hint + " | " + evidence
			} else {
				hint = evidence
			}
		}
		if hint != "" {
			hints = append(hints, truncateString(hint, 220))
		}
	}

	return queue.AnalysisJob{
		ProjectID:       session.ProjectID,
		SessionID:       session.ID,
		EventsObjectKey: session.EventsObjectKey,
		MarkerOffsetsMs: offsets,
		MarkerHints:     hints,
		TriggerKind:     triggerKind,
		Route:           session.Route,
		Site:            session.Site,
	}, true
}

func strongerTrigger(current, candidate string) string {
	weight := map[string]int{
		"ui_no_effect":      1,
		"validation_failed": 2,
		"api_error":         3,
		"js_exception":      4,
	}

	if weight[candidate] > weight[current] {
		return candidate
	}
	return current
}

func (h *Handler) withProjectContext(next http.Handler) http.Handler {
	return h.withProjectContextMode(next, false)
}

func (h *Handler) withProjectContextAllowQueryKey(next http.Handler) http.Handler {
	return h.withProjectContextMode(next, true)
}

func (h *Handler) withProjectContextMode(next http.Handler, allowQueryKey bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		provided := strings.TrimSpace(r.Header.Get("X-Retrospec-Key"))
		if allowQueryKey && provided == "" {
			provided = strings.TrimSpace(r.URL.Query().Get("key"))
		}
		projectID := defaultProjectID
		authenticated := false

		switch {
		case provided == "":
			// anonymous read access falls back to default project.
		case strings.TrimSpace(h.ingestAPIKey) != "" && provided == h.ingestAPIKey:
			authenticated = true
		default:
			resolvedProjectID, err := h.store.ResolveProjectIDByAPIKey(r.Context(), provided)
			if err != nil {
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
				return
			}
			projectID = resolvedProjectID
			authenticated = true
		}

		ctx := context.WithValue(r.Context(), projectIDContextKey, projectID)
		ctx = context.WithValue(ctx, keyAuthenticatedContext, authenticated)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (h *Handler) requireWriteAccess(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.TrimSpace(h.ingestAPIKey) == "" {
			next.ServeHTTP(w, r)
			return
		}

		if h.keyAuthenticatedFromContext(r.Context()) {
			next.ServeHTTP(w, r)
			return
		}

		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
	})
}

func (h *Handler) requireKeyAccess(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if h.keyAuthenticatedFromContext(r.Context()) {
			next.ServeHTTP(w, r)
			return
		}

		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
	})
}

func (h *Handler) requireInternalAccess(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.TrimSpace(h.internalAPIKey) == "" {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "internal endpoints disabled"})
			return
		}

		provided := strings.TrimSpace(r.Header.Get("X-Retrospec-Internal"))
		if provided == h.internalAPIKey {
			next.ServeHTTP(w, r)
			return
		}

		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
	})
}

func (h *Handler) projectIDFromContext(ctx context.Context) string {
	value, ok := ctx.Value(projectIDContextKey).(string)
	if !ok || strings.TrimSpace(value) == "" {
		return defaultProjectID
	}
	return value
}

func (h *Handler) keyAuthenticatedFromContext(ctx context.Context) bool {
	value, ok := ctx.Value(keyAuthenticatedContext).(bool)
	return ok && value
}

func slugSite(site string) string {
	site = strings.TrimSpace(strings.ToLower(site))
	if site == "" {
		return "unknown-site"
	}

	builder := strings.Builder{}
	for _, ch := range site {
		isAlpha := ch >= 'a' && ch <= 'z'
		isNumber := ch >= '0' && ch <= '9'
		if isAlpha || isNumber || ch == '.' || ch == '-' {
			builder.WriteRune(ch)
			continue
		}
		builder.WriteRune('-')
	}

	slug := strings.Trim(builder.String(), "-")
	if slug == "" {
		return "unknown-site"
	}
	return slug
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func valueOrEmpty[T any](value *T, selector func(*T) string) string {
	if value == nil {
		return ""
	}
	return selector(value)
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func truncateString(value string, maxLen int) string {
	if maxLen <= 0 || len(value) <= maxLen {
		return value
	}
	return value[:maxLen]
}
