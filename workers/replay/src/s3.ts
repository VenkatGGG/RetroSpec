import {
  GetObjectCommand,
  PutObjectCommand,
  S3Client,
} from "@aws-sdk/client-s3";
import { loadConfig } from "./config.js";

const config = loadConfig();

export const s3Client = new S3Client({
  region: config.s3Region,
  endpoint: config.s3Endpoint,
  forcePathStyle: Boolean(config.s3Endpoint),
  credentials: {
    accessKeyId: config.s3AccessKey,
    secretAccessKey: config.s3SecretKey,
  },
});

async function streamToString(stream: NodeJS.ReadableStream): Promise<string> {
  const chunks: Buffer[] = [];

  for await (const chunk of stream) {
    chunks.push(Buffer.isBuffer(chunk) ? chunk : Buffer.from(chunk));
  }

  return Buffer.concat(chunks).toString("utf-8");
}

export async function loadEventsBlob(objectKey: string): Promise<unknown> {
  const response = await s3Client.send(
    new GetObjectCommand({
      Bucket: config.s3Bucket,
      Key: objectKey,
    }),
  );

  if (!response.Body) {
    throw new Error(`events object is empty: ${objectKey}`);
  }

  const raw = await streamToString(response.Body as NodeJS.ReadableStream);
  return JSON.parse(raw);
}

export async function storeArtifact(objectKey: string, payload: unknown): Promise<string> {
  await s3Client.send(
    new PutObjectCommand({
      Bucket: config.s3Bucket,
      Key: objectKey,
      Body: JSON.stringify(payload),
      ContentType: "application/json",
    }),
  );

  return objectKey;
}
