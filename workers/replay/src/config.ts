export interface ReplayWorkerConfig {
  redisHost: string;
  redisPort: number;
  queueName: string;
  orchestratorBaseUrl: string;
  internalApiKey: string;
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
    redisPort: Number(process.env.REDIS_PORT ?? 6379),
    queueName: process.env.REPLAY_QUEUE_NAME ?? "replay-jobs",
    orchestratorBaseUrl: process.env.ORCHESTRATOR_BASE_URL ?? "http://localhost:8080",
    internalApiKey: process.env.INTERNAL_API_KEY ?? "",
    s3Endpoint: process.env.S3_ENDPOINT,
    s3Region: process.env.S3_REGION ?? "us-east-1",
    s3AccessKey: process.env.S3_ACCESS_KEY ?? "minioadmin",
    s3SecretKey: process.env.S3_SECRET_KEY ?? "minioadmin",
    s3Bucket: process.env.S3_BUCKET ?? "retrospec-artifacts",
    artifactPrefix: process.env.REPLAY_ARTIFACT_PREFIX ?? "replay-artifacts/",
  };
}
