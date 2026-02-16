package api

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5"

	"retrospec/services/orchestrator/internal/artifacts"
	"retrospec/services/orchestrator/internal/queue"
	"retrospec/services/orchestrator/internal/store"
)

type Handler struct {
	artifactStore            artifacts.Store
	replayProducer           queue.Producer
	store                    *store.Postgres
	clusterPromoteMinSession int
}

func NewHandler(
	store *store.Postgres,
	replayProducer queue.Producer,
	artifactStore artifacts.Store,
	clusterPromoteMinSession int,
) *Handler {
	return &Handler{
		store:                    store,
		replayProducer:           replayProducer,
		artifactStore:            artifactStore,
		clusterPromoteMinSession: clusterPromoteMinSession,
	}
}

func (h *Handler) Router() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(15 * time.Second))

	r.Get("/healthz", h.healthz)
	r.Route("/v1", func(r chi.Router) {
		r.Post("/ingest/session", h.ingestSession)
		r.Post("/issues/promote", h.promoteIssues)
		r.Get("/issues", h.listIssues)
		r.Get("/sessions/{sessionID}", h.getSession)
		r.Get("/sessions/{sessionID}/events", h.getSessionEvents)
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

	stored, err := h.store.Ingest(r.Context(), payload)
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

func (h *Handler) promoteIssues(w http.ResponseWriter, r *http.Request) {
	result, err := h.store.PromoteClusters(r.Context(), h.clusterPromoteMinSession)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "promote failed"})
		return
	}

	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) listIssues(w http.ResponseWriter, r *http.Request) {
	clusters, err := h.store.ListIssueClusters(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "list failed"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"issues": clusters})
}

func (h *Handler) getSession(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "sessionID")
	session, err := h.loadSession(r.Context(), sessionID)
	if err != nil {
		writeLookupError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"session": session})
}

func (h *Handler) getSessionEvents(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "sessionID")
	session, err := h.loadSession(r.Context(), sessionID)
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

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func (h *Handler) loadSession(ctx context.Context, sessionID string) (store.Session, error) {
	return h.store.GetSession(ctx, sessionID)
}

func writeLookupError(w http.ResponseWriter, err error) {
	if errors.Is(err, pgx.ErrNoRows) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "session not found"})
		return
	}
	writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "lookup failed"})
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
