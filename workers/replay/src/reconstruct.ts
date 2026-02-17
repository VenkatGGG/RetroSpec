import { z } from "zod";
import { loadConfig } from "./config.js";
import type { ReplayJobData, ReplayResult } from "./types.js";
import { renderReplayWebm } from "./render.js";
import { loadEventsBlob, storeArtifact, storeBinaryArtifact } from "./s3.js";

const rrwebEventSchema = z.object({
  type: z.number(),
  timestamp: z.number(),
});

const rrwebPayloadSchema = z.array(rrwebEventSchema);

function markerWindows(offsetsMs: number[]): Array<{ startMs: number; endMs: number }> {
  return offsetsMs.map((offset) => ({
    startMs: Math.max(0, offset - 2_000),
    endMs: offset + 8_000,
  }));
}

export async function processReplayJob(data: ReplayJobData): Promise<ReplayResult> {
  const config = loadConfig();
  const eventsBlob = await loadEventsBlob(data.eventsObjectKey);

  // Schema check catches corrupted uploads before expensive rendering starts.
  const replayEvents = rrwebPayloadSchema.parse(eventsBlob);
  const generatedAt = new Date().toISOString();
  let videoArtifactKey = "";
  let renderStatus: "ready" | "failed" | "skipped" = "skipped";
  let renderError = "";

  if (config.renderEnabled) {
    try {
      const videoBuffer = await renderReplayWebm(replayEvents, config);
      videoArtifactKey = `${config.artifactPrefix}${data.sessionId}/full-replay.webm`;
      await storeBinaryArtifact(videoArtifactKey, videoBuffer, "video/webm");
      renderStatus = "ready";
    } catch (error) {
      renderStatus = "failed";
      renderError = error instanceof Error ? error.message : String(error);
    }
  }

  const artifact = {
    version: 1,
    projectId: data.projectId,
    sessionId: data.sessionId,
    sourceEventsObjectKey: data.eventsObjectKey,
    markerWindows: markerWindows(data.markerOffsetsMs),
    triggerKind: data.triggerKind,
    replayVideoObjectKey: videoArtifactKey || null,
    renderStatus,
    renderError: renderError || null,
    generatedAt,
  };

  const artifactKey = `${config.artifactPrefix}${data.sessionId}/analysis.json`;
  await storeArtifact(artifactKey, artifact);

  return {
    sessionId: data.sessionId,
    artifactKey,
    markerWindows: artifact.markerWindows,
    generatedAt,
    videoArtifactKey: videoArtifactKey || undefined,
    videoStatus: renderStatus,
    videoError: renderError || undefined,
  };
}
