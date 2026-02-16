package api

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
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
	corsAllowedOrigins       []string
	adminAPIKey              string
	internalAPIKey           string
	ingestAPIKey             string
	store                    *store.Postgres
	clusterPromoteMinSession int
	rateLimiter              *apiRateLimiter
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
	adminAPIKey string,
	internalAPIKey string,
	ingestAPIKey string,
	clusterPromoteMinSession int,
	rateLimitRequestsPerSec float64,
	rateLimitBurst int,
	sessionRetentionDays int,
) *Handler {
	return &Handler{
		store:                    store,
		replayProducer:           replayProducer,
		artifactStore:            artifactStore,
		corsAllowedOrigins:       corsAllowedOrigins,
		adminAPIKey:              adminAPIKey,
		internalAPIKey:           internalAPIKey,
		ingestAPIKey:             ingestAPIKey,
		clusterPromoteMinSession: clusterPromoteMinSession,
		rateLimiter:              newAPIRateLimiter(rateLimitRequestsPerSec, rateLimitBurst),
		sessionRetentionDays:     sessionRetentionDays,
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
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-Retrospec-Key", "X-Retrospec-Admin", "X-Retrospec-Internal"},
		ExposedHeaders:   []string{"Link"},
		AllowCredentials: false,
		MaxAge:           300,
	}))

	r.Get("/healthz", h.healthz)
	r.Route("/v1", func(r chi.Router) {
		r.Route("/admin", func(r chi.Router) {
			r.With(h.requireAdminAccess).Get("/projects", h.listProjects)
			r.With(h.requireAdminAccess).Post("/projects", h.createProject)
			r.With(h.requireAdminAccess).Get("/projects/{projectID}/keys", h.listProjectAPIKeys)
			r.With(h.requireAdminAccess).Post("/projects/{projectID}/keys", h.createProjectAPIKey)
		})
		r.With(h.requireInternalAccess).Post("/internal/replay-results", h.reportReplayResult)
		r.With(h.withProjectContextAllowQueryKey).Get("/sessions/{sessionID}/artifacts/{artifactType}", h.getSessionArtifact)

		r.Group(func(r chi.Router) {
			r.Use(h.withProjectContext)

			r.With(h.requireWriteAccess).Post("/artifacts/session-events", h.uploadSessionEvents)
			r.With(h.requireWriteAccess).Post("/ingest/session", h.ingestSession)
			r.With(h.requireWriteAccess).Post("/issues/promote", h.promoteIssues)
			r.Get("/issues", h.listIssues)
			r.Get("/sessions/{sessionID}", h.getSession)
			r.Get("/sessions/{sessionID}/events", h.getSessionEvents)
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

	queueError := ""
	if job, ok := buildReplayJob(stored); ok {
		if err := h.replayProducer.EnqueueReplayJob(r.Context(), job); err != nil {
			queueError = err.Error()
			log.Printf("replay job enqueue failed session=%s err=%v", stored.ID, err)
		}
	}

	writeJSON(w, http.StatusAccepted, map[string]any{
		"session":    stored,
		"queueError": queueError,
	})
}

type uploadSessionEventsRequest struct {
	SessionID string          `json:"sessionId"`
	Site      string          `json:"site"`
	Events    json.RawMessage `json:"events"`
}

type createProjectRequest struct {
	Name  string `json:"name"`
	Site  string `json:"site"`
	Label string `json:"label"`
}

func (h *Handler) listProjects(w http.ResponseWriter, r *http.Request) {
	projects, err := h.store.ListProjects(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "projects lookup failed"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"projects": projects})
}

func (h *Handler) createProject(w http.ResponseWriter, r *http.Request) {
	payload := createProjectRequest{}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid payload"})
		return
	}

	name := strings.TrimSpace(payload.Name)
	site := strings.TrimSpace(payload.Site)
	label := strings.TrimSpace(payload.Label)
	if name == "" || site == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name and site are required"})
		return
	}

	if label == "" {
		label = "default-key"
	}

	rawKey := generateRawAPIKey()
	project, err := h.store.CreateProjectWithAPIKey(r.Context(), name, site, label, rawKey)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "project creation failed"})
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"project": project,
		"apiKey":  rawKey,
	})
}

type createProjectAPIKeyRequest struct {
	Label string `json:"label"`
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

func (h *Handler) listProjectAPIKeys(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "projectID")
	keys, err := h.store.ListProjectAPIKeys(r.Context(), projectID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "project keys lookup failed"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"keys": keys})
}

func (h *Handler) createProjectAPIKey(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "projectID")
	payload := createProjectAPIKeyRequest{}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid payload"})
		return
	}

	label := strings.TrimSpace(payload.Label)
	if label == "" {
		label = "project-key"
	}

	rawKey := generateRawAPIKey()
	stored, err := h.store.CreateAPIKeyForProject(r.Context(), projectID, label, rawKey)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "api key creation failed"})
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"apiKeyId":  stored.ID,
		"projectId": stored.ProjectID,
		"label":     stored.Label,
		"apiKey":    rawKey,
	})
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
	if projectID == "" || sessionID == "" || artifactKey == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "projectId, sessionId, and artifactKey are required"})
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
		payload.Status,
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

	sessionID := payload.SessionID
	if strings.TrimSpace(sessionID) == "" {
		sessionID = uuid.NewString()
	}

	projectID := h.projectIDFromContext(r.Context())
	siteKey := slugSite(payload.Site)
	objectKey := time.Now().UTC().Format("2006/01/02")
	objectKey = "session-events/" + projectID + "/" + objectKey + "/" + siteKey + "/" + sessionID + ".json"

	if err := h.artifactStore.StoreJSON(r.Context(), objectKey, payload.Events); err != nil {
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

	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) listIssues(w http.ResponseWriter, r *http.Request) {
	projectID := h.projectIDFromContext(r.Context())
	clusters, err := h.store.ListIssueClusters(r.Context(), projectID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "list failed"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"issues": clusters})
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

func (h *Handler) getSessionArtifact(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "sessionID")
	artifactType := strings.TrimSpace(chi.URLParam(r, "artifactType"))
	if artifactType == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "artifactType is required"})
		return
	}

	session, err := h.loadSession(r.Context(), h.projectIDFromContext(r.Context()), sessionID)
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

func (h *Handler) requireAdminAccess(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.TrimSpace(h.adminAPIKey) == "" {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "admin endpoints disabled"})
			return
		}

		provided := strings.TrimSpace(r.Header.Get("X-Retrospec-Admin"))
		if provided == h.adminAPIKey {
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

func generateRawAPIKey() string {
	return "rpk_" + strings.ReplaceAll(uuid.NewString(), "-", "") + strings.ReplaceAll(uuid.NewString(), "-", "")
}
