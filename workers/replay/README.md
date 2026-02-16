# Replay Worker

Async worker that consumes replay jobs, validates rrweb event blobs, generates marker-centered replay metadata, and writes analysis artifacts to S3-compatible storage.

## Current Behavior

- Pulls jobs from BullMQ queue (`replay-jobs` by default).
- Loads rrweb events from object storage.
- Validates basic rrweb event structure.
- Writes marker-window artifact JSON back to storage.

## Next Step

Integrate a browser rendering pipeline (Playwright + rrweb-player) to produce optional MP4 clips or full-session render outputs for dashboard playback.
