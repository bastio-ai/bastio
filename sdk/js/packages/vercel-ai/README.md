# @bastio/vercel-ai

[Bastio](https://bastio.ai) security provider and middleware for the
[Vercel AI SDK](https://sdk.vercel.ai). Wraps any language model —
OpenAI, Anthropic, Gemini, Bedrock, Mistral, or custom providers — so every
call is screened by Bastio's detectors before it reaches the provider
and again before the response reaches your app.

- **Prompt Injection & Jailbreak Defense**: Detects adversarial jailbreaks, system prompt extractions, and multi-turn attacks.
- **PII & Secret Masking**: Automatically tokenizes or redacts SSNs, credit cards, emails, API keys, and JWTs in real time.
- **Bi-directional Output Scanning**: Validates model completions before returning to users to prevent data leakage and harmful completions.
- **Zero Configuration**: Reads `BASTIO_API_KEY` and `BASTIO_URL` from the environment automatically.

## Install

```bash
npm install @bastio/vercel-ai ai
# or
pnpm add @bastio/vercel-ai ai
```

`ai` is a peer dependency (`>=4.0.0`).

## Usage

### Method 1: Provider Wrapper (Recommended)

Wrap any model with `createBastio` / `createBastioProvider`:

```ts
import { createBastio } from "@bastio/vercel-ai";
import { openai } from "@ai-sdk/openai";
import { generateText } from "ai";

const bastio = createBastio({
  apiKey: process.env.BASTIO_API_KEY,
  profile: "production-guard",
});

const { text } = await generateText({
  model: bastio(openai("gpt-4o")),
  prompt: "Summarize customer feedback: Contact dsjacobsen@example.com for refunds.",
});

console.log(text);
```

### Method 2: Middleware with `wrapLanguageModel`

You can also use Bastio as standard Vercel AI SDK middleware:

```ts
import { wrapLanguageModel } from "ai";
import { openai } from "@ai-sdk/openai";
import { bastioMiddleware } from "@bastio/vercel-ai";

const guardedModel = wrapLanguageModel({
  model: openai("gpt-4o"),
  middleware: bastioMiddleware({
    baseURL: process.env.BASTIO_URL,
    apiKey: process.env.BASTIO_API_KEY,
    profile: "default",
  }),
});
```

### Handling Security Blocks

When Bastio detects a threat (injection, jailbreak, or policy violation), it throws a `BastioBlockedError`:

```ts
import { generateText } from "ai";
import { createBastio, BastioBlockedError } from "@bastio/vercel-ai";
import { openai } from "@ai-sdk/openai";

const bastio = createBastio();

try {
  const result = await generateText({
    model: bastio(openai("gpt-4o")),
    prompt: req.body.prompt,
  });
} catch (error) {
  if (error instanceof BastioBlockedError) {
    console.error("Blocked by Bastio:", error.message);
    console.error("Action:", error.response.action);
    console.error("Findings:", error.response.messages[0]?.steps);
    // Respond with 403 Forbidden
  }
}
```

## Options

| Option | Type | Default | Description |
|---|---|---|---|
| `baseURL` | `string` | `process.env.BASTIO_URL` \|\| `"http://localhost:4000"` | Bastio gateway URL. |
| `apiKey` | `string` | `process.env.BASTIO_API_KEY` | Bearer token for Bastio. |
| `profile` | `string` | `"default"` | Named Bastio security profile. |
| `securityProfile` | `string` | — | Alias for `profile`. |
| `steps` | `DetectStep[]` | — | Inline step list; overrides the profile. |
| `onDecision` | `(stage, result) => void` | — | Fires for both `"input"` and `"output"` decisions. |
| `scanOutput` | `boolean` | `true` | Set to `false` to skip response scanning. |
| `timeoutMs` | `number` | `10000` | Transport timeout in milliseconds. |
| `fetch` | `typeof fetch` | `globalThis.fetch` | Custom fetch implementation. |
| `headers` | `Record<string, string>` | — | Custom HTTP headers. |

## License

[MIT](../../LICENSE).
