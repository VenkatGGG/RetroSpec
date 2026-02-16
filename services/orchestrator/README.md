# Orchestrator Service

Go API service for ingesting session/event metadata, promoting repeated failures into issue clusters, and serving dashboard report data.

## Endpoints

- `GET /healthz`
- `POST /v1/ingest/session`
- `POST /v1/issues/promote`
- `GET /v1/issues`
- `GET /v1/sessions/{sessionID}`

## Run

1. Configure environment variables (or use values from `.env.example`).
2. Apply SQL in `db/migrations/001_init.sql`.
3. Start API:

```bash
go run ./cmd/api
```
