import type { ReplayWorkerConfig } from "./config.js";
import type { AnalysisReportUpdate, ReplayArtifactReport } from "./types.js";

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

export async function reportAnalysisUpdate(
  config: ReplayWorkerConfig,
  payload: AnalysisReportUpdate,
): Promise<void> {
  if (!config.internalApiKey) {
    throw new Error("INTERNAL_API_KEY is required for replay callbacks");
  }

  const endpoint = `${normalizeBaseUrl(config.orchestratorBaseUrl)}/v1/internal/analysis-reports`;
  const response = await fetch(endpoint, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      "X-Retrospec-Internal": config.internalApiKey,
    },
    body: JSON.stringify({
      projectId: payload.projectId,
      sessionId: payload.sessionId,
      status: payload.status,
      symptom: payload.symptom ?? "",
      technicalRootCause: payload.technicalRootCause ?? "",
      suggestedFix: payload.suggestedFix ?? "",
      textSummary: payload.textSummary ?? "",
      visualSummary: payload.visualSummary ?? "",
      ...(typeof payload.confidence === "number" ? { confidence: payload.confidence } : {}),
      generatedAt: payload.generatedAt,
    }),
  });

  if (!response.ok) {
    const body = await response.text().catch(() => "");
    throw new Error(`analysis callback failed status=${response.status} body=${body}`);
  }
}
