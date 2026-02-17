# End-to-End Tests

These scripts execute against a running RetroSpec stack.

## Prerequisites

1. Start infra and apply DB migrations.
2. Run orchestrator + replay worker + analyzer worker.
3. Set API target and optional ingest key:

```bash
export RETROSPEC_API_BASE_URL=http://localhost:8080
export RETROSPEC_INGEST_API_KEY=...
```

## Pipeline Smoke Test

```bash
npm run test:e2e:pipeline
```

What it verifies:

- session event upload
- session ingest and marker clustering
- async replay/analyzer processing completion
- issue promotion and cluster session drilldown integrity

## Basic Ingest Load Test

```bash
RETROSPEC_LOAD_TOTAL=200 RETROSPEC_LOAD_CONCURRENCY=20 npm run test:load:ingest
```

Outputs p50/p95/p99 latency plus throughput for upload+ingest request flow.
