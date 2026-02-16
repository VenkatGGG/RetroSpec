# Orchestrator Service

Go API service for ingesting session/event metadata, promoting repeated failures into issue clusters, and serving dashboard report data.

## Endpoints

- `GET /healthz`
- `POST /v1/artifacts/session-events`
- `POST /v1/ingest/session`
- `POST /v1/issues/promote`
- `GET /v1/issues`
- `GET /v1/sessions/{sessionID}`
- `GET /v1/sessions/{sessionID}/events`
- `POST /v1/maintenance/cleanup`

## Run

1. Configure environment variables (or use values from `.env.example`).
2. Apply SQL in `db/migrations/001_init.sql`.
3. Apply SQL in `db/migrations/002_issue_cluster_representative_session.sql`.
4. Apply SQL in `db/migrations/003_projects_and_project_api_keys.sql`.
5. Start API:

```bash
go run ./cmd/api
```

## Notes

- `POST /v1/ingest/session` persists session metadata and queues replay jobs in Redis.
- `POST /v1/artifacts/session-events` stores rrweb event JSON and returns `eventsObjectKey` for session ingest.
- Session event payloads are loaded from S3-compatible storage via the configured `S3_*` environment variables.
- `POST /v1/maintenance/cleanup` removes data older than `SESSION_RETENTION_DAYS` (default 7), prunes orphan issue clusters, and deletes expired event objects from S3 when artifact storage is configured.
- CORS origins are controlled with `CORS_ALLOWED_ORIGINS` (comma-separated, default `*`) for SDK calls from customer domains.
- Data is project-scoped. Requests with `X-Retrospec-Key` are mapped to a project via `project_api_keys`.
- If `INGEST_API_KEY` is set, it remains a valid global write key for the default project.

## Project API keys

Create keys by inserting SHA-256 hashes into `project_api_keys`. Example hash command:

```bash
echo -n \"your_raw_key\" | shasum -a 256
```
