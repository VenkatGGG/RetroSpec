# Orchestrator Service

Go API service for ingesting session/event metadata, promoting repeated failures into issue clusters, and serving dashboard report data.

## Endpoints

- `GET /healthz`
- `POST /v1/ingest/session`
- `POST /v1/issues/promote`
- `GET /v1/issues`
- `GET /v1/sessions/{sessionID}`
- `GET /v1/sessions/{sessionID}/events`

## Run

1. Configure environment variables (or use values from `.env.example`).
2. Apply SQL in `db/migrations/001_init.sql`.
3. Apply SQL in `db/migrations/002_issue_cluster_representative_session.sql`.
4. Start API:

```bash
go run ./cmd/api
```

## Notes

- `POST /v1/ingest/session` persists session metadata and queues replay jobs in Redis.
- Session event payloads are loaded from S3-compatible storage via the configured `S3_*` environment variables.
