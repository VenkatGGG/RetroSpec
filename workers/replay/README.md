# Replay Worker

Async worker that consumes replay jobs from Redis, validates rrweb event blobs, generates marker-centered replay metadata, and writes analysis artifacts to S3-compatible storage.

## Current Behavior

- Pulls JSON jobs from Redis list (`replay-jobs` by default).
- Loads rrweb events from object storage.
- Validates basic rrweb event structure.
- Writes marker-window artifact JSON back to storage.
- Pushes failed payloads into dead-letter queue (`replay-jobs:failed`).

## Next Step

Integrate a browser rendering pipeline (Playwright + rrweb-player) to produce optional MP4 clips or full-session render outputs for dashboard playback.
