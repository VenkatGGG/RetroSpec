import { createHash } from "node:crypto";
import { hostname } from "node:os";
import { Redis } from "ioredis";
import { z } from "zod";
import { loadConfig } from "./config.js";
import { generateAnalysisReport } from "./model.js";
import { reportAnalysisCard } from "./orchestrator.js";
import { loadEventsBlob } from "./s3.js";

interface StreamMessage {
  id: string;
  payload: string;
}

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
const groupName = `${config.queueName}:group`;
const consumerName = `${hostname()}-${process.pid}`;
const failedQueueName = `${config.queueName}:failed`;
const retryQueueName = `${config.queueName}:retry`;

const redis = new Redis({
  host: config.redisHost,
  port: config.redisPort,
  maxRetriesPerRequest: null,
});

let shuttingDown = false;

function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => {
    setTimeout(resolve, ms);
  });
}

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

function extractPayload(fields: unknown): string {
  if (!Array.isArray(fields)) {
    return "";
  }
  for (let index = 0; index < fields.length - 1; index += 2) {
    const key = String(fields[index] ?? "");
    if (key === "payload") {
      return String(fields[index + 1] ?? "");
    }
  }
  return "";
}

function parseStreamMessages(raw: unknown): StreamMessage[] {
  if (!Array.isArray(raw)) {
    return [];
  }

  const messages: StreamMessage[] = [];
  for (const streamRows of raw) {
    if (!Array.isArray(streamRows) || streamRows.length < 2) {
      continue;
    }
    const rows = streamRows[1];
    if (!Array.isArray(rows)) {
      continue;
    }
    for (const row of rows) {
      if (!Array.isArray(row) || row.length < 2) {
        continue;
      }
      const id = String(row[0] ?? "");
      const payload = extractPayload(row[1]);
      if (id && payload) {
        messages.push({ id, payload });
      }
    }
  }

  return messages;
}

async function ensureStreamQueue(): Promise<void> {
  const queueType = await redis.type(queueName);
  if (queueType === "none" || queueType === "stream") {
    return;
  }

  if (queueType !== "list") {
    throw new Error(`unsupported redis key type for queue ${queueName}: ${queueType}`);
  }

  const legacyQueueName = `${queueName}:legacy:list:${Date.now()}`;
  const renamed = await redis.renamenx(queueName, legacyQueueName);
  if (renamed !== 1) {
    const refreshedType = await redis.type(queueName);
    if (refreshedType === "none" || refreshedType === "stream") {
      return;
    }
    if (refreshedType === "list") {
      throw new Error(`legacy list queue migration contention for ${queueName}`);
    }
    throw new Error(`unsupported redis key type for queue ${queueName}: ${refreshedType}`);
  }

  let migrated = 0;
  while (true) {
    const payload = await redis.rpop(legacyQueueName);
    if (!payload) {
      break;
    }
    await redis.xadd(queueName, "*", "payload", payload);
    migrated += 1;
  }
  await redis.del(legacyQueueName);
  if (migrated > 0) {
    console.warn(`[analyzer-worker] migrated legacy list queue=${queueName} entries=${migrated}`);
  }
}

async function ensureConsumerGroup(): Promise<void> {
  await ensureStreamQueue();
  try {
    await redis.xgroup("CREATE", queueName, groupName, "0", "MKSTREAM");
  } catch (error) {
    const details = error instanceof Error ? error.message : String(error);
    if (!details.includes("BUSYGROUP")) {
      throw error;
    }
  }
}

async function drainRetryQueue(): Promise<void> {
  const now = Date.now();
  const due = await redis.zrangebyscore(retryQueueName, 0, now, "LIMIT", 0, 20);
  if (due.length === 0) {
    return;
  }

  for (const item of due) {
    await redis.xadd(queueName, "*", "payload", item);
    await redis.zrem(retryQueueName, item);
  }
}

async function enqueueRetry(payload: unknown, attempt: number): Promise<void> {
  const baseDelay = Math.max(500, Math.floor(config.retryBaseMs));
  const delayMs = Math.min(120_000, baseDelay * 2 ** Math.max(0, attempt - 1));
  const runAtMs = Date.now() + delayMs;

  await redis.zadd(retryQueueName, runAtMs, JSON.stringify(payload));
}

async function readNewMessages(): Promise<StreamMessage[]> {
  const result = await redis.xreadgroup(
    "GROUP",
    groupName,
    consumerName,
    "COUNT",
    "5",
    "BLOCK",
    "5000",
    "STREAMS",
    queueName,
    ">",
  );

  return parseStreamMessages(result);
}

async function claimStalePendingMessages(): Promise<StreamMessage[]> {
  const minIdleMs = Math.max(60, Math.floor(config.processingStaleSec)) * 1_000;
  let cursor = "0-0";
  const claimed: StreamMessage[] = [];

  for (let attempt = 0; attempt < 5; attempt += 1) {
    const raw = await redis.call(
      "XAUTOCLAIM",
      queueName,
      groupName,
      consumerName,
      String(minIdleMs),
      cursor,
      "COUNT",
      "20",
    );
    if (!Array.isArray(raw) || raw.length < 2) {
      break;
    }

    cursor = String(raw[0] ?? "0-0");
    const messages = parseStreamMessages([[queueName, raw[1]]]);
    if (messages.length === 0) {
      break;
    }

    claimed.push(...messages);
    if (messages.length < 20) {
      break;
    }
  }

  return claimed;
}

async function acknowledgeMessage(messageID: string): Promise<void> {
  await redis.multi().xack(queueName, groupName, messageID).xdel(queueName, messageID).exec();
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

async function handleMessage(message: StreamMessage): Promise<void> {
  let parsed: z.infer<typeof analysisJobSchema> | null = null;
  let shouldAcknowledge = false;
  try {
    parsed = analysisJobSchema.parse(JSON.parse(message.payload));
    const doneKey = dedupeKey(parsed);
    const alreadyDone = await redis.get(doneKey);
    if (alreadyDone) {
      console.info(
        `[analyzer-worker] skipped duplicate session=${parsed.sessionId} project=${parsed.projectId} attempt=${parsed.attempt}`,
      );
      shouldAcknowledge = true;
      return;
    }

    const generatedAt = new Date();
    const rawEvents = await loadEventsBlob(parsed.eventsObjectKey);
    const report = await generateAnalysisReport(parsed, rawEvents, generatedAt, config);
    await reportAnalysisCard(config, report);
    await redis.set(doneKey, report.generatedAt, "EX", Math.max(60, config.dedupeWindowSec));
    shouldAcknowledge = true;

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
      shouldAcknowledge = true;
    } else {
      await redis.lpush(
        failedQueueName,
        JSON.stringify({
          failedAt: new Date().toISOString(),
          error: details,
          attempt,
          payload: message.payload,
        }),
      );
      shouldAcknowledge = true;
      if (parsed) {
        await reportFailure(parsed, details);
      }
    }
  } finally {
    if (shouldAcknowledge) {
      try {
        await acknowledgeMessage(message.id);
      } catch (ackError) {
        const details = ackError instanceof Error ? ackError.message : String(ackError);
        console.error(`[analyzer-worker] message ack failed id=${message.id} err=${details}`);
      }
    } else {
      console.warn(
        `[analyzer-worker] message left pending id=${message.id} for reclaim`,
      );
    }
  }
}

async function workerLoop() {
  console.info(`[analyzer-worker] listening stream=${queueName} group=${groupName} consumer=${consumerName}`);
  await ensureConsumerGroup();

  while (!shuttingDown) {
    try {
      await drainRetryQueue();

      const claimed = await claimStalePendingMessages();
      for (const message of claimed) {
        await handleMessage(message);
      }

      const freshMessages = await readNewMessages();
      for (const message of freshMessages) {
        await handleMessage(message);
      }
    } catch (error) {
      const details = error instanceof Error ? error.message : String(error);
      console.error(`[analyzer-worker] loop failed err=${details}`);
      await sleep(1_000);
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
