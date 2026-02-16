import type { ReplayWorkerConfig } from "./config.js";
import type { ReplayJobData, ReplayResult } from "./types.js";

function normalizeBaseUrl(value: string): string {
  return value.replace(/\/+$/, "");
}

export async function reportReplayResult(
  config: ReplayWorkerConfig,
  job: ReplayJobData,
  result: ReplayResult,
): Promise<void> {
  if (!config.internalApiKey) {
    throw new Error("INTERNAL_API_KEY is required for replay callbacks");
  }

  const endpoint = `${normalizeBaseUrl(config.orchestratorBaseUrl)}/v1/internal/replay-results`;
  const response = await fetch(endpoint, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      "X-Retrospec-Internal": config.internalApiKey,
    },
    body: JSON.stringify({
      projectId: job.projectId,
      sessionId: result.sessionId,
      artifactType: "analysis_json",
      artifactKey: result.artifactKey,
      triggerKind: job.triggerKind,
      status: "ready",
      generatedAt: result.generatedAt,
      windows: result.markerWindows,
    }),
  });

  if (!response.ok) {
    const body = await response.text().catch(() => "");
    throw new Error(`orchestrator callback failed status=${response.status} body=${body}`);
  }
}
