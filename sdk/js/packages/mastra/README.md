# @bastio/mastra

[Bastio](https://bastio.ai) security processors for the
[Mastra](https://mastra.ai) AI agent framework. Drop-in input/output
processors that:

- **block** malicious requests before the LLM sees them (prompt
  injection, jailbreak attempts, policy violations)
- **mask / tokenize** PII on input and output
- **warn** on suspicious content without interrupting the agent
- **emit traces** to your Bastio dashboard so every agent call has a
  security audit trail

Bastio's detectors are deterministic-first and typically add <50ms p50
vs. LLM-graded guardrails that burn an extra model call per message.

## Install

```bash
npm install @bastio/mastra @mastra/core
# or
pnpm add @bastio/mastra @mastra/core
```

`@mastra/core` is a peer dependency.

## Use

```ts
import { Agent } from "@mastra/core";
import { BastioInputProcessor, BastioOutputProcessor } from "@bastio/mastra";

const bastio = {
  baseURL: process.env.BASTIO_URL!,
  apiKey: process.env.BASTIO_KEY,
  profile: "default", // optional — uses the customer default when omitted
};

export const supportAgent = new Agent({
  id: "support",
  name: "Support Agent",
  instructions: "Help the user with their order.",
  inputProcessors: [new BastioInputProcessor(bastio)],
  outputProcessors: [new BastioOutputProcessor(bastio)],
});
```

When a profile step chooses `block`, the processor throws
`BastioBlockedError` — Mastra surfaces this as a failed run, which is
exactly what "blocked" means to the caller. Mask/tokenize strategies
rewrite the message content in place before the model runs.

## Options

`BastioInputProcessor` and `BastioOutputProcessor` accept the same
options — a superset of [`@bastio/core`](../core)'s `BastioClientOptions`:

| Option | Type | Default | Description |
|---|---|---|---|
| `baseURL` | `string` | — | Required. Bastio gateway URL. |
| `apiKey` | `string` | — | Bearer token for Bastio. |
| `profile` | `string` | `"default"` | Named Bastio security profile. |
| `steps` | `DetectStep[]` | — | Inline step list; overrides the profile. Useful for per-agent specialization or tests. |
| `onDecision` | `(result) => void` | — | Fires after every call. Use for metrics/logging alongside Bastio's own tracing. |
| `fetch`, `timeoutMs`, `headers` | — | see core | Transport knobs forwarded to `BastioClient`. |

## How content is mapped

Mastra messages can be a string or an array of parts. The processor:

1. Extracts all `text`-typed parts (or the raw string).
2. Sends the concatenated text to Bastio for detection.
3. On rewrite, replaces the first text span with the sanitized content.
4. Non-text parts (tool calls, multi-modal attachments) pass through
   unchanged.

## Testing locally

The processors accept a custom `fetch` implementation, so you can stub
the Bastio API in unit tests:

```ts
new BastioInputProcessor({
  baseURL: "https://stub.test",
  fetch: async () => new Response(JSON.stringify(mockResponse)),
});
```

## License

[MIT](../../LICENSE). The Bastio server is FSL-1.1-ALv2, but this client SDK is permissively licensed so any application can embed it.
