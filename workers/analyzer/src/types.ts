export type TriggerKind =
  | "api_error"
  | "js_exception"
  | "validation_failed"
  | "ui_no_effect";

export interface AnalysisJobData {
  projectId: string;
  sessionId: string;
  eventsObjectKey: string;
  markerOffsetsMs: number[];
  markerHints: string[];
  triggerKind: TriggerKind;
  route: string;
  site: string;
  attempt?: number;
}

export interface AnalysisReport {
  projectId: string;
  sessionId: string;
  status: "pending" | "ready" | "failed" | "discarded";
  symptom: string;
  technicalRootCause: string;
  suggestedFix: string;
  textSummary: string;
  visualSummary: string;
  confidence: number;
  generatedAt: string;
}
