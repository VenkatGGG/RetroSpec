export interface AnalyzerWorkerConfig {
  redisHost: string;
  redisPort: number;
  queueName: string;
  orchestratorBaseUrl: string;
  internalApiKey: string;
  provider: "heuristic" | "remote_text";
  textModelEndpoint: string;
  modelApiKey: string;
  modelTimeoutMs: number;
  fallbackToHeuristic: boolean;
  minAcceptConfidence: number;
  discardUncertain: boolean;
  processingStaleSec: number;
  maxAttempts: number;
  retryBaseMs: number;
  dedupeWindowSec: number;
  s3Endpoint?: string;
  s3Region: string;
  s3AccessKey: string;
  s3SecretKey: string;
  s3Bucket: string;
}

export function loadConfig(): AnalyzerWorkerConfig {
  return {
    redisHost: process.env.REDIS_HOST ?? "localhost",
    redisPort: envOrDefaultNumber("REDIS_PORT", 6379),
    queueName: process.env.ANALYSIS_QUEUE_NAME ?? "analysis-jobs",
    orchestratorBaseUrl: process.env.ORCHESTRATOR_BASE_URL ?? "http://localhost:8080",
    internalApiKey: process.env.INTERNAL_API_KEY ?? "",
    provider: parseProvider(process.env.ANALYZER_PROVIDER),
    textModelEndpoint: process.env.ANALYZER_TEXT_MODEL_ENDPOINT ?? "",
    modelApiKey: process.env.ANALYZER_MODEL_API_KEY ?? "",
    modelTimeoutMs: envOrDefaultNumber("ANALYZER_MODEL_TIMEOUT_MS", 20_000),
    fallbackToHeuristic: (process.env.ANALYZER_FALLBACK_TO_HEURISTIC ?? "true").toLowerCase() !== "false",
    minAcceptConfidence: clamp01(envOrDefaultNumber("ANALYZER_MIN_ACCEPT_CONFIDENCE", 0.6)),
    discardUncertain: (process.env.ANALYZER_DISCARD_UNCERTAIN ?? "true").toLowerCase() !== "false",
    processingStaleSec: envOrDefaultNumber("ANALYZER_PROCESSING_STALE_SEC", 900),
    maxAttempts: envOrDefaultNumber("ANALYZER_MAX_ATTEMPTS", 3),
    retryBaseMs: envOrDefaultNumber("ANALYZER_RETRY_BASE_MS", 2_000),
    dedupeWindowSec: envOrDefaultNumber("ANALYZER_DEDUPE_WINDOW_SEC", 21_600),
    s3Endpoint: process.env.S3_ENDPOINT,
    s3Region: process.env.S3_REGION ?? "us-east-1",
    s3AccessKey: process.env.S3_ACCESS_KEY ?? "minioadmin",
    s3SecretKey: process.env.S3_SECRET_KEY ?? "minioadmin",
    s3Bucket: process.env.S3_BUCKET ?? "retrospec-artifacts",
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

function parseProvider(rawValue: string | undefined): "heuristic" | "remote_text" {
  const normalized = (rawValue ?? "").trim().toLowerCase();
  if (normalized === "remote_text" || normalized === "remote" || normalized === "dual_http") {
    return "remote_text";
  }
  return "heuristic";
}

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
