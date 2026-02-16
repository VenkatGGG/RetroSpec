export interface ReplayJobData {
  projectId: string;
  sessionId: string;
  eventsObjectKey: string;
  markerOffsetsMs: number[];
  triggerKind: "api_error" | "js_exception" | "validation_failed" | "ui_no_effect";
}

export interface MarkerWindow {
  startMs: number;
  endMs: number;
}

export interface ReplayResult {
  sessionId: string;
  artifactKey: string;
  markerWindows: MarkerWindow[];
  generatedAt: string;
  videoArtifactKey?: string;
}

export interface ReplayArtifactReport {
  projectId: string;
  sessionId: string;
  triggerKind: ReplayJobData["triggerKind"];
  artifactType: "analysis_json" | "replay_video";
  artifactKey: string;
  status: "ready" | "failed";
  generatedAt: string;
  windows: MarkerWindow[];
}
