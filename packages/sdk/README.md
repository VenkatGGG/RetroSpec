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

Use a project-specific `apiKey` generated from `POST /v1/admin/projects` for multi-tenant isolation.
