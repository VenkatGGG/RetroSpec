import type { AnalyzerWorkerConfig } from "./config.js";
import { analyzeSession } from "./analyze.js";
import type { AnalysisJobData, AnalysisReport } from "./types.js";
import { z } from "zod";

interface RemoteTextResponse {
  symptom?: string;
  technicalRootCause?: string;
  suggestedFix?: string;
  summary?: string;
  confidence?: number;
}

interface RRWebEventLike {
  timestamp?: unknown;
}

const MAX_REMOTE_EVENTS = 180;
const MAX_REMOTE_PAYLOAD_CHARS = 45_000;
const MODEL_REQUEST_SCHEMA_VERSION = "retrospec.analysis.text.v1";

const contractText = z.preprocess((value) => {
  if (typeof value !== "string") {
    return value;
  }
  return value.trim();
}, z.string().min(3).max(2000));

const remoteTextResponseSchema = z.object({
  symptom: contractText.optional(),
  technicalRootCause: contractText.optional(),
  rootCause: contractText.optional(),
  cause: contractText.optional(),
  suggestedFix: contractText.optional(),
  fix: contractText.optional(),
  recommendation: contractText.optional(),
  summary: contractText.optional(),
  textSummary: contractText.optional(),
  confidence: z.number().min(0).max(1).optional(),
}).strict();

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

function selectEventsNearMarkers(
  rawEvents: unknown[],
  markerOffsetsMs: number[],
): unknown[] {
  if (rawEvents.length === 0 || markerOffsetsMs.length === 0) {
    return rawEvents.slice(0, MAX_REMOTE_EVENTS);
  }

  const typedEvents = rawEvents as RRWebEventLike[];
  const firstTimestamp =
    typeof typedEvents[0]?.timestamp === "number" ? (typedEvents[0].timestamp as number) : 0;
  const windows = markerOffsetsMs.map((offset) => ({
    start: Math.max(0, firstTimestamp + offset - 3_000),
    end: firstTimestamp + offset + 3_000,
  }));

  const selected: unknown[] = [];
  for (const event of typedEvents) {
    if (selected.length >= MAX_REMOTE_EVENTS) {
      break;
    }
    if (typeof event.timestamp !== "number") {
      continue;
    }
    const ts = event.timestamp;
    const inWindow = windows.some((window) => ts >= window.start && ts <= window.end);
    if (inWindow) {
      selected.push(event);
    }
  }

  if (selected.length > 0) {
    return selected;
  }
  return rawEvents.slice(0, MAX_REMOTE_EVENTS);
}

function redactSensitiveValues(input: string): string {
  return input
    .replace(/\b[\w.+-]+@[\w.-]+\.[A-Za-z]{2,}\b/g, "<email>")
    .replace(/\b[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}\b/gi, "<uuid>")
    .replace(/\b\d{12,19}\b/g, "<long-number>")
    .replace(/\b(?:\d[ -]*?){13,16}\b/g, "<card-number>")
    .replace(/\b(?:access|api|auth|secret|token|password|passwd)[^,\s]{0,40}/gi, "<credential>")
    .replace(/https?:\/\/[^\s"']+/gi, "<url>");
}

function summarizeEvents(rawEvents: unknown, markerOffsetsMs: number[]): {
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
  const sampled = selectEventsNearMarkers(rawEvents, markerOffsetsMs);
  let payload = "";
  try {
    payload = redactSensitiveValues(JSON.stringify(sampled)).slice(0, MAX_REMOTE_PAYLOAD_CHARS);
  } catch {
    payload = "";
  }

  return {
    eventCount,
    sampledPayload: payload,
  };
}

function parseRemoteTextResponse(raw: unknown): RemoteTextResponse {
  const parsed = remoteTextResponseSchema.safeParse(raw);
  if (!parsed.success) {
    throw new Error(`remote model response does not match contract: ${parsed.error.issues.map((issue) => issue.path.join(".")).join(",")}`);
  }

  const source = parsed.data;
  const response = {
    symptom: toStringIfNonEmpty(source.symptom),
    technicalRootCause:
      toStringIfNonEmpty(source.technicalRootCause) ||
      toStringIfNonEmpty(source.rootCause) ||
      toStringIfNonEmpty(source.cause),
    suggestedFix:
      toStringIfNonEmpty(source.suggestedFix) ||
      toStringIfNonEmpty(source.fix) ||
      toStringIfNonEmpty(source.recommendation),
    summary:
      toStringIfNonEmpty(source.summary) ||
      toStringIfNonEmpty(source.textSummary),
    confidence: typeof source.confidence === "number" ? clamp01(source.confidence) : undefined,
  };

  if (!response.symptom && !response.technicalRootCause && !response.suggestedFix && !response.summary) {
    throw new Error("remote model response did not provide any usable report fields");
  }

  return response;
}

async function postTextModelRequest(
  endpoint: string,
  job: AnalysisJobData,
  rawEvents: unknown,
  config: AnalyzerWorkerConfig,
): Promise<RemoteTextResponse> {
  const trimmedEndpoint = endpoint.trim();
  if (!trimmedEndpoint) {
    throw new Error("text model endpoint is not configured");
  }

  const { eventCount, sampledPayload } = summarizeEvents(rawEvents, job.markerOffsetsMs);
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
        schemaVersion: MODEL_REQUEST_SCHEMA_VERSION,
        expectedResponseSchema: "symptom|technicalRootCause|suggestedFix|summary|confidence",
        projectId: job.projectId,
        sessionId: job.sessionId,
        site: job.site,
        route: job.route,
        triggerKind: job.triggerKind,
        markerOffsetsMs: job.markerOffsetsMs,
        markerHints: (job.markerHints ?? []).slice(0, 20),
        eventCount,
        eventsSample: sampledPayload,
      }),
    });

    if (!response.ok) {
      const body = await response.text().catch(() => "");
      throw new Error(`text model status=${response.status} body=${body}`);
    }

    const payload = (await response.json().catch(() => ({}))) as unknown;
    return parseRemoteTextResponse(payload);
  } finally {
    clearTimeout(timeout);
  }
}

function mergeTextPathReport(
  baseline: AnalysisReport,
  textPath: RemoteTextResponse,
): AnalysisReport {
  const confidenceValues = [
    baseline.confidence,
    ...(typeof textPath.confidence === "number" ? [textPath.confidence] : []),
  ];
  const combinedConfidence =
    confidenceValues.reduce((sum, current) => sum + current, 0) /
    Math.max(1, confidenceValues.length);

  const textSummary = [
    textPath.summary || baseline.textSummary,
    "Text path source: remote model.",
  ]
    .filter(Boolean)
    .join(" ");

  return {
    ...baseline,
    symptom: textPath.symptom || baseline.symptom,
    technicalRootCause: textPath.technicalRootCause || baseline.technicalRootCause,
    suggestedFix: textPath.suggestedFix || baseline.suggestedFix,
    textSummary,
    confidence: clamp01(combinedConfidence),
  };
}

function applyTextGate(report: AnalysisReport, config: AnalyzerWorkerConfig): AnalysisReport {
  const threshold = clamp01(config.minAcceptConfidence);
  if (report.confidence >= threshold) {
    return {
      ...report,
      status: "pending",
      visualSummary: `${report.visualSummary} Awaiting visual confirmation from reconstructed replay video.`,
    };
  }

  if (!config.discardUncertain) {
    return {
      ...report,
      status: "pending",
      visualSummary: `${report.visualSummary} Forced to visual verification despite low confidence (${report.confidence.toFixed(2)}).`,
    };
  }

  return {
    ...report,
    status: "discarded",
    textSummary: `${report.textSummary} Discarded before visual stage: confidence ${report.confidence.toFixed(2)} below threshold ${threshold.toFixed(2)}.`,
    visualSummary: `${report.visualSummary} Visual stage skipped due to low text confidence.`,
  };
}

export async function generateAnalysisReport(
  job: AnalysisJobData,
  rawEvents: unknown,
  generatedAt: Date,
  config: AnalyzerWorkerConfig,
): Promise<AnalysisReport> {
  const baseline = analyzeSession(job, rawEvents, generatedAt);
  if (config.provider !== "remote_text") {
    return applyTextGate(baseline, config);
  }

  try {
    const textPath = await postTextModelRequest(config.textModelEndpoint, job, rawEvents, config);
    return applyTextGate(mergeTextPathReport(baseline, textPath), config);
  } catch (error) {
    if (!config.fallbackToHeuristic) {
      throw error;
    }
    return applyTextGate(
      {
        ...baseline,
        textSummary: `${baseline.textSummary} Text path source: heuristic fallback (remote unavailable).`,
      },
      config,
    );
  }
}
