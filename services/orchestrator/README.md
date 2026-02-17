# Orchestrator Service

Go API service for ingesting session/event metadata, promoting repeated failures into issue clusters, and serving dashboard report data.

## Endpoints

- `GET /healthz`
- `POST /v1/admin/projects`
- `GET /v1/admin/projects`
- `POST /v1/admin/projects/{projectID}/keys`
- `GET /v1/admin/projects/{projectID}/keys`
- `POST /v1/internal/replay-results`
- `POST /v1/internal/analysis-reports`
- `POST /v1/artifacts/session-events`
- `POST /v1/ingest/session`
- `POST /v1/issues/promote`
- `POST /v1/issues/{clusterKey}/state`
- `GET /v1/issues`
- `GET /v1/issues/{clusterKey}/sessions`
- `GET /v1/sessions/{sessionID}`
- `GET /v1/sessions/{sessionID}/events`
- `POST /v1/maintenance/cleanup`

## Run

1. Configure environment variables (or use values from `.env.example`).
2. Apply SQL in `db/migrations/001_init.sql`.
3. Apply SQL in `db/migrations/002_issue_cluster_representative_session.sql`.
4. Apply SQL in `db/migrations/003_projects_and_project_api_keys.sql`.
5. Apply SQL in `db/migrations/004_session_artifacts.sql`.
6. Apply SQL in `db/migrations/005_session_report_cards.sql`.
7. Apply SQL in `db/migrations/006_issue_cluster_states.sql`.
8. Apply SQL in `db/migrations/007_issue_alert_events.sql`.
9. Apply SQL in `db/migrations/008_error_markers_evidence.sql`.
5. Start API:

```bash
go run ./cmd/api
```

## Notes

- `POST /v1/ingest/session` persists session metadata and queues replay jobs in Redis.
- `POST /v1/ingest/session` also queues analysis jobs and creates a pending session report-card row.
- `POST /v1/ingest/session` can auto-promote clusters immediately when `AUTO_PROMOTE_ON_INGEST=true`.
- `GET /v1/issues/{clusterKey}/sessions` returns the recent sessions mapped to a promoted cluster key, including report-card status/confidence.
  - Query params: `limit` (1-200), `reportStatus` (`pending|ready|failed|discarded`), `minConfidence` (0-1).
- `POST /v1/issues/{clusterKey}/state` updates triage workflow state (`open|acknowledged|resolved|muted`) with assignee, note, and optional mute window.
- Configure outbound alerts with `ALERT_WEBHOOK_URL` (optional), plus `ALERT_AUTH_HEADER`, `ALERT_COOLDOWN_MINUTES`, and `ALERT_MIN_CLUSTER_CONFIDENCE`.
- `POST /v1/artifacts/session-events` stores rrweb event JSON and returns `eventsObjectKey` for session ingest.
- Session event payloads are loaded from S3-compatible storage via the configured `S3_*` environment variables.
- Internal worker callbacks (`/v1/internal/*`) require `INTERNAL_API_KEY` via `X-Retrospec-Internal`.
- `POST /v1/maintenance/cleanup` removes data older than `SESSION_RETENTION_DAYS` (default 7), prunes orphan issue clusters, and deletes expired event objects from S3 when artifact storage is configured.
- CORS origins are controlled with `CORS_ALLOWED_ORIGINS` (comma-separated, default `*`) for SDK calls from customer domains.
- Data is project-scoped. Requests with `X-Retrospec-Key` are mapped to a project via `project_api_keys`.
- If `INGEST_API_KEY` is set, it remains a valid global write key for the default project.
- Admin endpoints require `ADMIN_API_KEY` via `X-Retrospec-Admin` and return raw API keys on creation.

## Project API keys

Preferred flow:

1. Create a project with `POST /v1/admin/projects`.
2. Store the returned raw API key and configure SDK/dashboard with that key.
3. Rotate keys with `POST /v1/admin/projects/{projectID}/keys`.
