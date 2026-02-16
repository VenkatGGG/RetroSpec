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

export interface SessionSummary {
  id: string;
  site: string;
  route: string;
  startedAt: string;
  durationMs: number;
  markers: ErrorMarker[];
}

export interface IssueCluster {
  key: string;
  symptom: string;
  sessionCount: number;
  userCount: number;
  lastSeenAt: string;
  representativeSessionId: string;
}

export interface SessionsState {
  sessions: SessionSummary[];
  issueClusters: IssueCluster[];
  activeSessionId: string | null;
  activeMarkerId: string | null;
}
