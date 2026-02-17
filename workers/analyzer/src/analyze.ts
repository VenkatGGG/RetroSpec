import type { AnalysisJobData, AnalysisReport, TriggerKind } from "./types.js";

interface RRWebEvent {
  type?: unknown;
  timestamp?: unknown;
  data?: unknown;
}

interface SignalHints {
  technicalRootCause: string;
  suggestedFix: string;
  textFinding: string;
  visualFinding: string;
  confidenceBoost: number;
}

const baseConfidence: Record<TriggerKind, number> = {
  ui_no_effect: 0.56,
  validation_failed: 0.62,
  api_error: 0.67,
  js_exception: 0.74,
};

function toIntOffsets(offsets: number[]): number[] {
  return offsets
    .filter((offset) => Number.isFinite(offset) && offset >= 0)
    .map((offset) => Math.floor(offset))
    .sort((a, b) => a - b);
}

function normalizeEvents(raw: unknown): RRWebEvent[] {
  if (!Array.isArray(raw)) {
    throw new Error("rrweb events payload is not an array");
  }
  return raw as RRWebEvent[];
}

function stringifySlice(events: RRWebEvent[]): string {
  try {
    return JSON.stringify(events).toLowerCase();
  } catch {
    return "";
  }
}

function eventsNearMarkers(events: RRWebEvent[], markerOffsetsMs: number[]): RRWebEvent[] {
  if (events.length === 0 || markerOffsetsMs.length === 0) {
    return [];
  }

  const firstTimestamp =
    typeof events[0]?.timestamp === "number" ? (events[0].timestamp as number) : 0;
  const windows = markerOffsetsMs.map((offset) => ({
    start: Math.max(0, firstTimestamp + offset - 2_000),
    end: firstTimestamp + offset + 2_000,
  }));

  const selected: RRWebEvent[] = [];
  for (const event of events) {
    if (selected.length >= 300) {
      break;
    }
    if (typeof event.timestamp !== "number") {
      continue;
    }
    const ts = event.timestamp;
    const inWindow = windows.some((window) => ts >= window.start && ts <= window.end);
    if (inWindow) {
      selected.push(event);
    }
  }

  return selected;
}

function inferSignalHints(triggerKind: TriggerKind, searchText: string): SignalHints {
  if (triggerKind === "validation_failed") {
    if (/(email|format|invalid|required|constraint)/.test(searchText)) {
      return {
        technicalRootCause:
          "Client-side validation appears stricter than expected input, causing valid user input to be rejected.",
        suggestedFix:
          "Review frontend validation schema and backend input contract for this form and align accepted patterns.",
        textFinding: "Validation-related tokens were detected near the marker windows.",
        visualFinding:
          "Repeated form interaction near the same marker windows suggests users could not move past validation.",
        confidenceBoost: 0.14,
      };
    }
    return {
      technicalRootCause:
        "A form validation path blocked progression after user interaction and no successful transition was observed.",
      suggestedFix:
        "Inspect submit-handler validation guards and ensure valid payloads can pass with expected field formats.",
      textFinding: "No explicit validation token found; classification is based on captured failure kind.",
      visualFinding: "Markers are concentrated in form-action windows without follow-up state transition.",
      confidenceBoost: 0.05,
    };
  }

  if (triggerKind === "api_error") {
    if (/(status|4\\d\\d|5\\d\\d|network|fetch|xhr|timeout|bad request)/.test(searchText)) {
      return {
        technicalRootCause:
          "Network/API request failure was observed around the interaction, preventing the intended UI transition.",
        suggestedFix:
          "Inspect request payload construction and backend response codes for the failing endpoint in this route.",
        textFinding: "HTTP/network failure tokens were detected around marker windows.",
        visualFinding: "User repeated the same action without successful completion after request attempts.",
        confidenceBoost: 0.12,
      };
    }
    return {
      technicalRootCause:
        "API-dependent flow did not complete after user action; response handling likely prevented state update.",
      suggestedFix:
        "Trace the request/response handler chain for this action and harden fallback UI on non-2xx responses.",
      textFinding: "No concrete status token found; inferred from marker kind and repeated action pattern.",
      visualFinding: "Markers indicate action retries without success signal in the replay timeline.",
      confidenceBoost: 0.06,
    };
  }

  if (triggerKind === "js_exception") {
    if (/(exception|undefined|null|cannot read|typeerror|referenceerror)/.test(searchText)) {
      return {
        technicalRootCause:
          "A client runtime exception likely interrupted the interaction path before completion.",
        suggestedFix:
          "Check the failing route/component for unchecked null/undefined values and add defensive guards.",
        textFinding: "Runtime exception keywords were detected in nearby event payload data.",
        visualFinding: "Interaction stalls occur immediately after markers, consistent with script interruption.",
        confidenceBoost: 0.12,
      };
    }
    return {
      technicalRootCause:
        "Client-side script execution likely failed during interaction processing.",
      suggestedFix:
        "Audit event handlers on this route and capture stack traces for uncaught exceptions in production telemetry.",
      textFinding: "No specific exception token found; inference uses marker classification.",
      visualFinding: "Markers cluster at interaction points followed by missing expected UI progression.",
      confidenceBoost: 0.06,
    };
  }

  if (/(click|submit|button|pointer|target)/.test(searchText)) {
    return {
      technicalRootCause:
        "User actions were captured, but the UI did not transition, indicating a likely no-op action path.",
      suggestedFix:
        "Verify click handlers, disabled/loading states, and action guards for this route to ensure side effects fire.",
      textFinding: "Input/interaction tokens appear around markers without matching completion signals.",
      visualFinding: "Repeated interactions around same offsets indicate perceived non-responsive UI.",
      confidenceBoost: 0.1,
    };
  }

  return {
    technicalRootCause:
      "The session captured repeated user actions without expected state change, indicating a non-responsive interaction path.",
    suggestedFix:
      "Inspect route-level action handlers and asynchronous state transitions for missing success/failure state updates.",
    textFinding: "No strong textual clue detected; classification is based on behavioral markers.",
    visualFinding: "Rage-click style retry pattern inferred from marker offsets.",
    confidenceBoost: 0.05,
  };
}

function symptomFor(triggerKind: TriggerKind, markerCount: number, route: string): string {
  const repeated = markerCount > 1 ? `${markerCount} repeated attempts` : "single failure";
  switch (triggerKind) {
    case "validation_failed":
      return `Validation blocked progression on ${route} (${repeated}).`;
    case "api_error":
      return `API-dependent action failed on ${route} (${repeated}).`;
    case "js_exception":
      return `Runtime exception interrupted interaction on ${route} (${repeated}).`;
    default:
      return `UI action had no visible effect on ${route} (${repeated}).`;
  }
}

export function analyzeSession(
  job: AnalysisJobData,
  rawEvents: unknown,
  generatedAt: Date,
): AnalysisReport {
  const events = normalizeEvents(rawEvents);
  const offsets = toIntOffsets(job.markerOffsetsMs);
  const markerCount = offsets.length;
  const firstOffset = markerCount > 0 ? offsets[0] : 0;
  const lastOffset = markerCount > 0 ? offsets[markerCount - 1] : 0;

  const nearbyEvents = eventsNearMarkers(events, offsets);
  const searchText = stringifySlice(nearbyEvents).slice(0, 40_000);
  const hints = inferSignalHints(job.triggerKind, searchText);

  let confidence = baseConfidence[job.triggerKind] + hints.confidenceBoost;
  if (events.length < 20) {
    confidence -= 0.08;
  }
  if (markerCount >= 3) {
    confidence += 0.04;
  }
  confidence = Math.max(0, Math.min(1, confidence));

  const textSummary = [
    `Events scanned: ${events.length}.`,
    `Markers: ${markerCount} (${firstOffset}ms to ${lastOffset}ms).`,
    hints.textFinding,
  ].join(" ");
  const visualSummary = [
    `Route: ${job.route}.`,
    `Site: ${job.site}.`,
    hints.visualFinding,
  ].join(" ");

  return {
    projectId: job.projectId,
    sessionId: job.sessionId,
    status: "ready",
    symptom: symptomFor(job.triggerKind, markerCount, job.route),
    technicalRootCause: hints.technicalRootCause,
    suggestedFix: hints.suggestedFix,
    textSummary,
    visualSummary,
    confidence,
    generatedAt: generatedAt.toISOString(),
  };
}
