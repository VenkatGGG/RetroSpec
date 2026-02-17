# Replay Worker

Async worker that consumes replay jobs from Redis, validates rrweb event blobs, generates marker-centered replay metadata, and writes replay artifacts to S3-compatible storage.

## Current Behavior

- Consumes JSON payload jobs from Redis Stream (`replay-jobs` by default) using a consumer group.
- Loads rrweb events from object storage.
- Validates basic rrweb event structure.
- Writes marker-window artifact JSON back to storage.
- Optionally renders full-session replay video (`.webm`) with Playwright.
- Retries failures with exponential backoff, reclaims stale pending messages (`REPLAY_PROCESSING_STALE_SEC`), and dead-letters exhausted payloads (`replay-jobs:failed`).
