# RetroSpec

RetroSpec is an async web reliability platform that captures browser session events, reconstructs replay artifacts, clusters repeated failures, and surfaces high-confidence issue reports in a developer dashboard.

## Product Scope (Current)

- Report recurring failures, do not auto-fix code.
- Cluster repeated issues across users/sessions before surfacing to developers.
- Show full session replay and allow jumping directly to error markers.
- Allow drilling from each promoted cluster to all matching sessions before replay inspection.
- Keep heavy artifacts short-lived (default 7-day retention).

## Monorepo Layout

- `apps/dashboard` React + Redux Toolkit dashboard.
- `services/orchestrator` Go service for ingest, clustering, and reporting.
- `workers/replay` Async replay/analysis worker (rrweb + media pipeline).
- `workers/analyzer` Async report-card worker (text-path analysis stage).
- `packages/sdk` Browser capture SDK for third-party website integration.
- `infra` Docker and local infra manifests.

## Architecture Notes

- Session capture is rrweb event-based (not raw live video in-browser).
- Replay and analysis are asynchronous server workflows with strict gating:
  - Stage 1: text-path analysis flags sessions.
  - Stage 2: replay worker renders video and asks visual model to confirm.
- Workers use Redis Streams consumer groups with stale-claim recovery for at-least-once handling in crash scenarios.
- Existing Redis list queues are auto-migrated to stream keys on startup for backward-compatible upgrades.
- Issue promotion is threshold-based (`>=2` similar confirmed sessions by default).
- Storage split:
  - PostgreSQL for metadata and issue clusters.
  - S3-compatible object storage for event blobs and optional videos.

## Quick Start (after scaffold is complete)

1. Copy `.env.example` to `.env` and set values.
2. Start local infra from `infra/docker-compose.yml`.
3. Apply schema migration:
   - `psql postgresql://retrospec:retrospec@localhost:5432/retrospec -f services/orchestrator/db/migrations/001_init.sql`
   - `psql postgresql://retrospec:retrospec@localhost:5432/retrospec -f services/orchestrator/db/migrations/002_issue_cluster_representative_session.sql`
   - `psql postgresql://retrospec:retrospec@localhost:5432/retrospec -f services/orchestrator/db/migrations/003_projects_and_project_api_keys.sql`
   - `psql postgresql://retrospec:retrospec@localhost:5432/retrospec -f services/orchestrator/db/migrations/004_session_artifacts.sql`
   - `psql postgresql://retrospec:retrospec@localhost:5432/retrospec -f services/orchestrator/db/migrations/005_session_report_cards.sql`
   - `psql postgresql://retrospec:retrospec@localhost:5432/retrospec -f services/orchestrator/db/migrations/008_error_markers_evidence.sql`
4. Start services:
   - `npm install`
   - `npx playwright install chromium` (required only if `REPLAY_RENDER_ENABLED=true`)
   - `npm run dev -w apps/dashboard`
   - `go run ./services/orchestrator/cmd/api`
   - `npm run dev -w workers/replay`
   - `npm run dev -w workers/analyzer`

## Dashboard API

Set `VITE_API_BASE_URL` to point the dashboard at your orchestrator service (default `http://localhost:8080`).
If backend write auth is enabled, set `VITE_INGEST_API_KEY` so dashboard actions can call protected endpoints.
Set `INTERNAL_API_KEY` on both orchestrator and replay worker so async replay jobs can persist artifact metadata.
Set `INTERNAL_API_KEY` on analyzer worker so report-card callbacks are authorized.
Set `ORCHESTRATOR_BASE_URL` for the replay worker callback target (default `http://localhost:8080`).
Set `ANALYSIS_QUEUE_NAME` and analyzer retry envs (`ANALYZER_MAX_ATTEMPTS`, `ANALYZER_RETRY_BASE_MS`, `ANALYZER_DEDUPE_WINDOW_SEC`) for the analyzer queue.
Analyzer supports `ANALYZER_PROVIDER=heuristic` (default) and `ANALYZER_PROVIDER=remote_text`.
For `remote_text`, configure:
- `ANALYZER_TEXT_MODEL_ENDPOINT` (logic/text path)
- `ANALYZER_MODEL_API_KEY` (optional bearer token)
- `ANALYZER_MODEL_TIMEOUT_MS` and `ANALYZER_FALLBACK_TO_HEURISTIC`
Use `ANALYZER_MIN_ACCEPT_CONFIDENCE` to decide whether a session should proceed to the replay/VLM stage.
Set `ARTIFACT_TOKEN_SECRET` to enable short-lived signed artifact playback tokens (defaults to `INTERNAL_API_KEY` if omitted).
Set `REPLAY_RENDER_ENABLED=true` on the replay worker to render full-session `.webm` assets via Playwright.
Set `REPLAY_VISUAL_MODEL_ENDPOINT` to run visual confirmation against reconstructed replay videos.
Use `REPLAY_RENDER_DAILY_LIMIT_PER_PROJECT`, `REPLAY_RENDER_DAILY_LIMIT_GLOBAL`, and `REPLAY_RENDER_MIN_INTERVAL_SEC_PER_PROJECT` to cap video render spend.
Replay worker retries failed jobs automatically (`REPLAY_MAX_ATTEMPTS`, `REPLAY_RETRY_BASE_MS`) before dead-lettering.
Replay worker deduplicates repeated payloads for a TTL window (`REPLAY_DEDUPE_WINDOW_SEC`).
Replay/analyzer workers reclaim stale in-flight jobs with `REPLAY_PROCESSING_STALE_SEC` and `ANALYZER_PROCESSING_STALE_SEC`.
API rate limiting is configurable with `RATE_LIMIT_REQUESTS_PER_SEC` and `RATE_LIMIT_BURST`.
Optional background cleanup loop can be enabled with `AUTO_CLEANUP_INTERVAL_MINUTES` (recommended for 7-day retention enforcement).
Issue trend stats are available at `GET /v1/issues/stats?hours=24`.
Session-level AI report cards are available on `GET /v1/sessions/{sessionID}` under `reportCard`.
Report cards use statuses: `pending` (awaiting visual confirmation), `ready` (confirmed), `failed`, and `discarded`.

## E2E and Load Tests

Run pipeline smoke validation against a live local stack:

- `npm run test:e2e:pipeline`

Run a basic upload+ingest load test:

- `RETROSPEC_LOAD_TOTAL=200 RETROSPEC_LOAD_CONCURRENCY=20 npm run test:load:ingest`

## Website Integration (SDK)

Use `@retrospec/sdk` on customer websites to record rrweb events and report failures:

```ts
import { initRetrospec } from "@retrospec/sdk";

const retrospec = initRetrospec({
  apiBaseUrl: "http://localhost:8080",
  apiKey: "replace-if-ingest-key-enabled",
  site: "example.com",
  maskAllInputs: true,
});

window.addEventListener("beforeunload", () => {
  void retrospec.flush();
});
```

By default, network failure markers include both `fetch` and `XMLHttpRequest` traffic.
SDK markers now include compact stack/breadcrumb evidence for JS and network failures to improve post-session diagnosis.
Server-side ingest also applies JSON redaction before events are stored in object storage.
