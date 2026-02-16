import { QueueEvents, Worker } from "bullmq";
import { z } from "zod";
import { loadConfig } from "./config.js";
import { processReplayJob } from "./reconstruct.js";

const replayJobSchema = z.object({
  sessionId: z.string().min(1),
  eventsObjectKey: z.string().min(1),
  markerOffsetsMs: z.array(z.number().int().nonnegative()).min(1),
  triggerKind: z.enum(["api_error", "js_exception", "validation_failed", "ui_no_effect"]),
});

const config = loadConfig();
const connection = {
  host: config.redisHost,
  port: config.redisPort,
};

const worker = new Worker(
  config.queueName,
  async (job) => {
    const data = replayJobSchema.parse(job.data);
    const result = await processReplayJob(data);
    return result;
  },
  {
    connection,
    concurrency: 2,
  },
);

const events = new QueueEvents(config.queueName, { connection });

worker.on("ready", () => {
  console.info(`[replay-worker] listening queue=${config.queueName}`);
});

worker.on("completed", (job, result) => {
  console.info(`[replay-worker] completed job=${job.id} artifact=${result.artifactKey}`);
});

worker.on("failed", (job, err) => {
  console.error(`[replay-worker] failed job=${job?.id}`, err);
});

events.on("error", (err) => {
  console.error("[replay-worker] queue events error", err);
});

process.on("SIGINT", async () => {
  await worker.close();
  await events.close();
  process.exit(0);
});

process.on("SIGTERM", async () => {
  await worker.close();
  await events.close();
  process.exit(0);
});
