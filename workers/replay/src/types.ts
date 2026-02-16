export interface ReplayJobData {
  sessionId: string;
  eventsObjectKey: string;
  markerOffsetsMs: number[];
  triggerKind: "api_error" | "js_exception" | "validation_failed" | "ui_no_effect";
}

export interface ReplayResult {
  sessionId: string;
  artifactKey: string;
  markerWindows: Array<{ startMs: number; endMs: number }>;
  generatedAt: string;
}
