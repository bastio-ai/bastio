# Bastio TypeScript SDKs

This workspace contains the first-party TypeScript SDKs for
[Bastio](https://bastio.ai) — the open-source AI security gateway.

| Package | What it does | npm |
|---|---|---|
| [`@bastio/core`](./packages/core) | Thin HTTP client for `POST /v1/detect`, shared types, `BastioBlockedError`. | `@bastio/core` |
| [`@bastio/mastra`](./packages/mastra) | Input/output processors that plug Bastio into a [Mastra](https://mastra.ai) agent. | `@bastio/mastra` |
| [`@bastio/vercel-ai`](./packages/vercel-ai) | Middleware for the [Vercel AI SDK](https://sdk.vercel.ai) — protects any model wrapped with `wrapLanguageModel`. | `@bastio/vercel-ai` |

All three are Apache-friendly runtime-only wrappers; they do not depend
on the Bastio gateway code itself. You run a Bastio gateway somewhere
(OSS container, cloud tenant, or self-hosted), point the SDK at its
`baseURL`, and every agent call flows through its security profile.

## Quickstart

```ts
// Mastra
import { Agent } from "@mastra/core";
import { BastioInputProcessor, BastioOutputProcessor } from "@bastio/mastra";

const agent = new Agent({
  id: "support",
  inputProcessors: [
    new BastioInputProcessor({ baseURL: process.env.BASTIO_URL!, apiKey: process.env.BASTIO_KEY }),
  ],
  outputProcessors: [
    new BastioOutputProcessor({ baseURL: process.env.BASTIO_URL!, apiKey: process.env.BASTIO_KEY }),
  ],
});
```

```ts
// Vercel AI SDK
import { wrapLanguageModel } from "ai";
import { openai } from "@ai-sdk/openai";
import { bastioMiddleware } from "@bastio/vercel-ai";

const guardedModel = wrapLanguageModel({
  model: openai("gpt-4o"),
  middleware: bastioMiddleware({
    baseURL: process.env.BASTIO_URL!,
    apiKey: process.env.BASTIO_KEY,
  }),
});
```

When a step in the Bastio profile chooses `block`, the SDK throws
`BastioBlockedError` with the full `DetectResponse` attached. When a step
chooses `mask` / `tokenize`, the SDK rewrites the message content before
the model sees it. `warn` and `log_only` are transparent.

## Development

```bash
pnpm install        # install workspace deps
pnpm typecheck      # tsc --noEmit in every package
pnpm test           # vitest run in every package
pnpm build          # tsup ESM + d.ts
```

### Repo layout

```
sdk/js/
├── pnpm-workspace.yaml
├── package.json
├── tsconfig.base.json
└── packages/
    ├── core/        # @bastio/core — HTTP client + types
    ├── mastra/      # @bastio/mastra — Mastra processors
    └── vercel-ai/   # @bastio/vercel-ai — Vercel AI middleware
```

### Releasing

We use [Changesets](https://github.com/changesets/changesets) for
versioning and publishing. See [`NOTES.md`](./NOTES.md) for outstanding
decisions (license, first-release version, npm scope bootstrap) that
need to be resolved before the first publish.

Typical flow:

```bash
pnpm changeset              # describe the change, pick bump type
git add .changeset/ && git commit -m "chore: changeset for X"
```

When the PR lands on `main`, the `sdk-js` GitHub Actions workflow opens
a "Version Packages" PR. Merging that PR publishes the bumped packages
to npm and pushes git tags.

## Documentation

Each package has a dedicated README with its own options reference.
Start there, then use `NOTES.md` to track publishing follow-ups.
