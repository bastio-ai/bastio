# @bastio/vercel-ai

[Bastio](https://bastio.ai) security middleware for the
[Vercel AI SDK](https://sdk.vercel.ai). Wraps any language model —
OpenAI, Anthropic, Gemini, Bedrock, or a custom provider — so every
call is screened by Bastio's detectors before it reaches the provider
and again before the response reaches your app.

## Install

```bash
npm install @bastio/vercel-ai ai
# or
pnpm add @bastio/vercel-ai ai
```

`ai` is a peer dependency (`>=4.0.0`).

## Use

```ts
import { wrapLanguageModel } from "ai";
import { openai } from "@ai-sdk/openai";
import { bastioMiddleware } from "@bastio/vercel-ai";

const guardedModel = wrapLanguageModel({
  model: openai("gpt-4o"),
  middleware: bastioMiddleware({
    baseURL: process.env.BASTIO_URL!,
    apiKey: process.env.BASTIO_KEY,
    profile: "default",
  }),
});

// Use guardedModel with generateText, streamText, generateObject, etc.
```

`block` in the configured profile throws `BastioBlockedError` — caught
by your app's error handler. `mask` / `tokenize` rewrite the prompt
before it's sent to the model, and rewrite the response before it's
returned to the caller.

## Options

| Option | Type | Default | Description |
|---|---|---|---|
| `baseURL` | `string` | — | Required. Bastio gateway URL. |
| `apiKey` | `string` | — | Bearer token for Bastio. |
| `profile` | `string` | `"default"` | Named Bastio security profile. |
| `steps` | `DetectStep[]` | — | Inline step list; overrides the profile. |
| `onDecision` | `(stage, result) => void` | — | Fires for both `"input"` and `"output"` decisions. Stage tells you which. |
| `scanOutput` | `boolean` | `true` | Set to `false` to skip response scanning. |
| `fetch`, `timeoutMs`, `headers` | — | see core | Transport knobs. |

## Streaming

`wrapStream` is currently a pass-through. Bastio's detectors reason on
complete text; scanning partial-token windows produces false positives
on common benign phrasings. Full streaming support is on the roadmap
for v0.2 and will use end-of-stream buffering with windowed early
detection for the worst categories (injection, jailbreak).

Meanwhile: if you need protection on streamed output, use
`generateText` instead of `streamText` on the guarded model, or scan
the full assembled response in a custom `onFinish` callback.

## License

[MIT](../../LICENSE). The Bastio server is FSL-1.1-ALv2, but this client SDK is permissively licensed so any application can embed it.
