import type { AnalyzerWorkerConfig } from "./config.js";
import { analyzeSession } from "./analyze.js";
import type { AnalysisJobData, AnalysisReport } from "./types.js";

interface RemotePathResponse {
  symptom?: string;
  technicalRootCause?: string;
  suggestedFix?: string;
  summary?: string;
  confidence?: number;
}

function clamp01(value: number): number {
  if (!Number.isFinite(value)) {
    return 0;
  }
  if (value < 0) {
    return 0;
  }
  if (value > 1) {
    return 1;
  }
  return value;
}

function toStringIfNonEmpty(value: unknown): string {
  if (typeof value !== "string") {
    return "";
  }
  return value.trim();
}

function summarizeEvents(rawEvents: unknown): {
  eventCount: number;
  sampledPayload: string;
} {
  if (!Array.isArray(rawEvents)) {
    return {
      eventCount: 0,
      sampledPayload: "",
    };
  }

  const eventCount = rawEvents.length;
  const sampled = rawEvents.slice(0, 120);
  let payload = "";
  try {
    payload = JSON.stringify(sampled).slice(0, 40_000);
  } catch {
    payload = "";
  }

  return {
    eventCount,
    sampledPayload: payload,
  };
}

function parseRemotePathResponse(raw: unknown): RemotePathResponse {
  if (!raw || typeof raw !== "object") {
    return {};
  }
  const source = raw as Record<string, unknown>;
  const technicalRootCause =
    toStringIfNonEmpty(source.technicalRootCause) ||
    toStringIfNonEmpty(source.rootCause) ||
    toStringIfNonEmpty(source.cause);
  const suggestedFix =
    toStringIfNonEmpty(source.suggestedFix) ||
    toStringIfNonEmpty(source.fix) ||
    toStringIfNonEmpty(source.recommendation);
  const summary =
    toStringIfNonEmpty(source.summary) ||
    toStringIfNonEmpty(source.textSummary) ||
    toStringIfNonEmpty(source.visualSummary);
  const confidenceRaw = source.confidence;
  const confidence = typeof confidenceRaw === "number" ? clamp01(confidenceRaw) : undefined;

  return {
    symptom: toStringIfNonEmpty(source.symptom),
    technicalRootCause,
    suggestedFix,
    summary,
    confidence,
  };
}

async function postModelRequest(
  endpoint: string,
  mode: "text" | "visual",
  job: AnalysisJobData,
  rawEvents: unknown,
  config: AnalyzerWorkerConfig,
): Promise<RemotePathResponse> {
  const trimmedEndpoint = endpoint.trim();
  if (!trimmedEndpoint) {
    throw new Error(`${mode} model endpoint is not configured`);
  }

  const { eventCount, sampledPayload } = summarizeEvents(rawEvents);
  const controller = new AbortController();
  const timeout = setTimeout(() => {
    controller.abort();
  }, Math.max(5_000, config.modelTimeoutMs));

  try {
    const response = await fetch(trimmedEndpoint, {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        ...(config.modelApiKey
          ? {
              Authorization: `Bearer ${config.modelApiKey}`,
            }
          : {}),
      },
      signal: controller.signal,
      body: JSON.stringify({
        mode,
        projectId: job.projectId,
        sessionId: job.sessionId,
        site: job.site,
        route: job.route,
        triggerKind: job.triggerKind,
        markerOffsetsMs: job.markerOffsetsMs,
        eventCount,
        eventsSample: sampledPayload,
      }),
    });

    if (!response.ok) {
      const body = await response.text().catch(() => "");
      throw new Error(`${mode} model status=${response.status} body=${body}`);
    }

    const payload = (await response.json().catch(() => ({}))) as unknown;
    return parseRemotePathResponse(payload);
  } finally {
    clearTimeout(timeout);
  }
}

function mergeDualPathReport(
  baseline: AnalysisReport,
  textPath: RemotePathResponse | null,
  visualPath: RemotePathResponse | null,
): AnalysisReport {
  const textConfidence = textPath?.confidence;
  const visualConfidence = visualPath?.confidence;
  const confidenceValues = [
    baseline.confidence,
    ...(typeof textConfidence === "number" ? [textConfidence] : []),
    ...(typeof visualConfidence === "number" ? [visualConfidence] : []),
  ];
  const combinedConfidence =
    confidenceValues.reduce((sum, current) => sum + current, 0) /
    Math.max(1, confidenceValues.length);

  const textSummary = [
    textPath?.summary || baseline.textSummary,
    textPath ? "Text path source: remote model." : "Text path source: heuristic fallback.",
  ]
    .filter(Boolean)
    .join(" ");
  const visualSummary = [
    visualPath?.summary || baseline.visualSummary,
    visualPath ? "Visual path source: remote model." : "Visual path source: heuristic fallback.",
  ]
    .filter(Boolean)
    .join(" ");

  return {
    ...baseline,
    symptom: textPath?.symptom || visualPath?.symptom || baseline.symptom,
    technicalRootCause:
      textPath?.technicalRootCause || visualPath?.technicalRootCause || baseline.technicalRootCause,
    suggestedFix: textPath?.suggestedFix || visualPath?.suggestedFix || baseline.suggestedFix,
    textSummary,
    visualSummary,
    confidence: clamp01(combinedConfidence),
  };
}

export async function generateAnalysisReport(
  job: AnalysisJobData,
  rawEvents: unknown,
  generatedAt: Date,
  config: AnalyzerWorkerConfig,
): Promise<AnalysisReport> {
  const baseline = analyzeSession(job, rawEvents, generatedAt);
  if (config.provider !== "dual_http") {
    return baseline;
  }

  const [textResult, visualResult] = await Promise.allSettled([
    postModelRequest(config.textModelEndpoint, "text", job, rawEvents, config),
    postModelRequest(config.visualModelEndpoint, "visual", job, rawEvents, config),
  ]);

  const textPath = textResult.status === "fulfilled" ? textResult.value : null;
  const visualPath = visualResult.status === "fulfilled" ? visualResult.value : null;

  if (!textPath && !visualPath) {
    if (config.fallbackToHeuristic) {
      return {
        ...baseline,
        textSummary: `${baseline.textSummary} Text path source: heuristic fallback (remote unavailable).`,
        visualSummary: `${baseline.visualSummary} Visual path source: heuristic fallback (remote unavailable).`,
      };
    }
    const textError = textResult.status === "rejected" ? textResult.reason : "unknown";
    const visualError = visualResult.status === "rejected" ? visualResult.reason : "unknown";
    throw new Error(`dual_http provider failed text=${String(textError)} visual=${String(visualError)}`);
  }

  return mergeDualPathReport(baseline, textPath, visualPath);
}
