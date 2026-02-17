import { createHash } from "node:crypto";
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
  attempt: z.number().int().positive().default(1),
});

const config = loadConfig();
const queueName = config.queueName;
const failedQueueName = `${config.queueName}:failed`;
const retryQueueName = `${config.queueName}:retry`;
const processingQueueName = `${config.queueName}:processing`;

const redis = new Redis({
  host: config.redisHost,
  port: config.redisPort,
  maxRetriesPerRequest: null,
});

let shuttingDown = false;

function jobFingerprint(parsed: z.infer<typeof replayJobSchema>): string {
  return createHash("sha1")
    .update(
      [
        parsed.projectId,
        parsed.sessionId,
        parsed.eventsObjectKey,
        parsed.triggerKind,
        parsed.markerOffsetsMs.join(","),
      ].join("|"),
    )
    .digest("hex")
    .slice(0, 24);
}

function dedupeKey(parsed: z.infer<typeof replayJobSchema>): string {
  return `${queueName}:done:${jobFingerprint(parsed)}`;
}

async function drainRetryQueue(): Promise<void> {
  const now = Date.now();
  const due = await redis.zrangebyscore(retryQueueName, 0, now, "LIMIT", 0, 20);
  if (due.length === 0) {
    return;
  }

  const pipeline = redis.multi();
  for (const item of due) {
    pipeline.zrem(retryQueueName, item);
    pipeline.lpush(queueName, item);
  }
  await pipeline.exec();
}

async function enqueueRetry(payload: unknown, attempt: number): Promise<void> {
  const baseDelay = Math.max(500, Math.floor(config.retryBaseMs));
  const delayMs = Math.min(120_000, baseDelay * 2 ** Math.max(0, attempt - 1));
  const runAtMs = Date.now() + delayMs;

  await redis.zadd(retryQueueName, runAtMs, JSON.stringify(payload));
}

async function recoverInFlightJobs(): Promise<void> {
  let moved = 0;
  while (true) {
    const payload = await redis.rpoplpush(processingQueueName, queueName);
    if (!payload) {
      break;
    }
    moved += 1;
    if (moved >= 10_000) {
      break;
    }
  }
  if (moved > 0) {
    console.warn(`[replay-worker] recovered in-flight jobs=${moved}`);
  }
}

async function workerLoop() {
  console.info(`[replay-worker] listening queue=${queueName}`);
  await recoverInFlightJobs();

  while (!shuttingDown) {
    let rawPayload = "";
    let parsed: z.infer<typeof replayJobSchema> | null = null;
    try {
      await drainRetryQueue();

      const payload = await redis.brpoplpush(queueName, processingQueueName, 5);
      if (!payload) {
        continue;
      }

      rawPayload = payload;
      parsed = replayJobSchema.parse(JSON.parse(rawPayload));
      const doneKey = dedupeKey(parsed);
      const alreadyDone = await redis.get(doneKey);
      if (alreadyDone) {
        console.info(
          `[replay-worker] skipped duplicate session=${parsed.sessionId} project=${parsed.projectId} attempt=${parsed.attempt}`,
        );
        continue;
      }

      const result = await processReplayJob(parsed);
      await reportReplayArtifact(config, {
        projectId: parsed.projectId,
        sessionId: result.sessionId,
        triggerKind: parsed.triggerKind,
        artifactType: "analysis_json",
        artifactKey: result.artifactKey,
        status: result.videoStatus === "failed" ? "failed" : "ready",
        generatedAt: result.generatedAt,
        windows: result.markerWindows,
      });
      if (result.videoStatus === "ready" && result.videoArtifactKey) {
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
      } else if (result.videoStatus === "failed") {
        await reportReplayArtifact(config, {
          projectId: parsed.projectId,
          sessionId: result.sessionId,
          triggerKind: parsed.triggerKind,
          artifactType: "replay_video",
          artifactKey: "",
          status: "failed",
          generatedAt: result.generatedAt,
          windows: result.markerWindows,
        });
      }
      await redis.set(doneKey, result.generatedAt, "EX", Math.max(60, config.dedupeWindowSec));

      console.info(
        `[replay-worker] processed session=${result.sessionId} project=${parsed.projectId} analysis=${result.artifactKey} videoStatus=${result.videoStatus} video=${result.videoArtifactKey ?? "none"}`,
      );
    } catch (error) {
      const details = error instanceof Error ? error.message : String(error);
      console.error("[replay-worker] job failed", details);

      const attempt = parsed?.attempt ?? 1;
      if (parsed && attempt < Math.max(1, config.maxAttempts)) {
        const nextAttempt = attempt + 1;
        await enqueueRetry({ ...parsed, attempt: nextAttempt }, nextAttempt);
        console.warn(
          `[replay-worker] scheduled retry session=${parsed.sessionId} attempt=${nextAttempt}`,
        );
      } else {
        // Push malformed or exhausted jobs to a dead-letter queue for manual inspection.
        await redis.lpush(
          failedQueueName,
          JSON.stringify({
            failedAt: new Date().toISOString(),
            error: details,
            attempt,
            payload: rawPayload,
          }),
        );
      }
    }
    finally {
      if (rawPayload) {
        await redis.lrem(processingQueueName, 1, rawPayload);
      }
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
