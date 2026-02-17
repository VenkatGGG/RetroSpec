# Analyzer Worker

Async worker that consumes `analysis-jobs` from Redis and writes per-session report cards back to the orchestrator.

## Current Behavior

- Reads rrweb event JSON from S3-compatible storage.
- Receives marker hint context (label + evidence) from orchestrator jobs and uses it during report generation.
- Supports provider modes:
  - `heuristic`: deterministic report generation.
  - `dual_http`: calls separate text and visual model endpoints and merges both paths.
- `dual_http` responses are validated against a strict schema contract before being merged.
- Remote model payloads are sampled around marker windows and redacted for common sensitive tokens.
- Reports `pending -> ready/failed` status via `POST /v1/internal/analysis-reports`.
- Retries failed jobs with exponential backoff, recovers in-flight jobs from a processing queue, and dead-letters exhausted payloads.

## Provider Config

- `ANALYZER_PROVIDER=heuristic|dual_http`
- `ANALYZER_TEXT_MODEL_ENDPOINT` (required for `dual_http`)
- `ANALYZER_VISUAL_MODEL_ENDPOINT` (required for `dual_http`)
- `ANALYZER_MODEL_API_KEY` (optional bearer auth)
- `ANALYZER_MODEL_TIMEOUT_MS` (default 20000)
- `ANALYZER_FALLBACK_TO_HEURISTIC=true|false`
- `ANALYZER_MIN_ACCEPT_CONFIDENCE` (default 0.6)
- `ANALYZER_DISCARD_UNCERTAIN=true|false` (marks low-confidence reports as `discarded`)

## Next Step

Move queue consumption to Redis Streams/SQS-style acknowledgements for stronger durability under worker crashes.
