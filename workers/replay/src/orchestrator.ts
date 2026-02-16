import type { ReplayWorkerConfig } from "./config.js";
import type { ReplayArtifactReport } from "./types.js";

function normalizeBaseUrl(value: string): string {
  return value.replace(/\/+$/, "");
}

export async function reportReplayArtifact(
  config: ReplayWorkerConfig,
  payload: ReplayArtifactReport,
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
      projectId: payload.projectId,
      sessionId: payload.sessionId,
      artifactType: payload.artifactType,
      artifactKey: payload.artifactKey,
      triggerKind: payload.triggerKind,
      status: payload.status,
      generatedAt: payload.generatedAt,
      windows: payload.windows,
    }),
  });

  if (!response.ok) {
    const body = await response.text().catch(() => "");
    throw new Error(`orchestrator callback failed status=${response.status} body=${body}`);
  }
}
