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
  breadcrumbs: string[];
  lastFlushedMarkerCount: number;
  lastFlushedEventTimestamp: number;
}

const DEFAULTS = {
  sampleRate: 1,
  maxEvents: 50_000,
  autoFlushMs: 60_000,
  recordConsole: true,
  recordNetwork: true,
  recordXHR: true,
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
    breadcrumbs: [],
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
    if (config.recordXHR) {
      cleanupFns.push(instrumentXHRFailures(state));
    }
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
    const source = `${event.filename || "inline"}:${event.lineno || 0}:${event.colno || 0}`;
    const stack = toStackSnippet(event.error);
    appendBreadcrumb(state, `js_error:${source}:${normalizeToken(event.message || "error")}`);
    pushMarker(state, {
      kind: "js_exception",
      label: event.message || "Unhandled error",
      clusterHint: event.filename || "runtime",
      evidence: compactEvidence([source, stack, recentBreadcrumbs(state)]),
    });
  };

  const onRejection = (event: PromiseRejectionEvent) => {
    const reason = toErrorReason(event.reason);
    appendBreadcrumb(state, `promise_rejection:${normalizeToken(reason)}`);
    pushMarker(state, {
      kind: "js_exception",
      label: `Unhandled promise rejection: ${reason}`,
      clusterHint: "promise-rejection",
      evidence: compactEvidence([toStackSnippet(event.reason), recentBreadcrumbs(state)]),
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
    appendBreadcrumb(state, `console_error:${normalizeToken(stringifyConsoleArgs(args).slice(0, 60))}`);
    pushMarker(state, {
      kind: "js_exception",
      label: stringifyConsoleArgs(args),
      clusterHint: "console-error",
      evidence: compactEvidence([extractConsoleStack(args), recentBreadcrumbs(state)]),
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
        appendBreadcrumb(state, `fetch:${method}:${normalizePath(extractUrl(input))}:${response.status}`);
        pushMarker(state, {
          kind: "api_error",
          label: `${method} ${extractUrl(input)} -> ${response.status}`,
          clusterHint: `${method}:${normalizePath(extractUrl(input))}:${Math.floor(response.status / 100)}xx`,
          evidence: compactEvidence([
            `statusText=${response.statusText || "unknown"}`,
            `route=${normalizePath(window.location.pathname)}`,
            recentBreadcrumbs(state),
          ]),
        });
      }
      return response;
    } catch (error) {
      const reason = toErrorReason(error);
      appendBreadcrumb(state, `fetch:${method}:${normalizePath(extractUrl(input))}:network`);
      pushMarker(state, {
        kind: "api_error",
        label: `${method} ${extractUrl(input)} -> network_error`,
        clusterHint: `${method}:${normalizePath(extractUrl(input))}:network`,
        evidence: compactEvidence([`reason=${reason}`, toStackSnippet(error), recentBreadcrumbs(state)]),
      });
      throw error;
    }
  };

  return () => {
    window.fetch = originalFetch;
  };
}

function instrumentXHRFailures(state: InternalState): () => void {
  const originalOpen = XMLHttpRequest.prototype.open;
  const originalSend = XMLHttpRequest.prototype.send;
  const metaMap = new WeakMap<XMLHttpRequest, { method: string; url: string }>();

  XMLHttpRequest.prototype.open = function (
    this: XMLHttpRequest,
    method: string,
    url: string | URL,
    ...rest: unknown[]
  ) {
    const normalizedMethod = String(method || "GET").toUpperCase();
    metaMap.set(this, {
      method: normalizedMethod,
      url: String(url),
    });

    return (originalOpen as (...args: unknown[]) => void).call(this, method, url, ...rest);
  };

  XMLHttpRequest.prototype.send = function (this: XMLHttpRequest, body?: Document | BodyInit | null) {
    const requestMeta = metaMap.get(this) ?? { method: "GET", url: "unknown" };
    const method = requestMeta.method;
    const url = requestMeta.url;
    const cleanup = () => {
      this.removeEventListener("loadend", onLoadEnd);
      this.removeEventListener("error", onError);
      this.removeEventListener("timeout", onTimeout);
      this.removeEventListener("abort", onAbort);
    };
    const onLoadEnd = () => {
      if (this.status >= 400) {
        appendBreadcrumb(state, `xhr:${method}:${normalizePath(url)}:${this.status}`);
        pushMarker(state, {
          kind: "api_error",
          label: `${method} ${url} -> ${this.status}`,
          clusterHint: `${method}:${normalizePath(url)}:${Math.floor(this.status / 100)}xx`,
          evidence: compactEvidence([
            `statusText=${this.statusText || "unknown"}`,
            `route=${normalizePath(window.location.pathname)}`,
            recentBreadcrumbs(state),
          ]),
        });
      }
      cleanup();
    };
    const onError = () => {
      appendBreadcrumb(state, `xhr:${method}:${normalizePath(url)}:network`);
      pushMarker(state, {
        kind: "api_error",
        label: `${method} ${url} -> network_error`,
        clusterHint: `${method}:${normalizePath(url)}:network`,
        evidence: compactEvidence([`statusText=${this.statusText || "unknown"}`, recentBreadcrumbs(state)]),
      });
      cleanup();
    };
    const onTimeout = () => {
      appendBreadcrumb(state, `xhr:${method}:${normalizePath(url)}:timeout`);
      pushMarker(state, {
        kind: "api_error",
        label: `${method} ${url} -> timeout`,
        clusterHint: `${method}:${normalizePath(url)}:timeout`,
        evidence: compactEvidence([`statusText=${this.statusText || "unknown"}`, recentBreadcrumbs(state)]),
      });
      cleanup();
    };
    const onAbort = () => {
      cleanup();
    };

    this.addEventListener("loadend", onLoadEnd);
    this.addEventListener("error", onError);
    this.addEventListener("timeout", onTimeout);
    this.addEventListener("abort", onAbort);
    return originalSend.call(this, body as any);
  };

  return () => {
    XMLHttpRequest.prototype.open = originalOpen;
    XMLHttpRequest.prototype.send = originalSend;
  };
}

function pushMarker(
  state: InternalState,
  input: { kind: MarkerKind; label: string; clusterHint: string; evidence?: string },
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
    evidence: (input.evidence || "").trim().slice(0, 180),
    replayOffsetMs: Math.max(0, offset),
    kind: input.kind,
  });
}

function appendBreadcrumb(state: InternalState, value: string): void {
  const trimmed = value.trim();
  if (!trimmed) {
    return;
  }
  state.breadcrumbs.push(trimmed.slice(0, 140));
  if (state.breadcrumbs.length > 20) {
    state.breadcrumbs.splice(0, state.breadcrumbs.length - 20);
  }
}

function recentBreadcrumbs(state: InternalState): string {
  if (state.breadcrumbs.length === 0) {
    return "";
  }
  return `trail=${state.breadcrumbs.slice(-3).join(" <- ")}`;
}

function compactEvidence(items: Array<string | undefined>): string {
  return items
    .map((item) => (item || "").trim())
    .filter((item) => item.length > 0)
    .join(" | ")
    .slice(0, 150);
}

function toErrorReason(value: unknown): string {
  if (value instanceof Error) {
    return value.message || value.name;
  }
  if (typeof value === "string") {
    return value;
  }
  try {
    return JSON.stringify(value);
  } catch {
    return String(value);
  }
}

function toStackSnippet(value: unknown): string {
  if (value instanceof Error && value.stack) {
    return value.stack.split("\n").slice(0, 2).join(" ").slice(0, 120);
  }
  if (value && typeof value === "object" && "stack" in (value as Record<string, unknown>)) {
    const stackValue = (value as Record<string, unknown>).stack;
    if (typeof stackValue === "string") {
      return stackValue.split("\n").slice(0, 2).join(" ").slice(0, 120);
    }
  }
  return "";
}

function extractConsoleStack(args: unknown[]): string {
  for (const arg of args) {
    const stack = toStackSnippet(arg);
    if (stack) {
      return stack;
    }
  }
  return "";
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
      if (!firstClick || now - firstClick.at <= windowMs) {
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
    const reason = detectValidationReason(event.target);
    const reasonToken = normalizeToken(reason || "validation");
    pushMarker(state, {
      kind: "validation_failed",
      label: reason
        ? `Validation blocked submission: ${fieldName} (${reason})`
        : `Validation blocked submission: ${fieldName}`,
      clusterHint: `invalid:${normalizeToken(signature)}:${reasonToken}:${normalizePath(window.location.pathname)}`,
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

function detectValidationReason(element: Element): string {
  if (
    element instanceof HTMLInputElement ||
    element instanceof HTMLSelectElement ||
    element instanceof HTMLTextAreaElement
  ) {
    return (element.validationMessage || "").trim().slice(0, 120);
  }

  return "";
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
