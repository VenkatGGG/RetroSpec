export type MarkerKind = "validation_failed" | "api_error" | "js_exception" | "ui_no_effect";

export interface RetrospecMarker {
  id: string;
  clusterKey: string;
  label: string;
  evidence?: string;
  replayOffsetMs: number;
  kind: MarkerKind;
}

export interface RetrospecInitOptions {
  apiBaseUrl: string;
  apiKey?: string;
  site: string;
  route?: string;
  sampleRate?: number;
  maxEvents?: number;
  autoFlushMs?: number;
  recordConsole?: boolean;
  recordNetwork?: boolean;
  recordXHR?: boolean;
  detectRageClicks?: boolean;
  detectFormValidation?: boolean;
  detectReloadLoops?: boolean;
  rageClickThreshold?: number;
  rageClickWindowMs?: number;
  reloadLoopThreshold?: number;
  reloadLoopWindowMs?: number;
  maskAllInputs?: boolean;
  maskInputOptions?: Record<string, boolean>;
  debug?: boolean;
}

export interface FlushResult {
  sessionId: string;
  eventsObjectKey: string | null;
  ingestAccepted: boolean;
}

export interface RetrospecClient {
  sessionId: string;
  stop: () => Promise<FlushResult>;
  flush: () => Promise<FlushResult>;
}
