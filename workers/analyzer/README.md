# Analyzer Worker

Async worker that consumes `analysis-jobs` from Redis and writes per-session report cards back to the orchestrator.

## Current Behavior

- Consumes analysis jobs from Redis Stream (`analysis-jobs` by default) using a consumer group.
- Automatically migrates legacy Redis list queues to stream format on startup.
- Reads rrweb event JSON from S3-compatible storage.
- Receives marker hint context (label + evidence) from orchestrator jobs and uses it during report generation.
- Supports provider modes:
  - `heuristic`: deterministic report generation.
  - `dual_http`: calls separate text and visual model endpoints and merges both paths.
- `dual_http` responses are validated against a strict schema contract before being merged.
- Remote model payloads are sampled around marker windows and redacted for common sensitive tokens.
- Reports `pending -> ready/failed` status via `POST /v1/internal/analysis-reports`.
- Retries failed jobs with exponential backoff, reclaims stale pending messages (`ANALYZER_PROCESSING_STALE_SEC`), and dead-letters exhausted payloads.

## Provider Config

- `ANALYZER_PROVIDER=heuristic|dual_http`
- `ANALYZER_TEXT_MODEL_ENDPOINT` (required for `dual_http`)
- `ANALYZER_VISUAL_MODEL_ENDPOINT` (required for `dual_http`)
- `ANALYZER_MODEL_API_KEY` (optional bearer auth)
- `ANALYZER_MODEL_TIMEOUT_MS` (default 20000)
- `ANALYZER_FALLBACK_TO_HEURISTIC=true|false`
- `ANALYZER_MIN_ACCEPT_CONFIDENCE` (default 0.6)
- `ANALYZER_DISCARD_UNCERTAIN=true|false` (marks low-confidence reports as `discarded`)
