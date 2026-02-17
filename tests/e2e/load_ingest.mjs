#!/usr/bin/env node

import { performance } from "node:perf_hooks";

const apiBaseUrl = (process.env.RETROSPEC_API_BASE_URL ?? "http://localhost:8080").replace(/\/+$/, "");
const ingestApiKey = process.env.RETROSPEC_INGEST_API_KEY ?? "";
const totalRequests = Math.max(1, Number(process.env.RETROSPEC_LOAD_TOTAL ?? 100));
const concurrency = Math.max(1, Number(process.env.RETROSPEC_LOAD_CONCURRENCY ?? 10));

function headers() {
  const result = {
    "Content-Type": "application/json",
  };
  if (ingestApiKey.trim()) {
    result["X-Retrospec-Key"] = ingestApiKey.trim();
  }
  return result;
}

async function request(path, options = {}) {
  const response = await fetch(`${apiBaseUrl}${path}`, {
    method: "GET",
    ...options,
    headers: {
      ...(options.headers ?? {}),
      ...headers(),
    },
  });
  const payload = await response.json().catch(() => null);
  if (!response.ok) {
    throw new Error(`HTTP ${response.status} ${path} failed: ${JSON.stringify(payload)}`);
  }
  return payload;
}

function buildEvents() {
  const ts = Date.now();
  return [
    { type: 2, timestamp: ts },
    { type: 3, timestamp: ts + 200 },
  ];
}

function sessionID(index) {
  return `load_${Date.now()}_${index}_${Math.random().toString(36).slice(2, 8)}`;
}

function percentile(sortedValues, p) {
  if (sortedValues.length === 0) {
    return 0;
  }
  const index = Math.ceil((p / 100) * sortedValues.length) - 1;
  const safe = Math.max(0, Math.min(sortedValues.length - 1, index));
  return sortedValues[safe];
}

async function runOne(index) {
  const started = performance.now();
  const sid = sessionID(index);
  const eventsPayload = await request("/v1/artifacts/session-events", {
    method: "POST",
    body: JSON.stringify({
      sessionId: sid,
      site: "load.example.local",
      events: buildEvents(),
    }),
  });

  await request("/v1/ingest/session", {
    method: "POST",
    body: JSON.stringify({
      session: {
        id: sid,
        site: "load.example.local",
        route: "/load/checkout",
        startedAt: new Date().toISOString(),
        durationMs: 3000,
        eventsObjectKey: eventsPayload.eventsObjectKey,
      },
      markers: [
        {
          id: `marker_${sid}`,
          clusterKey: "load:checkout",
          label: "Load synthetic checkout delay",
          replayOffsetMs: 900,
          kind: "ui_no_effect",
          evidence: "load script",
        },
      ],
    }),
  });

  return performance.now() - started;
}

async function run() {
  const latencies = [];
  let failures = 0;
  let startedIndex = 0;

  async function workerLoop() {
    while (true) {
      const index = startedIndex;
      startedIndex += 1;
      if (index >= totalRequests) {
        break;
      }
      try {
        const latency = await runOne(index);
        latencies.push(latency);
      } catch (error) {
        failures += 1;
        console.error(`[load] request ${index + 1} failed: ${error instanceof Error ? error.message : String(error)}`);
      }
    }
  }

  console.log(`[load] api=${apiBaseUrl} total=${totalRequests} concurrency=${concurrency}`);
  const startedAt = performance.now();
  await Promise.all(Array.from({ length: concurrency }, () => workerLoop()));
  const elapsedMs = performance.now() - startedAt;

  latencies.sort((a, b) => a - b);
  const success = latencies.length;
  const rps = success > 0 ? (success * 1000) / elapsedMs : 0;

  console.log(`[load] completed in ${(elapsedMs / 1000).toFixed(2)}s`);
  console.log(`[load] success=${success} failures=${failures} throughput=${rps.toFixed(2)} req/s`);
  if (latencies.length > 0) {
    console.log(`[load] latency p50=${percentile(latencies, 50).toFixed(1)}ms p95=${percentile(latencies, 95).toFixed(1)}ms p99=${percentile(latencies, 99).toFixed(1)}ms`);
  }

  if (failures > 0) {
    process.exit(1);
  }
}

run().catch((error) => {
  console.error(`[load] FAIL: ${error instanceof Error ? error.message : String(error)}`);
  process.exit(1);
});
