# Replay Worker

Async worker that consumes replay jobs from Redis, validates rrweb event blobs, generates marker-centered replay metadata, and writes replay artifacts to S3-compatible storage.

## Current Behavior

- Pulls JSON jobs from Redis list (`replay-jobs` by default).
- Loads rrweb events from object storage.
- Validates basic rrweb event structure.
- Writes marker-window artifact JSON back to storage.
- Optionally renders full-session replay video (`.webm`) with Playwright.
- Retries failures with exponential backoff, recovers in-flight jobs from a processing queue, and dead-letters exhausted payloads (`replay-jobs:failed`).

## Next Step

Move queue consumption to Redis Streams/SQS-style acknowledgements for stronger durability under worker crashes.
