import { record, type eventWithTime } from "rrweb";
import type {
  FlushResult,
  MarkerKind,
  RetrospecClient,
  RetrospecInitOptions,
  RetrospecMarker,
} from "./types.js";

interface InternalState {
  sessionId: string;
  startedAt: Date;
  events: eventWithTime[];
  markers: RetrospecMarker[];
  lastFlushedMarkerCount: number;
  lastFlushedEventTimestamp: number;
}

const DEFAULTS = {
  sampleRate: 1,
  maxEvents: 50_000,
  autoFlushMs: 60_000,
  recordConsole: true,
  recordNetwork: true,
  detectRageClicks: true,
  detectFormValidation: true,
  detectReloadLoops: true,
  rageClickThreshold: 3,
  rageClickWindowMs: 1_200,
  reloadLoopThreshold: 3,
  reloadLoopWindowMs: 15_000,
  debug: false,
};

export function initRetrospec(options: RetrospecInitOptions): RetrospecClient {
  const config = { ...DEFAULTS, ...options };

  if (Math.random() > config.sampleRate) {
    return {
      sessionId: "sampled-out",
      stop: async () => ({ sessionId: "sampled-out", eventsObjectKey: null, ingestAccepted: false }),
      flush: async () => ({ sessionId: "sampled-out", eventsObjectKey: null, ingestAccepted: false }),
    };
  }

  const state: InternalState = {
    sessionId: generateId(),
    startedAt: new Date(),
    events: [],
    markers: [],
    lastFlushedMarkerCount: 0,
    lastFlushedEventTimestamp: 0,
  };

  const stopRecording = record({
    emit(event) {
      state.events.push(event);
      if (state.events.length > config.maxEvents) {
        state.events.splice(0, state.events.length - config.maxEvents);
      }
    },
  });

  const cleanupFns: Array<() => void> = [];
  cleanupFns.push(instrumentJSErrors(state));

  if (config.recordConsole) {
    cleanupFns.push(instrumentConsoleErrors(state));
  }

  if (config.recordNetwork) {
    cleanupFns.push(instrumentFetchFailures(state));
  }
  if (config.detectRageClicks) {
    cleanupFns.push(instrumentRageClicks(state, config.rageClickThreshold, config.rageClickWindowMs));
  }
  if (config.detectFormValidation) {
    cleanupFns.push(instrumentValidationFailures(state));
  }
  if (config.detectReloadLoops) {
    detectReloadLoop(state, config.reloadLoopThreshold, config.reloadLoopWindowMs);
  }

  const onPageHide = () => {
    void flush();
  };
  window.addEventListener("pagehide", onPageHide);
  cleanupFns.push(() => {
    window.removeEventListener("pagehide", onPageHide);
  });

  let autoFlushTimer: number | undefined;
  if (config.autoFlushMs > 0) {
    autoFlushTimer = window.setInterval(() => {
      void flush();
    }, config.autoFlushMs);
  }

  const flush = async (): Promise<FlushResult> => {
    if (state.events.length === 0) {
      return {
        sessionId: state.sessionId,
        eventsObjectKey: null,
        ingestAccepted: false,
      };
    }

    const latestEventTimestamp = state.events[state.events.length - 1]?.timestamp ?? 0;
    const hasNewEvents = latestEventTimestamp > state.lastFlushedEventTimestamp;
    const hasNewMarkers = state.markers.length > state.lastFlushedMarkerCount;
    if (!hasNewEvents && !hasNewMarkers) {
      return {
        sessionId: state.sessionId,
        eventsObjectKey: null,
        ingestAccepted: false,
      };
    }

    const eventsObjectKey = await uploadSessionEvents(config.apiBaseUrl, config.apiKey, {
      sessionId: state.sessionId,
      site: config.site,
      events: state.events,
    });
    const newMarkers = state.markers.slice(state.lastFlushedMarkerCount);

    await ingestSession(config.apiBaseUrl, config.apiKey, {
      session: {
        id: state.sessionId,
        site: config.site,
        route: config.route ?? window.location.pathname,
        startedAt: state.startedAt.toISOString(),
        durationMs: Date.now() - state.startedAt.getTime(),
        eventsObjectKey,
      },
      markers: newMarkers,
    });

    state.lastFlushedEventTimestamp = latestEventTimestamp;
    state.lastFlushedMarkerCount = state.markers.length;

    if (config.debug) {
      // eslint-disable-next-line no-console
      console.info("[retrospec] flushed", {
        sessionId: state.sessionId,
        eventsObjectKey,
        events: state.events.length,
        markers: state.markers.length,
        markersSent: newMarkers.length,
      });
    }

    return {
      sessionId: state.sessionId,
      eventsObjectKey,
      ingestAccepted: true,
    };
  };

  const stop = async (): Promise<FlushResult> => {
    stopRecording?.();
    if (autoFlushTimer) {
      window.clearInterval(autoFlushTimer);
    }
    for (const cleanup of cleanupFns) {
      cleanup();
    }
    return flush();
  };

  return {
    sessionId: state.sessionId,
    flush,
    stop,
  };
}

async function uploadSessionEvents(
  apiBaseUrl: string,
  apiKey: string | undefined,
  payload: { sessionId: string; site: string; events: eventWithTime[] },
): Promise<string> {
  const headers: Record<string, string> = { "Content-Type": "application/json" };
  if (apiKey) {
    headers["X-Retrospec-Key"] = apiKey;
  }

  const response = await fetch(`${apiBaseUrl}/v1/artifacts/session-events`, {
    method: "POST",
    headers,
    body: JSON.stringify(payload),
  });

  if (!response.ok) {
    throw new Error(`events upload failed: ${response.status}`);
  }

  const body = (await response.json()) as { eventsObjectKey: string };
  return body.eventsObjectKey;
}

async function ingestSession(
  apiBaseUrl: string,
  apiKey: string | undefined,
  payload: {
    session: {
      id: string;
      site: string;
      route: string;
      startedAt: string;
      durationMs: number;
      eventsObjectKey: string;
    };
    markers: RetrospecMarker[];
  },
): Promise<void> {
  const headers: Record<string, string> = { "Content-Type": "application/json" };
  if (apiKey) {
    headers["X-Retrospec-Key"] = apiKey;
  }

  const response = await fetch(`${apiBaseUrl}/v1/ingest/session`, {
    method: "POST",
    headers,
    body: JSON.stringify(payload),
  });

  if (!response.ok) {
    throw new Error(`session ingest failed: ${response.status}`);
  }
}

function instrumentJSErrors(state: InternalState): () => void {
  const onError = (event: ErrorEvent) => {
    pushMarker(state, {
      kind: "js_exception",
      label: event.message || "Unhandled error",
      clusterHint: event.filename || "runtime",
    });
  };

  const onRejection = (event: PromiseRejectionEvent) => {
    pushMarker(state, {
      kind: "js_exception",
      label: `Unhandled promise rejection: ${String(event.reason)}`,
      clusterHint: "promise-rejection",
    });
  };

  window.addEventListener("error", onError);
  window.addEventListener("unhandledrejection", onRejection);

  return () => {
    window.removeEventListener("error", onError);
    window.removeEventListener("unhandledrejection", onRejection);
  };
}

function instrumentConsoleErrors(state: InternalState): () => void {
  const original = console.error;
  console.error = (...args: unknown[]) => {
    pushMarker(state, {
      kind: "js_exception",
      label: stringifyConsoleArgs(args),
      clusterHint: "console-error",
    });
    original(...args);
  };

  return () => {
    console.error = original;
  };
}

function instrumentFetchFailures(state: InternalState): () => void {
  const originalFetch = window.fetch.bind(window);
  window.fetch = async (...args: Parameters<typeof fetch>) => {
    const [input, init] = args;
    const method = (init?.method ?? "GET").toUpperCase();

    try {
      const response = await originalFetch(...args);
      if (response.status >= 400) {
        pushMarker(state, {
          kind: "api_error",
          label: `${method} ${extractUrl(input)} -> ${response.status}`,
          clusterHint: `${method}:${normalizePath(extractUrl(input))}:${Math.floor(response.status / 100)}xx`,
        });
      }
      return response;
    } catch (error) {
      pushMarker(state, {
        kind: "api_error",
        label: `${method} ${extractUrl(input)} -> network_error`,
        clusterHint: `${method}:${normalizePath(extractUrl(input))}:network`,
      });
      throw error;
    }
  };

  return () => {
    window.fetch = originalFetch;
  };
}

function pushMarker(
  state: InternalState,
  input: { kind: MarkerKind; label: string; clusterHint: string },
): void {
  const offset = Date.now() - state.startedAt.getTime();
  const clusterKey = `${input.kind}:${input.clusterHint}`;
  const previous = state.markers[state.markers.length - 1];
  if (
    previous &&
    previous.clusterKey === clusterKey &&
    Math.abs(previous.replayOffsetMs - offset) < 750
  ) {
    return;
  }

  state.markers.push({
    id: generateId(),
    clusterKey,
    label: input.label.slice(0, 220),
    replayOffsetMs: Math.max(0, offset),
    kind: input.kind,
  });
}

function instrumentRageClicks(
  state: InternalState,
  thresholdRaw: number,
  windowMsRaw: number,
): () => void {
  const threshold = Math.max(2, Math.floor(thresholdRaw));
  const windowMs = Math.max(500, Math.floor(windowMsRaw));
  const recentClicks: Array<{ at: number; signature: string }> = [];
  const emittedAt = new Map<string, number>();

  const onClick = (event: MouseEvent) => {
    const signature = event.target instanceof Element ? elementSignature(event.target) : "unknown";
    const now = Date.now();

    recentClicks.push({ at: now, signature });
    while (recentClicks.length > 0) {
      const firstClick = recentClicks[0];
      if (!firstClick || now-firstClick.at <= windowMs) {
        break;
      }
      recentClicks.shift();
    }

    const repeats = recentClicks.reduce((count, click) => {
      if (click.signature === signature) {
        return count + 1;
      }
      return count;
    }, 0);

    const lastEmittedAt = emittedAt.get(signature) ?? 0;
    if (repeats >= threshold && now - lastEmittedAt >= windowMs) {
      emittedAt.set(signature, now);
      pushMarker(state, {
        kind: "ui_no_effect",
        label: `Repeated clicks with no progress on ${signature}`,
        clusterHint: `rage:${normalizeToken(signature)}:${normalizePath(window.location.pathname)}`,
      });
    }
  };

  window.addEventListener("click", onClick, true);
  return () => {
    window.removeEventListener("click", onClick, true);
  };
}

function instrumentValidationFailures(state: InternalState): () => void {
  const onInvalid = (event: Event) => {
    if (!(event.target instanceof Element)) {
      return;
    }

    const signature = elementSignature(event.target);
    const fieldName = detectFieldName(event.target);
    pushMarker(state, {
      kind: "validation_failed",
      label: `Validation blocked submission: ${fieldName}`,
      clusterHint: `invalid:${normalizeToken(signature)}:${normalizePath(window.location.pathname)}`,
    });
  };

  window.addEventListener("invalid", onInvalid, true);
  return () => {
    window.removeEventListener("invalid", onInvalid, true);
  };
}

function detectReloadLoop(state: InternalState, thresholdRaw: number, windowMsRaw: number): void {
  const threshold = Math.max(2, Math.floor(thresholdRaw));
  const windowMs = Math.max(1_000, Math.floor(windowMsRaw));
  const storageKey = "__retrospec_recent_loads";
  const now = Date.now();

  try {
    const raw = window.localStorage.getItem(storageKey);
    const previous = raw ? (JSON.parse(raw) as unknown) : [];
    const numericTimestamps = Array.isArray(previous)
      ? previous.filter((item): item is number => typeof item === "number")
      : [];
    const recent = numericTimestamps.filter((timestamp) => now - timestamp <= windowMs);
    recent.push(now);

    window.localStorage.setItem(storageKey, JSON.stringify(recent.slice(-20)));
    if (recent.length >= threshold) {
      pushMarker(state, {
        kind: "ui_no_effect",
        label: `Rapid reload loop detected (${recent.length} reloads)`,
        clusterHint: `reload-loop:${normalizePath(window.location.pathname)}`,
      });
    }
  } catch {
    // Ignore storage failures (private mode or blocked storage).
  }
}

function detectFieldName(element: Element): string {
  if (
    element instanceof HTMLInputElement ||
    element instanceof HTMLSelectElement ||
    element instanceof HTMLTextAreaElement
  ) {
    return element.name || element.id || element.type || element.tagName.toLowerCase();
  }

  const id = element.getAttribute("id");
  if (id) {
    return id;
  }

  return element.tagName.toLowerCase();
}

function elementSignature(element: Element): string {
  const tag = element.tagName.toLowerCase();
  const id = element.getAttribute("id");
  const role = element.getAttribute("role");
  const testID = element.getAttribute("data-testid") || element.getAttribute("data-test-id");
  const aria = element.getAttribute("aria-label");
  const classes = Array.from(element.classList).slice(0, 2).map(normalizeToken).filter(Boolean);
  const text = normalizeToken((element.textContent ?? "").slice(0, 40));

  const tokens = [`tag:${tag}`];
  if (id) {
    tokens.push(`id:${normalizeToken(id)}`);
  }
  if (role) {
    tokens.push(`role:${normalizeToken(role)}`);
  }
  if (testID) {
    tokens.push(`test:${normalizeToken(testID)}`);
  }
  if (aria) {
    tokens.push(`aria:${normalizeToken(aria)}`);
  }
  if (classes.length > 0) {
    tokens.push(`class:${classes.join(".")}`);
  }
  if (text) {
    tokens.push(`text:${text}`);
  }

  return tokens.join("|").slice(0, 160);
}

function normalizeToken(value: string): string {
  return value
    .toLowerCase()
    .trim()
    .replace(/[^a-z0-9:_-]+/g, "-")
    .replace(/-+/g, "-")
    .replace(/^-|-$/g, "")
    .slice(0, 80);
}

function extractUrl(input: RequestInfo | URL): string {
  if (typeof input === "string") {
    return input;
  }
  if (input instanceof URL) {
    return input.toString();
  }
  return input.url;
}

function normalizePath(urlLike: string): string {
  try {
    const url = new URL(urlLike, window.location.origin);
    return `${url.origin}${url.pathname}`;
  } catch {
    return urlLike;
  }
}

function stringifyConsoleArgs(args: unknown[]): string {
  return args
    .map((arg) => {
      if (typeof arg === "string") {
        return arg;
      }
      try {
        return JSON.stringify(arg);
      } catch {
        return String(arg);
      }
    })
    .join(" ")
    .slice(0, 220);
}

function generateId(): string {
  if (typeof crypto !== "undefined" && typeof crypto.randomUUID === "function") {
    return crypto.randomUUID();
  }

  return `rs_${Date.now()}_${Math.random().toString(16).slice(2)}`;
}

export type { FlushResult, RetrospecClient, RetrospecInitOptions, RetrospecMarker } from "./types.js";
