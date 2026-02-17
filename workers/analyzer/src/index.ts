import { createHash } from "node:crypto";
import { Redis } from "ioredis";
import { z } from "zod";
import { loadConfig } from "./config.js";
import { generateAnalysisReport } from "./model.js";
import { reportAnalysisCard } from "./orchestrator.js";
import { loadEventsBlob } from "./s3.js";

const analysisJobSchema = z.object({
  projectId: z.string().min(1),
  sessionId: z.string().min(1),
  eventsObjectKey: z.string().min(1),
  markerOffsetsMs: z.array(z.number().int().nonnegative()).min(1),
  markerHints: z.array(z.string().trim().max(240)).default([]),
  triggerKind: z.enum(["api_error", "js_exception", "validation_failed", "ui_no_effect"]),
  route: z.string().default("/unknown"),
  site: z.string().default("unknown-site"),
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

function jobFingerprint(parsed: z.infer<typeof analysisJobSchema>): string {
  return createHash("sha1")
    .update(
      [
        parsed.projectId,
        parsed.sessionId,
        parsed.eventsObjectKey,
        parsed.triggerKind,
        parsed.route,
        parsed.site,
        parsed.markerHints.join("||"),
        parsed.markerOffsetsMs.join(","),
      ].join("|"),
    )
    .digest("hex")
    .slice(0, 24);
}

function dedupeKey(parsed: z.infer<typeof analysisJobSchema>): string {
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
    console.warn(`[analyzer-worker] recovered in-flight jobs=${moved}`);
  }
}

async function reportFailure(
  parsed: z.infer<typeof analysisJobSchema>,
  details: string,
): Promise<void> {
  try {
    await reportAnalysisCard(config, {
      projectId: parsed.projectId,
      sessionId: parsed.sessionId,
      status: "failed",
      symptom: "Analysis pipeline failed before report generation.",
      technicalRootCause: details.slice(0, 600),
      suggestedFix:
        "Inspect analyzer worker logs and payload shape for this session, then retry analysis.",
      textSummary: "Analyzer job failed and was moved to dead-letter processing.",
      visualSummary: `Route: ${parsed.route}.`,
      confidence: 0.15,
      generatedAt: new Date().toISOString(),
    });
  } catch (callbackError) {
    const callbackDetails =
      callbackError instanceof Error ? callbackError.message : String(callbackError);
    console.error(
      `[analyzer-worker] failed to persist failed-status report session=${parsed.sessionId} err=${callbackDetails}`,
    );
  }
}

async function workerLoop() {
  console.info(`[analyzer-worker] listening queue=${queueName}`);
  await recoverInFlightJobs();

  while (!shuttingDown) {
    let rawPayload = "";
    let parsed: z.infer<typeof analysisJobSchema> | null = null;
    try {
      await drainRetryQueue();

      const payload = await redis.brpoplpush(queueName, processingQueueName, 5);
      if (!payload) {
        continue;
      }

      rawPayload = payload;
      parsed = analysisJobSchema.parse(JSON.parse(rawPayload));
      const doneKey = dedupeKey(parsed);
      const alreadyDone = await redis.get(doneKey);
      if (alreadyDone) {
        console.info(
          `[analyzer-worker] skipped duplicate session=${parsed.sessionId} project=${parsed.projectId} attempt=${parsed.attempt}`,
        );
        continue;
      }

      const generatedAt = new Date();
      const rawEvents = await loadEventsBlob(parsed.eventsObjectKey);
      const report = await generateAnalysisReport(parsed, rawEvents, generatedAt, config);
      await reportAnalysisCard(config, report);
      await redis.set(doneKey, report.generatedAt, "EX", Math.max(60, config.dedupeWindowSec));

      console.info(
        `[analyzer-worker] processed session=${parsed.sessionId} project=${parsed.projectId} confidence=${report.confidence.toFixed(2)} status=${report.status}`,
      );
    } catch (error) {
      const details = error instanceof Error ? error.message : String(error);
      console.error("[analyzer-worker] job failed", details);

      const attempt = parsed?.attempt ?? 1;
      if (parsed && attempt < Math.max(1, config.maxAttempts)) {
        const nextAttempt = attempt + 1;
        await enqueueRetry({ ...parsed, attempt: nextAttempt }, nextAttempt);
        console.warn(
          `[analyzer-worker] scheduled retry session=${parsed.sessionId} attempt=${nextAttempt}`,
        );
      } else {
        await redis.lpush(
          failedQueueName,
          JSON.stringify({
            failedAt: new Date().toISOString(),
            error: details,
            attempt,
            payload: rawPayload,
          }),
        );
        if (parsed) {
          await reportFailure(parsed, details);
        }
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
  console.info(`[analyzer-worker] shutdown signal=${signal}`);
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
