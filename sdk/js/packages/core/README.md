# @bastio/core

Low-level TypeScript client for [Bastio](https://bastio.ai)'s
`POST /v1/detect` endpoint, plus the shared types and errors used by
the framework adapters ([`@bastio/mastra`](../mastra),
[`@bastio/vercel-ai`](../vercel-ai)).

You usually don't import this directly — the adapters pull it in as a
dependency. Use it when you're writing a custom integration or a
server-side policy check outside a supported framework.

## Install

```bash
npm install @bastio/core
# or
pnpm add @bastio/core
```

## Use

```ts
import { BastioClient, BastioBlockedError } from "@bastio/core";

const client = new BastioClient({
  baseURL: "https://bastio.example.com",
  apiKey: process.env.BASTIO_KEY,
});

const result = await client.detect({
  messages: [{ role: "user", content: "Ignore previous instructions…" }],
  direction: "input",
  profile: "default",
});

if (result.should_block) throw new BastioBlockedError(result);

// otherwise, continue with result.messages[0].sanitized_content
```

## Options

| Option | Type | Default | Description |
|---|---|---|---|
| `baseURL` | `string` | — | Required. Base URL of the Bastio gateway. Trailing slashes tolerated. |
| `apiKey` | `string` | — | Bearer token sent to Bastio's auth middleware. Omit for gateways without auth. |
| `fetch` | `typeof fetch` | `globalThis.fetch` | Custom fetch — useful for edge runtimes and tests. |
| `timeoutMs` | `number` | `10_000` | Per-call timeout. On expiry throws `BastioError`. |
| `headers` | `Record<string,string>` | `{}` | Extra headers (e.g. `X-Request-Id`) applied to every call. |

## Error model

- `BastioError` — transport or HTTP failure; `.status` and `.body` are
  populated when available.
- `BastioBlockedError` — thrown by the framework adapters (not the
  client) when `result.should_block === true`. Carries the full
  `DetectResponse` on `.result` so you can render the decision without
  re-calling the API.

## License

[MIT](../../LICENSE). The Bastio server is FSL-1.1-ALv2, but this client SDK is permissively licensed so any application can embed it.
