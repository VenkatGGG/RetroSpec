# Analyzer Worker

Async worker that consumes `analysis-jobs` from Redis and writes per-session report cards back to the orchestrator.

## Current Behavior

- Reads rrweb event JSON from S3-compatible storage.
- Supports provider modes:
  - `heuristic`: deterministic report generation.
  - `dual_http`: calls separate text and visual model endpoints and merges both paths.
- Reports `pending -> ready/failed` status via `POST /v1/internal/analysis-reports`.
- Retries failed jobs with exponential backoff and dead-letters exhausted payloads.

## Provider Config

- `ANALYZER_PROVIDER=heuristic|dual_http`
- `ANALYZER_TEXT_MODEL_ENDPOINT` (required for `dual_http`)
- `ANALYZER_VISUAL_MODEL_ENDPOINT` (required for `dual_http`)
- `ANALYZER_MODEL_API_KEY` (optional bearer auth)
- `ANALYZER_MODEL_TIMEOUT_MS` (default 20000)
- `ANALYZER_FALLBACK_TO_HEURISTIC=true|false`

## Next Step

Harden model contracts with strict JSON schemas and add redaction layers before sending remote model payloads.
