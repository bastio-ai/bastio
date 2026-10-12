---
title: Workspace model catalogue
description: Current provider models, identifiers, access requirements and compatibility notes.
---

The employee picker and assistant editor share a curated text-chat catalogue,
verified on **11 October 2026**. New assistants and chats default to GPT-6.1 Sol
on OpenAI; choosing another provider without a model uses that provider's current
Workspace default. Existing assistants keep their explicit provider/model pair.

| Provider | Current choices |
| --- | --- |
| OpenAI | `gpt-6.1-sol`, `gpt-6-astra`, `gpt-6-luna` |
| Claude | `claude-sonnet-5-5`, `claude-opus-5-5`, `claude-fable-5-1`, `claude-haiku-5-5` |
| Gemini | `gemini-3.8-flash`, `gemini-3.6-flash`, `gemini-3.5-flash-lite` |
| DeepSeek | `deepseek-flash` (V4.1 Flash), `deepseek-v4-pro` |
| Grok / xAI | `grok-4.7` |
| Groq | `openai/gpt-oss-120b`, `openai/gpt-oss-20b`; Llama 3.3 70B and Llama 3.1 8B require enterprise access |
| OpenRouter | Current OpenAI, Claude, Gemini, DeepSeek and Grok choices, plus Mistral Large 4, Qwen3.8 Max Prime, Kimi K3 and GLM 5.3 Prime |
| Bedrock | Claude Sonnet 5.5 and Opus 5.5; requires access in the configured AWS region |
| Ollama | `gemma4`, `qwen3.5`, `gpt-oss:20b`; requires installation on the configured Ollama server |

Store the provider credential in Bastio's Provider Keys settings. OpenRouter uses
one OpenRouter key and its own exact model slugs, such as
`anthropic/claude-sonnet-5.5`, rather than Claude's direct API ID. xAI and Groq are
separate providers and require separate credentials. Self-hosted deployments can
also use `OPENROUTER_API_KEY` or `XAI_API_KEY` (`XAI_BASE_URL` defaults to
`https://api.x.ai/v1`). Never put a provider key in a model ID or system prompt.

OpenRouter, xAI, Groq and DeepSeek emit incremental Workspace chat tokens and
provider usage through the OpenAI-compatible stream decoder. Existing gateway
and Workspace security controls still apply. OpenAI GPT-6 chat uses Chat
Completions without tool calling; GPT-6 tool calling requires the Responses API.

Legacy models remain resolvable for existing assistants and explicit workspace
allowlists, but are hidden from the default-open employee picker. An explicitly
approved model on a supported provider remains selectable even when it is not
in the curated catalogue. Provider availability, account entitlements and model
retirement still apply; these selections do not provision upstream access.

Budget/cost estimates use published standard uncached text rates. They are
estimates, not provider invoices: cache discounts, long-context tiers, regional
pricing, reasoning/tool charges and OpenRouter routing may change the actual
charge. DeepSeek estimates use peak rates. The Gemini 3.8 Flash introductory
rates need review before 1 January 2027.

Sources checked:

- [OpenAI models](https://developers.openai.com/api/docs/models) and [GPT-6.1 Sol](https://developers.openai.com/api/docs/models/gpt-6.1-sol)
- [Claude model IDs and Bedrock IDs](https://platform.claude.com/docs/en/models/overview)
- [Gemini model IDs](https://ai.google.dev/gemini-api/docs/models)
- [DeepSeek current model aliases](https://api-docs.deepseek.com/quick_start/pricing)
- [xAI models](https://docs.x.ai/developers/models) and [Chat Completions](https://docs.x.ai/developers/model-capabilities/legacy/chat-completions)
- [Groq production and enterprise models](https://console.groq.com/docs/models)
- [OpenRouter public model feed](https://openrouter.ai/api/v1/models) (458 models at verification; curated chat subset, excluding batch/image-only/audio-only entries)
- [Ollama library](https://ollama.com/library)
- [OpenAI pricing](https://developers.openai.com/api/docs/pricing), [Claude pricing](https://platform.claude.com/docs/en/about-claude/pricing), [Gemini pricing](https://ai.google.dev/gemini-api/docs/pricing)
