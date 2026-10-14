export type ModelProvider =
  | "openai"
  | "anthropic"
  | "gemini"
  | "deepseek"
  | "xai"
  | "openrouter"
  | "groq"
  | "bedrock"
  | "ollama";

// ModelChoice carries the provider + model pair sent to the server,
// plus concise employee-facing guidance. The gateway still owns the
// real provider contract; this catalogue only curates models that make
// sense in a general-purpose enterprise workspace.
export type ModelChoice = {
  provider: ModelProvider;
  model: string;
  label: string;
  description: string;
  badge?: string;
  // Old models remain resolvable for tenant whitelists and existing
  // assistants, but do not crowd the default employee picker.
  legacy?: boolean;
};

// Curated chat catalogue verified against provider docs and OpenRouter models
// on 2026-10-11. Exact sources and availability notes: docs/workspace-models.md.
// Keep this focused: image, audio, moderation, and embedding-only models
// do not belong in a text-first employee chat picker.
export const MODEL_CATALOG: ModelChoice[] = [
  {
    "provider": "openai",
    "model": "gpt-6.1-sol",
    "label": "GPT-6.1 Sol",
    "description": "Balanced capability and cost for daily professional work",
    "badge": "Recommended"
  },
  {
    "provider": "openai",
    "model": "gpt-6-astra",
    "label": "GPT-6 Astra",
    "description": "Flagship reasoning and coding for demanding work",
    "badge": "Advanced"
  },
  {
    "provider": "openai",
    "model": "gpt-6-luna",
    "label": "GPT-6 Luna",
    "description": "Efficient model for focused, high-volume tasks",
    "badge": "Fast"
  },
  {
    "provider": "anthropic",
    "model": "claude-sonnet-5-5",
    "label": "Claude Sonnet 5.5",
    "description": "Balanced speed and intelligence for professional work",
    "badge": "Popular"
  },
  {
    "provider": "anthropic",
    "model": "claude-opus-5-5",
    "label": "Claude Opus 5.5",
    "description": "Long-running coding and knowledge work",
    "badge": "Advanced"
  },
  {
    "provider": "anthropic",
    "model": "claude-fable-5-1",
    "label": "Claude Fable 5.1",
    "description": "Demanding reasoning and long-horizon agent work",
    "badge": "Flagship"
  },
  {
    "provider": "anthropic",
    "model": "claude-haiku-5-5",
    "label": "Claude Haiku 5.5",
    "description": "Low-latency classification, extraction and everyday tasks",
    "badge": "Fast"
  },
  {
    "provider": "gemini",
    "model": "gemini-3.8-flash",
    "label": "Gemini 3.8 Flash",
    "description": "Current stable multimodal model for agentic and everyday work",
    "badge": "New"
  },
  {
    "provider": "gemini",
    "model": "gemini-3.6-flash",
    "label": "Gemini 3.6 Flash",
    "description": "Stable previous-generation multimodal model"
  },
  {
    "provider": "gemini",
    "model": "gemini-3.5-flash-lite",
    "label": "Gemini 3.5 Flash-Lite",
    "description": "Efficient model for frequent, lightweight work",
    "badge": "Efficient"
  },
  {
    "provider": "deepseek",
    "model": "deepseek-flash",
    "label": "DeepSeek V4.1 Flash",
    "description": "Current Flash model with vision and optional thinking",
    "badge": "Fast"
  },
  {
    "provider": "deepseek",
    "model": "deepseek-v4-pro",
    "label": "DeepSeek V4 Pro",
    "description": "Reasoning model for complex work",
    "badge": "Reasoning"
  },
  {
    "provider": "xai",
    "model": "grok-4.7",
    "label": "Grok 4.7",
    "description": "xAI flagship with configurable reasoning",
    "badge": "New"
  },
  {
    "provider": "groq",
    "model": "openai/gpt-oss-120b",
    "label": "GPT OSS 120B",
    "description": "Large open-weight model served on Groq"
  },
  {
    "provider": "groq",
    "model": "openai/gpt-oss-20b",
    "label": "GPT OSS 20B",
    "description": "Fast, compact open-weight model served on Groq",
    "badge": "Fast"
  },
  {
    "provider": "groq",
    "model": "llama-3.3-70b-versatile",
    "label": "Llama 3.3 70B",
    "description": "General-purpose model; requires Groq enterprise access",
    "badge": "Enterprise"
  },
  {
    "provider": "groq",
    "model": "llama-3.1-8b-instant",
    "label": "Llama 3.1 8B Instant",
    "description": "Compact model; requires Groq enterprise access",
    "badge": "Enterprise"
  },
  {
    "provider": "openrouter",
    "model": "openai/gpt-6.1-sol",
    "label": "GPT-6.1 Sol",
    "description": "Balanced OpenAI model via OpenRouter"
  },
  {
    "provider": "openrouter",
    "model": "openai/gpt-6-astra",
    "label": "GPT-6 Astra",
    "description": "Flagship OpenAI reasoning via OpenRouter"
  },
  {
    "provider": "openrouter",
    "model": "openai/gpt-6-luna",
    "label": "GPT-6 Luna",
    "description": "Efficient OpenAI model via OpenRouter"
  },
  {
    "provider": "openrouter",
    "model": "anthropic/claude-sonnet-5.5",
    "label": "Claude Sonnet 5.5",
    "description": "Balanced Claude model via OpenRouter"
  },
  {
    "provider": "openrouter",
    "model": "anthropic/claude-opus-5.5",
    "label": "Claude Opus 5.5",
    "description": "Advanced Claude model via OpenRouter"
  },
  {
    "provider": "openrouter",
    "model": "anthropic/claude-fable-5.1",
    "label": "Claude Fable 5.1",
    "description": "Long-horizon Claude reasoning via OpenRouter"
  },
  {
    "provider": "openrouter",
    "model": "anthropic/claude-haiku-5.5",
    "label": "Claude Haiku 5.5",
    "description": "Low-latency Claude model via OpenRouter"
  },
  {
    "provider": "openrouter",
    "model": "google/gemini-3.8-flash",
    "label": "Gemini 3.8 Flash",
    "description": "Current Google Flash model via OpenRouter"
  },
  {
    "provider": "openrouter",
    "model": "deepseek/deepseek-v4.1-flash",
    "label": "DeepSeek V4.1 Flash",
    "description": "Current DeepSeek Flash model via OpenRouter"
  },
  {
    "provider": "openrouter",
    "model": "deepseek/deepseek-v4-pro-0813",
    "label": "DeepSeek V4 Pro",
    "description": "DeepSeek reasoning model via OpenRouter"
  },
  {
    "provider": "openrouter",
    "model": "x-ai/grok-4.7",
    "label": "Grok 4.7",
    "description": "Current xAI flagship via OpenRouter"
  },
  {
    "provider": "openrouter",
    "model": "mistralai/mistral-large-4-0",
    "label": "Mistral Large 4",
    "description": "Current Mistral flagship via OpenRouter"
  },
  {
    "provider": "openrouter",
    "model": "qwen/qwen3.8-max-prime",
    "label": "Qwen3.8 Max Prime",
    "description": "Advanced Qwen multimodal model via OpenRouter"
  },
  {
    "provider": "openrouter",
    "model": "moonshotai/kimi-k3",
    "label": "Kimi K3",
    "description": "Moonshot multimodal model via OpenRouter"
  },
  {
    "provider": "openrouter",
    "model": "z-ai/glm-5.3-prime",
    "label": "GLM 5.3 Prime",
    "description": "Advanced Z.ai text reasoning via OpenRouter"
  },
  {
    "provider": "bedrock",
    "model": "anthropic.claude-sonnet-5-5",
    "label": "Claude Sonnet 5.5 (Bedrock)",
    "description": "Requires model access in your AWS region"
  },
  {
    "provider": "bedrock",
    "model": "anthropic.claude-opus-5-5",
    "label": "Claude Opus 5.5 (Bedrock)",
    "description": "Requires model access in your AWS region"
  },
  {
    "provider": "ollama",
    "model": "gemma4",
    "label": "Gemma 4 (Ollama)",
    "description": "Requires this model installed on your Ollama server"
  },
  {
    "provider": "ollama",
    "model": "qwen3.5",
    "label": "Qwen3.5 (Ollama)",
    "description": "Requires this model installed on your Ollama server"
  },
  {
    "provider": "ollama",
    "model": "gpt-oss:20b",
    "label": "GPT OSS 20B (Ollama)",
    "description": "Requires this model installed on your Ollama server"
  },
  {
    "provider": "openai",
    "model": "gpt-5.6-terra",
    "label": "GPT-5.6 Terra",
    "description": "Balanced intelligence, speed, and cost for daily work",
    "legacy": true
  },
  {
    "provider": "openai",
    "model": "gpt-5.6-sol",
    "label": "GPT-5.6 Sol",
    "description": "Highest capability for complex analysis and coding",
    "legacy": true
  },
  {
    "provider": "openai",
    "model": "gpt-5.6-luna",
    "label": "GPT-5.6 Luna",
    "description": "Fast, cost-efficient model for high-volume tasks",
    "legacy": true
  },
  {
    "provider": "anthropic",
    "model": "claude-sonnet-5",
    "label": "Claude Sonnet 5",
    "description": "Fast, capable model for everyday professional work",
    "legacy": true
  },
  {
    "provider": "anthropic",
    "model": "claude-fable-5",
    "label": "Claude Fable 5",
    "description": "Long-running agents and complex enterprise workflows",
    "legacy": true
  },
  {
    "provider": "anthropic",
    "model": "claude-opus-5",
    "label": "Claude Opus 5",
    "description": "Deep reasoning for demanding professional tasks",
    "legacy": true
  },
  {
    "provider": "anthropic",
    "model": "claude-haiku-4-5",
    "label": "Claude Haiku 4.5",
    "description": "Anthropic's fastest model for lightweight tasks",
    "legacy": true
  },
  {
    "provider": "gemini",
    "model": "gemini-3.7-flash",
    "label": "Gemini 3.7 Flash",
    "description": "Google's latest fast multimodal model",
    "legacy": true
  },
  {
    "provider": "deepseek",
    "model": "deepseek-v4-flash",
    "label": "DeepSeek V4 Flash",
    "description": "High-speed model with optional thinking",
    "legacy": true
  },
  {
    "provider": "openai",
    "model": "gpt-5.4-mini",
    "label": "GPT-5.4 Mini",
    "description": "Previous-generation compact model",
    "legacy": true
  },
  {
    "provider": "openai",
    "model": "gpt-5.4",
    "label": "GPT-5.4",
    "description": "Previous-generation flagship model",
    "legacy": true
  },
  {
    "provider": "openai",
    "model": "gpt-4o-mini",
    "label": "GPT-4o Mini",
    "description": "Legacy compact multimodal model",
    "legacy": true
  },
  {
    "provider": "openai",
    "model": "gpt-4o",
    "label": "GPT-4o",
    "description": "Legacy multimodal model",
    "legacy": true
  },
  {
    "provider": "anthropic",
    "model": "claude-sonnet-4-6",
    "label": "Claude Sonnet 4.6",
    "description": "Previous-generation balanced Claude model",
    "legacy": true
  },
  {
    "provider": "anthropic",
    "model": "claude-opus-4-7",
    "label": "Claude Opus 4.7",
    "description": "Previous-generation advanced Claude model",
    "legacy": true
  }
];

// Default-open workspaces see only the current catalogue. Legacy models
// remain available when an administrator explicitly whitelists them.
export const DEFAULT_MODELS: ModelChoice[] = MODEL_CATALOG.filter((model) => !model.legacy);
export const DEFAULT_MODEL: ModelChoice = DEFAULT_MODELS[0]!;

export type AllowedModelEntry = { provider: string; model: string };

export function pickModelChoices(allowed: AllowedModelEntry[]): ModelChoice[] {
  if (!allowed || allowed.length === 0) return DEFAULT_MODELS;
  const known = new Map(MODEL_CATALOG.map((entry) => [`${entry.provider}/${entry.model}`, entry]));
  const seen = new Set<string>();
  return allowed.flatMap((entry) => {
    const key = `${entry.provider}/${entry.model}`;
    if (!MODEL_PROVIDERS.includes(entry.provider as ModelProvider) || !entry.model || seen.has(key)) return [];
    seen.add(key);
    return [known.get(key) ?? {
      provider: entry.provider as ModelProvider,
      model: entry.model,
      label: entry.model,
      description: "Workspace-approved model",
    }];
  });
}

export const MODEL_PROVIDERS: ModelProvider[] = [
  "openai",
  "anthropic",
  "gemini",
  "deepseek",
  "xai",
  "openrouter",
  "groq",
  "bedrock",
  "ollama",
];

