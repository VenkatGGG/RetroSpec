import { z } from "zod";
import { loadConfig } from "./config.js";
import type { ReplayJobData, ReplayResult } from "./types.js";
import { loadEventsBlob, storeArtifact } from "./s3.js";

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
  rrwebPayloadSchema.parse(eventsBlob);

  const artifact = {
    version: 1,
    projectId: data.projectId,
    sessionId: data.sessionId,
    sourceEventsObjectKey: data.eventsObjectKey,
    markerWindows: markerWindows(data.markerOffsetsMs),
    triggerKind: data.triggerKind,
    notes:
      "Replay rendering output placeholder. Hook Playwright/rrweb render pipeline here to emit MP4 or clipped assets.",
    generatedAt: new Date().toISOString(),
  };

  const artifactKey = `${config.artifactPrefix}${data.sessionId}/analysis.json`;
  await storeArtifact(artifactKey, artifact);

  return {
    sessionId: data.sessionId,
    artifactKey,
    markerWindows: artifact.markerWindows,
    generatedAt: artifact.generatedAt,
  };
}
