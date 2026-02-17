# Analyzer Worker

Async worker that consumes `analysis-jobs` from Redis and writes per-session report cards back to the orchestrator.

## Current Behavior

- Reads rrweb event JSON from S3-compatible storage.
- Uses deterministic heuristics to infer symptom/root-cause/fix hints from trigger kind and nearby event payloads.
- Reports `pending -> ready/failed` status via `POST /v1/internal/analysis-reports`.
- Retries failed jobs with exponential backoff and dead-letters exhausted payloads.

## Next Step

Replace deterministic heuristics with LLM/VLM providers while keeping this queue contract unchanged.
