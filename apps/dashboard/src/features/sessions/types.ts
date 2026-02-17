export type FailureKind =
  | "validation_failed"
  | "api_error"
  | "js_exception"
  | "ui_no_effect";

export interface ErrorMarker {
  id: string;
  clusterKey: string;
  label: string;
  replayOffsetMs: number;
  kind: FailureKind;
}

export interface ArtifactWindow {
  startMs: number;
  endMs: number;
}

export interface SessionArtifact {
  id: string;
  projectId: string;
  sessionId: string;
  artifactType: string;
  artifactKey: string;
  triggerKind: FailureKind;
  status: string;
  generatedAt: string;
  createdAt: string;
  updatedAt: string;
  windows: ArtifactWindow[];
}

export interface SessionReportCard {
  id: string;
  projectId: string;
  sessionId: string;
  status: "pending" | "ready" | "failed";
  symptom: string;
  technicalRootCause: string;
  suggestedFix: string;
  textSummary: string;
  visualSummary: string;
  confidence: number;
  generatedAt: string;
  createdAt: string;
  updatedAt: string;
}

export interface SessionSummary {
  id: string;
  site: string;
  route: string;
  startedAt: string;
  durationMs: number;
  eventsObjectKey?: string;
  createdAt?: string;
  markers: ErrorMarker[];
  artifacts: SessionArtifact[];
  reportCard?: SessionReportCard;
}

export interface IssueCluster {
  key: string;
  symptom: string;
  sessionCount: number;
  userCount: number;
  confidence: number;
  lastSeenAt: string;
  representativeSessionId: string;
}

export interface IssueKindStat {
  kind: string;
  markerCount: number;
  sessionCount: number;
  clusterCount: number;
  lastSeenAt: string;
}

export interface IssueClusterSession {
  sessionId: string;
  projectId: string;
  site: string;
  route: string;
  startedAt: string;
  durationMs: number;
  lastObservedAt: string;
  markerCount: number;
  reportStatus: "pending" | "ready" | "failed" | string;
  reportConfidence: number;
  reportSymptom: string;
}

export interface SessionsState {
  activeMarkerId: string | null;
}
