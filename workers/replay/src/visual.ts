import { z } from "zod";
import type { ReplayWorkerConfig } from "./config.js";
import type { ReplayJobData, ReplayResult, VisualModelVerdict } from "./types.js";

const VISUAL_REQUEST_SCHEMA_VERSION = "retrospec.visual.v1";

const contractText = z.preprocess((value) => {
  if (typeof value !== "string") {
    return value;
  }
  return value.trim();
}, z.string().min(3).max(2200));

const visualResponseSchema = z.object({
  confirmed: z.boolean().optional(),
  confidence: z.number().min(0).max(1).optional(),
  summary: contractText.optional(),
  symptom: contractText.optional(),
  technicalRootCause: contractText.optional(),
  suggestedFix: contractText.optional(),
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

function parseVisualVerdict(raw: unknown): VisualModelVerdict {
  const parsed = visualResponseSchema.safeParse(raw);
  if (!parsed.success) {
    throw new Error(`visual model response does not match contract: ${parsed.error.issues.map((issue) => issue.path.join(".")).join(",")}`);
  }

  const source = parsed.data;
  const confirmed = Boolean(source.confirmed);
  const confidence = clamp01(typeof source.confidence === "number" ? source.confidence : 0);
  const summary = toStringIfNonEmpty(source.summary);

  if (!summary && !source.symptom && !source.technicalRootCause && !source.suggestedFix) {
    throw new Error("visual model response did not provide any usable report fields");
  }

  return {
    confirmed,
    confidence,
    summary,
    symptom: toStringIfNonEmpty(source.symptom) || undefined,
    technicalRootCause: toStringIfNonEmpty(source.technicalRootCause) || undefined,
    suggestedFix: toStringIfNonEmpty(source.suggestedFix) || undefined,
  };
}

export async function runVisualVerification(
  config: ReplayWorkerConfig,
  job: ReplayJobData,
  replayResult: ReplayResult,
): Promise<VisualModelVerdict> {
  const endpoint = config.visualModelEndpoint.trim();
  if (!endpoint) {
    throw new Error("visual model endpoint is not configured");
  }
  if (!replayResult.videoArtifactKey || replayResult.videoStatus !== "ready") {
    throw new Error("replay video is not available for visual verification");
  }

  const controller = new AbortController();
  const timeout = setTimeout(() => {
    controller.abort();
  }, Math.max(5_000, config.visualModelTimeoutMs));

  try {
    const response = await fetch(endpoint, {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        ...(config.visualModelApiKey
          ? {
              Authorization: `Bearer ${config.visualModelApiKey}`,
            }
          : {}),
      },
      signal: controller.signal,
      body: JSON.stringify({
        schemaVersion: VISUAL_REQUEST_SCHEMA_VERSION,
        projectId: job.projectId,
        sessionId: job.sessionId,
        site: job.site,
        route: job.route,
        triggerKind: job.triggerKind,
        markerOffsetsMs: job.markerOffsetsMs,
        markerWindows: replayResult.markerWindows,
        generatedAt: replayResult.generatedAt,
        videoArtifact: {
          bucket: config.s3Bucket,
          endpoint: config.s3Endpoint ?? "",
          region: config.s3Region,
          key: replayResult.videoArtifactKey,
          contentType: "video/webm",
        },
      }),
    });
    if (!response.ok) {
      const body = await response.text().catch(() => "");
      throw new Error(`visual model status=${response.status} body=${body}`);
    }

    const payload = (await response.json().catch(() => ({}))) as unknown;
    return parseVisualVerdict(payload);
  } finally {
    clearTimeout(timeout);
  }
}
