import { createHash } from "node:crypto";
import { hostname } from "node:os";
import { Redis } from "ioredis";
import { z } from "zod";
import { loadConfig } from "./config.js";
import { reportReplayArtifact } from "./orchestrator.js";
import { processReplayJob } from "./reconstruct.js";

interface StreamMessage {
  id: string;
  payload: string;
}

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

async function ensureConsumerGroup(): Promise<void> {
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

  const pipeline = redis.multi();
  for (const item of due) {
    pipeline.zrem(retryQueueName, item);
    pipeline.xadd(queueName, "*", "payload", item);
  }
  await pipeline.exec();
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

async function handleMessage(message: StreamMessage): Promise<void> {
  let parsed: z.infer<typeof replayJobSchema> | null = null;

  try {
    parsed = replayJobSchema.parse(JSON.parse(message.payload));
    const doneKey = dedupeKey(parsed);
    const alreadyDone = await redis.get(doneKey);
    if (alreadyDone) {
      console.info(
        `[replay-worker] skipped duplicate session=${parsed.sessionId} project=${parsed.projectId} attempt=${parsed.attempt}`,
      );
      return;
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
      await redis.lpush(
        failedQueueName,
        JSON.stringify({
          failedAt: new Date().toISOString(),
          error: details,
          attempt,
          payload: message.payload,
        }),
      );
    }
  } finally {
    try {
      await acknowledgeMessage(message.id);
    } catch (ackError) {
      const details = ackError instanceof Error ? ackError.message : String(ackError);
      console.error(`[replay-worker] message ack failed id=${message.id} err=${details}`);
    }
  }
}

async function workerLoop() {
  console.info(`[replay-worker] listening stream=${queueName} group=${groupName} consumer=${consumerName}`);
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
      console.error(`[replay-worker] loop failed err=${details}`);
      await sleep(1_000);
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
