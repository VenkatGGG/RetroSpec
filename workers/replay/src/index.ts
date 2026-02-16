import { Redis } from "ioredis";
import { z } from "zod";
import { loadConfig } from "./config.js";
import { reportReplayArtifact } from "./orchestrator.js";
import { processReplayJob } from "./reconstruct.js";

const replayJobSchema = z.object({
  projectId: z.string().min(1),
  sessionId: z.string().min(1),
  eventsObjectKey: z.string().min(1),
  markerOffsetsMs: z.array(z.number().int().nonnegative()).min(1),
  triggerKind: z.enum(["api_error", "js_exception", "validation_failed", "ui_no_effect"]),
});

const config = loadConfig();
const queueName = config.queueName;
const failedQueueName = `${config.queueName}:failed`;

const redis = new Redis({
  host: config.redisHost,
  port: config.redisPort,
  maxRetriesPerRequest: null,
});

let shuttingDown = false;

async function workerLoop() {
  console.info(`[replay-worker] listening queue=${queueName}`);

  while (!shuttingDown) {
    let rawPayload = "";
    try {
      const item = await redis.brpop(queueName, 5);
      if (!item) {
        continue;
      }

      rawPayload = item[1];
      const parsed = replayJobSchema.parse(JSON.parse(rawPayload));
      const result = await processReplayJob(parsed);
      await reportReplayArtifact(config, {
        projectId: parsed.projectId,
        sessionId: result.sessionId,
        triggerKind: parsed.triggerKind,
        artifactType: "analysis_json",
        artifactKey: result.artifactKey,
        status: "ready",
        generatedAt: result.generatedAt,
        windows: result.markerWindows,
      });
      if (result.videoArtifactKey) {
        await reportReplayArtifact(config, {
          projectId: parsed.projectId,
          sessionId: result.sessionId,
          triggerKind: parsed.triggerKind,
          artifactType: "replay_video",
          artifactKey: result.videoArtifactKey,
          status: "ready",
          generatedAt: result.generatedAt,
          windows: result.markerWindows,
        });
      }

      console.info(
        `[replay-worker] processed session=${result.sessionId} project=${parsed.projectId} analysis=${result.artifactKey} video=${result.videoArtifactKey ?? "none"}`,
      );
    } catch (error) {
      const details = error instanceof Error ? error.message : String(error);
      console.error("[replay-worker] job failed", details);

      // Push malformed or failed payloads to a dead-letter queue for manual inspection.
      await redis.lpush(
        failedQueueName,
        JSON.stringify({
          failedAt: new Date().toISOString(),
          error: details,
          payload: rawPayload,
        }),
      );
    }
  }
}

async function shutdown(signal: string) {
  console.info(`[replay-worker] shutdown signal=${signal}`);
  shuttingDown = true;
  await redis.quit();
  process.exit(0);
}

process.on("SIGINT", () => {
  void shutdown("SIGINT");
});

process.on("SIGTERM", () => {
  void shutdown("SIGTERM");
});

void workerLoop();
