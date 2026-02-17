export interface ReplayWorkerConfig {
  redisHost: string;
  redisPort: number;
  queueName: string;
  orchestratorBaseUrl: string;
  internalApiKey: string;
  renderEnabled: boolean;
  renderSpeed: number;
  renderMaxDurationMs: number;
  renderViewportWidth: number;
  renderViewportHeight: number;
  maxAttempts: number;
  retryBaseMs: number;
  dedupeWindowSec: number;
  processingStaleSec: number;
  s3Endpoint?: string;
  s3Region: string;
  s3AccessKey: string;
  s3SecretKey: string;
  s3Bucket: string;
  artifactPrefix: string;
}

export function loadConfig(): ReplayWorkerConfig {
  return {
    redisHost: process.env.REDIS_HOST ?? "localhost",
    redisPort: envOrDefaultNumber("REDIS_PORT", 6379),
    queueName: process.env.REPLAY_QUEUE_NAME ?? "replay-jobs",
    orchestratorBaseUrl: process.env.ORCHESTRATOR_BASE_URL ?? "http://localhost:8080",
    internalApiKey: process.env.INTERNAL_API_KEY ?? "",
    renderEnabled: (process.env.REPLAY_RENDER_ENABLED ?? "").toLowerCase() === "true",
    renderSpeed: envOrDefaultNumber("REPLAY_RENDER_SPEED", 4),
    renderMaxDurationMs: envOrDefaultNumber("REPLAY_RENDER_MAX_DURATION_MS", 120_000),
    renderViewportWidth: envOrDefaultNumber("REPLAY_RENDER_VIEWPORT_WIDTH", 1280),
    renderViewportHeight: envOrDefaultNumber("REPLAY_RENDER_VIEWPORT_HEIGHT", 720),
    maxAttempts: envOrDefaultNumber("REPLAY_MAX_ATTEMPTS", 3),
    retryBaseMs: envOrDefaultNumber("REPLAY_RETRY_BASE_MS", 2_000),
    dedupeWindowSec: envOrDefaultNumber("REPLAY_DEDUPE_WINDOW_SEC", 21_600),
    processingStaleSec: envOrDefaultNumber("REPLAY_PROCESSING_STALE_SEC", 900),
    s3Endpoint: process.env.S3_ENDPOINT,
    s3Region: process.env.S3_REGION ?? "us-east-1",
    s3AccessKey: process.env.S3_ACCESS_KEY ?? "minioadmin",
    s3SecretKey: process.env.S3_SECRET_KEY ?? "minioadmin",
    s3Bucket: process.env.S3_BUCKET ?? "retrospec-artifacts",
    artifactPrefix: process.env.REPLAY_ARTIFACT_PREFIX ?? "replay-artifacts/",
  };
}

function envOrDefaultNumber(key: string, fallback: number): number {
  const value = process.env[key];
  if (!value) {
    return fallback;
  }

  const parsed = Number(value);
  if (!Number.isFinite(parsed)) {
    return fallback;
  }
  return parsed;
}
