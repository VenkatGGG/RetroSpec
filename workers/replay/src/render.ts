import { mkdtemp, readFile, rm } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import type { Browser, BrowserContext, Page } from "playwright";
import { chromium } from "playwright";
import type { ReplayWorkerConfig } from "./config.js";

const rrwebPlayerCSS = "https://cdn.jsdelivr.net/npm/rrweb-player@latest/dist/style.css";
const rrwebPlayerScript = "https://cdn.jsdelivr.net/npm/rrweb-player@latest/dist/index.js";

function playbackDurationMs(events: unknown[], speedRaw: number, maxDurationRaw: number): number {
  const speed = Number.isFinite(speedRaw) && speedRaw > 0 ? speedRaw : 1;
  const maxDurationMs = Math.max(5_000, Math.floor(maxDurationRaw));

  const firstEvent = events[0] as { timestamp?: unknown } | undefined;
  const lastEvent = events[events.length - 1] as { timestamp?: unknown } | undefined;

  const start = typeof firstEvent?.timestamp === "number" ? firstEvent.timestamp : Date.now();
  const end = typeof lastEvent?.timestamp === "number" ? lastEvent.timestamp : start + 15_000;
  const timelineMs = Math.max(1_000, end - start);

  return Math.min(maxDurationMs, Math.ceil(timelineMs / speed) + 1_500);
}

async function closeQuietly(resource: { close: () => Promise<void> } | null): Promise<void> {
  if (!resource) {
    return;
  }

  try {
    await resource.close();
  } catch {
    // Ignore close errors during cleanup.
  }
}

export async function renderReplayWebm(
  events: unknown[],
  config: ReplayWorkerConfig,
): Promise<Uint8Array> {
  if (events.length === 0) {
    throw new Error("cannot render replay video with empty events list");
  }

  const width = Math.max(640, Math.floor(config.renderViewportWidth));
  const height = Math.max(360, Math.floor(config.renderViewportHeight));
  const tempDir = await mkdtemp(path.join(os.tmpdir(), "retrospec-render-"));

  let browser: Browser | null = null;
  let context: BrowserContext | null = null;
  let page: Page | null = null;
  let contextClosed = false;

  try {
    browser = await chromium.launch({ headless: true });
    context = await browser.newContext({
      viewport: { width, height },
      recordVideo: {
        dir: tempDir,
        size: { width, height },
      },
    });
    page = await context.newPage();

    await page.setContent(
      `<!doctype html>
      <html>
        <head>
          <meta charset="utf-8" />
          <link rel="stylesheet" href="${rrwebPlayerCSS}" />
          <style>
            body {
              margin: 0;
              background: #0f172a;
            }
            #replay-root {
              width: ${width}px;
              height: ${height}px;
              margin: 0 auto;
            }
          </style>
        </head>
        <body>
          <div id="replay-root"></div>
          <script src="${rrwebPlayerScript}"></script>
        </body>
      </html>`,
      { waitUntil: "domcontentloaded" },
    );

    await page.waitForFunction(
      () => typeof (window as { rrwebPlayer?: unknown }).rrwebPlayer === "function",
      undefined,
      { timeout: 15_000 },
    );

    await page.evaluate(
      ({ replayEvents, replayWidth, replayHeight, replaySpeed }) => {
        const Player = (window as { rrwebPlayer?: any }).rrwebPlayer;
        if (typeof Player !== "function") {
          throw new Error("rrweb-player script unavailable");
        }

        const root = document.getElementById("replay-root");
        if (!root) {
          throw new Error("missing replay root");
        }

        const player = new Player({
          target: root,
          props: {
            events: replayEvents,
            autoPlay: false,
            speed: replaySpeed,
            showController: false,
            width: replayWidth,
            height: replayHeight,
          },
        });
        player.play();
      },
      {
        replayEvents: events,
        replayWidth: width,
        replayHeight: height,
        replaySpeed: config.renderSpeed,
      },
    );

    const duration = playbackDurationMs(events, config.renderSpeed, config.renderMaxDurationMs);
    await page.waitForTimeout(duration);

    const video = page.video();
    if (!video) {
      throw new Error("playwright video capture is not available");
    }

    await context.close();
    contextClosed = true;

    const videoPath = await video.path();
    return await readFile(videoPath);
  } finally {
    if (!contextClosed) {
      await closeQuietly(context);
    }
    await closeQuietly(browser);
    await rm(tempDir, { recursive: true, force: true });
  }
}
