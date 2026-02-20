# Replay Worker

Async worker that consumes replay jobs from Redis, validates rrweb event blobs, generates marker-centered replay metadata, and writes replay artifacts to S3-compatible storage.

## Current Behavior

- Consumes JSON payload jobs from Redis Stream (`replay-jobs` by default) using a consumer group.
- Automatically migrates legacy Redis list queues to stream format on startup.
- Loads rrweb events from object storage.
- Validates basic rrweb event structure.
- Writes marker-window artifact JSON back to storage.
- Optionally renders full-session replay video (`.webm`) with Playwright.
- Calls a visual model endpoint with replay-video artifact metadata and marker windows.
- Reports final visual verdicts back to orchestrator (`ready|discarded|failed`) through `/v1/internal/analysis-reports`.
- Enforces optional render spend controls (daily project/global quotas and per-project cooldown interval).
- Retries failures with exponential backoff, reclaims stale pending messages (`REPLAY_PROCESSING_STALE_SEC`), and dead-letters exhausted payloads (`replay-jobs:failed`).
