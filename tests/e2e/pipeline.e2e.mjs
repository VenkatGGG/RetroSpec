#!/usr/bin/env node

const apiBaseUrl = (process.env.RETROSPEC_API_BASE_URL ?? "http://localhost:8080").replace(/\/+$/, "");
const ingestApiKey = process.env.RETROSPEC_INGEST_API_KEY ?? "";
const timeoutMs = Number(process.env.RETROSPEC_E2E_TIMEOUT_MS ?? 120_000);
const pollIntervalMs = Number(process.env.RETROSPEC_E2E_POLL_MS ?? 3_000);

function nowIso() {
  return new Date().toISOString();
}

function randomID(prefix) {
  const token = Math.random().toString(36).slice(2, 10);
  return `${prefix}_${Date.now()}_${token}`;
}

function buildHeaders() {
  const headers = {
    "Content-Type": "application/json",
  };
  if (ingestApiKey.trim()) {
    headers["X-Retrospec-Key"] = ingestApiKey.trim();
  }
  return headers;
}

async function apiRequest(path, options = {}) {
  const response = await fetch(`${apiBaseUrl}${path}`, {
    method: "GET",
    ...options,
    headers: {
      ...(options.headers ?? {}),
      ...buildHeaders(),
    },
  });

  let body;
  try {
    body = await response.json();
  } catch {
    body = null;
  }

  if (!response.ok) {
    throw new Error(`HTTP ${response.status} ${path} failed: ${JSON.stringify(body)}`);
  }

  return body;
}

function buildEvents(offsetMs) {
  const base = Date.now() + offsetMs;
  return [
    {
      type: 2,
      timestamp: base,
    },
    {
      type: 3,
      timestamp: base + 250,
    },
  ];
}

async function uploadSessionEvents(sessionId, site, offsetMs) {
  const body = {
    sessionId,
    site,
    events: buildEvents(offsetMs),
  };
  const payload = await apiRequest("/v1/artifacts/session-events", {
    method: "POST",
    body: JSON.stringify(body),
  });

  return payload.eventsObjectKey;
}

async function ingestSyntheticSession(sessionId, eventsObjectKey, route, markerLabel) {
  const payload = {
    session: {
      id: sessionId,
      site: "e2e.example.local",
      route,
      startedAt: nowIso(),
      durationMs: 4200,
      eventsObjectKey,
    },
    markers: [
      {
        id: randomID("marker"),
        clusterKey: `integration:${route}`,
        label: markerLabel,
        replayOffsetMs: 1200,
        kind: "ui_no_effect",
        evidence: "e2e synthetic marker",
      },
    ],
  };

  return apiRequest("/v1/ingest/session", {
    method: "POST",
    body: JSON.stringify(payload),
  });
}

async function waitForSessionReady(sessionId) {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    try {
      const payload = await apiRequest(`/v1/sessions/${encodeURIComponent(sessionId)}`);
      const session = payload.session;
      const hasAnalysisArtifact =
        Array.isArray(session?.artifacts) &&
        session.artifacts.some((artifact) => artifact.artifactType === "analysis_json");
      const reportStatus = session?.reportCard?.status ?? "pending";
      if (hasAnalysisArtifact && reportStatus !== "pending") {
        return session;
      }
    } catch {
      // Keep polling until timeout.
    }
    await new Promise((resolve) => setTimeout(resolve, pollIntervalMs));
  }

  throw new Error(`session ${sessionId} did not finish replay/analyzer processing within ${timeoutMs}ms`);
}

async function assertClusterContainsSessions(clusterKey, sessionIDs) {
  const payload = await apiRequest(`/v1/issues/${encodeURIComponent(clusterKey)}/sessions?limit=100`);
  const foundSessionIDs = new Set(payload.sessions.map((session) => session.sessionId));
  for (const sessionID of sessionIDs) {
    if (!foundSessionIDs.has(sessionID)) {
      throw new Error(`cluster ${clusterKey} missing session ${sessionID} in /issues/{clusterKey}/sessions`);
    }
  }
}

async function run() {
  const route = "/checkout/integration-e2e";
  const markerLabel = "Checkout did not submit in synthetic e2e flow";

  const sessionA = randomID("e2e_session");
  const sessionB = randomID("e2e_session");

  console.log(`[e2e] api=${apiBaseUrl}`);
  console.log(`[e2e] creating synthetic sessions ${sessionA}, ${sessionB}`);

  const objectA = await uploadSessionEvents(sessionA, "e2e.example.local", 0);
  const ingestA = await ingestSyntheticSession(sessionA, objectA, route, markerLabel);
  const objectB = await uploadSessionEvents(sessionB, "e2e.example.local", 500);
  const ingestB = await ingestSyntheticSession(sessionB, objectB, route, markerLabel);

  const markerA = ingestA?.session?.markers?.[0]?.clusterKey;
  const markerB = ingestB?.session?.markers?.[0]?.clusterKey;
  if (!markerA || markerA !== markerB) {
    throw new Error(`unexpected cluster keys from ingest: markerA=${markerA} markerB=${markerB}`);
  }

  console.log("[e2e] waiting for async replay/analyzer processing");
  await waitForSessionReady(sessionA);
  await waitForSessionReady(sessionB);

  await apiRequest("/v1/issues/promote", { method: "POST", body: "{}" });
  const issues = await apiRequest("/v1/issues");
  const cluster = issues.issues.find((issue) => issue.key === markerA);
  if (!cluster) {
    throw new Error(`expected promoted issue cluster ${markerA} not found`);
  }
  if ((cluster.sessionCount ?? 0) < 2) {
    throw new Error(`expected cluster ${markerA} sessionCount >= 2, got ${cluster.sessionCount}`);
  }

  await assertClusterContainsSessions(markerA, [sessionA, sessionB]);
  console.log(`[e2e] PASS cluster=${markerA} sessionCount=${cluster.sessionCount}`);
}

run().catch((error) => {
  console.error(`[e2e] FAIL: ${error instanceof Error ? error.message : String(error)}`);
  process.exit(1);
});
