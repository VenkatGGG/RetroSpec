import type { AnalyzerWorkerConfig } from "./config.js";
import type { AnalysisReport } from "./types.js";

function normalizeBaseUrl(value: string): string {
  return value.replace(/\/+$/, "");
}

export async function reportAnalysisCard(
  config: AnalyzerWorkerConfig,
  payload: AnalysisReport,
): Promise<void> {
  if (!config.internalApiKey) {
    throw new Error("INTERNAL_API_KEY is required for analysis callbacks");
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
      symptom: payload.symptom,
      technicalRootCause: payload.technicalRootCause,
      suggestedFix: payload.suggestedFix,
      textSummary: payload.textSummary,
      visualSummary: payload.visualSummary,
      confidence: payload.confidence,
      generatedAt: payload.generatedAt,
    }),
  });

  if (!response.ok) {
    const body = await response.text().catch(() => "");
    throw new Error(`orchestrator callback failed status=${response.status} body=${body}`);
  }
}
