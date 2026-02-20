# RetroSpec SDK

Browser SDK that records rrweb events and reports session failures to the RetroSpec orchestrator.

## Install

```bash
npm install @retrospec/sdk
```

## Usage

```ts
import { initRetrospec } from "@retrospec/sdk";

const client = initRetrospec({
  apiBaseUrl: "http://localhost:8080",
  apiKey: "replace-if-ingest-key-enabled",
  site: "demo-shop.io",
  maskAllInputs: true,
});

window.addEventListener("beforeunload", () => {
  void client.flush();
});
```

## Flow

1. Capture DOM events with rrweb.
2. Upload session event payloads to `POST /v1/artifacts/session-events`.
3. Submit metadata + markers to `POST /v1/ingest/session`.
4. Auto-flush incremental updates periodically and on `pagehide`.

Use an `apiKey` value from your deployment environment (`INGEST_API_KEY`) for authenticated writes.

## Privacy Defaults

- Input values are masked at capture time (`maskAllInputs: true` by default).
- Additional redaction happens server-side before rrweb payloads are stored.
