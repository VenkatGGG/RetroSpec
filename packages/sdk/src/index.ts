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
  flushed: boolean;
}

const DEFAULTS = {
  sampleRate: 1,
  maxEvents: 50_000,
  autoFlushMs: 60_000,
  recordConsole: true,
  recordNetwork: true,
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
    flushed: false,
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

  let autoFlushTimer: number | undefined;
  if (config.autoFlushMs > 0) {
    autoFlushTimer = window.setInterval(() => {
      void flush();
    }, config.autoFlushMs);
  }

  const flush = async (): Promise<FlushResult> => {
    if (state.flushed || state.events.length === 0) {
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

    await ingestSession(config.apiBaseUrl, config.apiKey, {
      session: {
        id: state.sessionId,
        site: config.site,
        route: config.route ?? window.location.pathname,
        startedAt: state.startedAt.toISOString(),
        durationMs: Date.now() - state.startedAt.getTime(),
        eventsObjectKey,
      },
      markers: state.markers,
    });

    state.flushed = true;

    if (config.debug) {
      // eslint-disable-next-line no-console
      console.info("[retrospec] flushed", {
        sessionId: state.sessionId,
        eventsObjectKey,
        events: state.events.length,
        markers: state.markers.length,
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
  state.markers.push({
    id: generateId(),
    clusterKey: `${input.kind}:${input.clusterHint}`,
    label: input.label.slice(0, 220),
    replayOffsetMs: Math.max(0, offset),
    kind: input.kind,
  });
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
