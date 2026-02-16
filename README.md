# RetroSpec

RetroSpec is an async web reliability platform that captures browser session events, reconstructs replay artifacts, clusters repeated failures, and surfaces high-confidence issue reports in a developer dashboard.

## Product Scope (Current)

- Report recurring failures, do not auto-fix code.
- Cluster repeated issues across users/sessions before surfacing to developers.
- Show full session replay and allow jumping directly to error markers.
- Keep heavy artifacts short-lived (default 7-day retention).

## Monorepo Layout

- `apps/dashboard` React + Redux Toolkit dashboard.
- `services/orchestrator` Go service for ingest, clustering, and reporting.
- `workers/replay` Async replay/analysis worker (rrweb + media pipeline).
- `packages/sdk` Browser capture SDK for third-party website integration.
- `infra` Docker and local infra manifests.

## Architecture Notes

- Session capture is rrweb event-based (not raw live video in-browser).
- Replay and analysis are asynchronous server workflows.
- Issue promotion is threshold-based (`>=2` similar events by default).
- Storage split:
  - PostgreSQL for metadata and issue clusters.
  - S3-compatible object storage for event blobs and optional videos.

## Quick Start (after scaffold is complete)

1. Copy `.env.example` to `.env` and set values.
2. Start local infra from `infra/docker-compose.yml`.
3. Apply schema migration:
   - `psql postgresql://retrospec:retrospec@localhost:5432/retrospec -f services/orchestrator/db/migrations/001_init.sql`
   - `psql postgresql://retrospec:retrospec@localhost:5432/retrospec -f services/orchestrator/db/migrations/002_issue_cluster_representative_session.sql`
4. Start services:
   - `npm install`
   - `npm run dev -w apps/dashboard`
   - `go run ./services/orchestrator/cmd/api`
   - `npm run dev -w workers/replay`

## Dashboard API

Set `VITE_API_BASE_URL` to point the dashboard at your orchestrator service (default `http://localhost:8080`).
If backend write auth is enabled, set `VITE_INGEST_API_KEY` so dashboard actions can call protected endpoints.

## Website Integration (SDK)

Use `@retrospec/sdk` on customer websites to record rrweb events and report failures:

```ts
import { initRetrospec } from "@retrospec/sdk";

const retrospec = initRetrospec({
  apiBaseUrl: "http://localhost:8080",
  apiKey: "replace-if-ingest-key-enabled",
  site: "example.com",
});

window.addEventListener("beforeunload", () => {
  void retrospec.flush();
});
```
