# Orchestrator Service

Go API service for ingesting session/event metadata, promoting repeated failures into issue clusters, and serving dashboard report data.

## Endpoints

- `GET /healthz`
- `POST /v1/internal/replay-results`
- `POST /v1/internal/analysis-reports`
- `POST /v1/artifacts/session-events`
- `POST /v1/ingest/session`
- `POST /v1/issues/promote`
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
7. Apply SQL in `db/migrations/008_error_markers_evidence.sql`.
8. Start API:

```bash
go run ./cmd/api
```

## Notes

- `POST /v1/ingest/session` persists session metadata, creates an initial pending report-card row, and queues text analysis jobs.
- `POST /v1/internal/analysis-reports` with status `pending` triggers replay queueing for visual verification.
- Replay worker sends final visual verdicts (`ready|discarded|failed`) via `POST /v1/internal/analysis-reports`.
- Cluster promotion is based on sessions with report-card status `ready` only.
- `GET /v1/issues/{clusterKey}/sessions` returns the recent sessions mapped to a promoted cluster key, including report-card status/confidence.
  - Query params: `limit` (1-200), `reportStatus` (`pending|ready|failed|discarded`), `minConfidence` (0-1).
- `GET /v1/issues` supports optional `state` filter (`active` or empty).
- `POST /v1/artifacts/session-events` sanitizes and stores rrweb event JSON, returning `eventsObjectKey` for session ingest.
- Session event payloads are loaded from S3-compatible storage via the configured `S3_*` environment variables.
- Internal worker callbacks (`/v1/internal/*`) require `INTERNAL_API_KEY` via `X-Retrospec-Internal`.
- `POST /v1/maintenance/cleanup` removes data older than `SESSION_RETENTION_DAYS` (default 7), prunes orphan issue clusters, and deletes expired event objects from S3 when artifact storage is configured.
- CORS origins are controlled with `CORS_ALLOWED_ORIGINS` (comma-separated, default `*`) for SDK calls from customer domains.
- Data is project-scoped. Requests with `X-Retrospec-Key` are mapped to a project via `project_api_keys`.
- If `INGEST_API_KEY` is set, it remains a valid global write key for the default project.
