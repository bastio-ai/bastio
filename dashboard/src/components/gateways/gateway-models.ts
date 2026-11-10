import { MODEL_CATALOG, type ModelChoice } from "../workspace/model-catalog.ts";

// Suggestions reuse the workspace catalogue, filtered to this gateway's provider.
// Availability and account permissions are still checked by the provider.
export function gatewayModelChoices(
  provider: string,
  configuredModel = "",
): ModelChoice[] {
  const choices = MODEL_CATALOG.filter(
    (choice) => choice.provider === provider && !choice.legacy,
  );
  const configured = configuredModel.trim();
  if (configured && !choices.some((choice) => choice.model === configured)) {
    const known = MODEL_CATALOG.find(
      (choice) => choice.provider === provider && choice.model === configured,
    );
    choices.unshift(
      known ?? {
        provider: provider as ModelChoice["provider"],
        model: configured,
        label: configured,
        description: "Your gateway's configured default model",
      },
    );
  }
  return choices;
}

export function defaultGatewayTestModel(
  provider: string,
  configuredModel = "",
) {
  const configured = configuredModel.trim();
  if (configured) return configured;
  const choices = gatewayModelChoices(provider);
  return (
    (
      choices.find((choice) =>
        /haiku|luna|flash|instant|20b/i.test(choice.label),
      ) ?? choices[0]
    )?.model ?? ""
  );
}

export function matchesGatewayModel(choice: ModelChoice, query: string) {
  return `${choice.label} ${choice.model} ${choice.description}`
    .toLowerCase()
    .includes(query.trim().toLowerCase());
}

export type GatewayModelListing = {
  provider: string;
  source: "provider" | "catalog";
  models: { id: string; name?: string }[];
};

// Include older IDs when reported by this account. A public catalogue must not
// invent provider account access.
export function listedGatewayModelChoices(
  listing: GatewayModelListing,
): ModelChoice[] {
  if (listing.source === "catalog")
    return gatewayModelChoices(listing.provider);
  return listing.models.map(({ id, name }) => {
    const known = MODEL_CATALOG.find(
      (item) => item.provider === listing.provider && item.model === id,
    );
    return (
      known ?? {
        provider: listing.provider as ModelChoice["provider"],
        model: id,
        label:
          name ||
          (listing.provider === "openai" ? friendlyOpenAIModel(id) : id),
        description: "Reported by your provider key",
      }
    );
  });
}

export function defaultListedGatewayModel(
  choices: ModelChoice[],
  configured = "",
): string {
  if (choices.some((choice) => choice.model === configured.trim()))
    return configured.trim();
  // Prefer inexpensive non-reasoning aliases for a bounded hello, independent
  // of provider list order. Never select an ID this account did not report.
  for (const id of [
    "gpt-4.1-nano",
    "gpt-4o-mini",
    "gpt-4.1-mini",
    "gpt-5-nano",
    "gpt-5-mini",
  ]) {
    const choice = choices.find(
      (item) => item.provider === "openai" && item.model === id,
    );
    if (choice) return choice.model;
  }
  return (
    (
      choices.find((choice) =>
        /mini|nano|haiku|flash|instant|20b/i.test(choice.model),
      ) ?? choices[0]
    )?.model ?? ""
  );
}

function friendlyOpenAIModel(id: string): string {
  return id
    .replace(/^gpt-/, "GPT-")
    .replace(
      /-(mini|nano)(?=-|$)/,
      (_, size: string) => ` ${size.charAt(0).toUpperCase()}${size.slice(1)}`,
    )
    .replace(/-(\d{4}-\d{2}-\d{2})$/, " · $1");
}

// ── Snippet model resolution ────────────────────────────────────────────────
// Connection examples need a real model ID or a copy-pasted request fails.
// Order: the gateway's configured default, then a small general chat model
// from the IDs the provider key actually reported, then MODEL_PLACEHOLDER.
// Never return an ID that is not in the reported list.

const NON_CHAT_MODEL =
  /embed|whisper|tts|audio|transcri|speech|dall-e|image|imagen|moderation|realtime|rerank|guard|davinci|babbage|turbo-instruct|search|computer-use|codex|deep-research|sora|veo|lyria|aqa/i;

/** Heuristic: true unless the ID is obviously not a chat-completions model. */
export function isChatModelId(id: string): boolean {
  return id.trim() !== "" && !NON_CHAT_MODEL.test(id);
}

/**
 * Per-provider preference lists, cheapest general chat models first. Matched
 * by exact ID (Gemini also as `models/<id>`) against the reported list.
 */
export const SNIPPET_MODEL_PREFERENCES: Record<string, readonly string[]> = {
  openai: ["gpt-4.1-nano", "gpt-4o-mini", "gpt-4.1-mini", "gpt-5-nano", "gpt-5-mini", "gpt-5.4-mini"],
  anthropic: ["claude-haiku-4-5", "claude-3-5-haiku-latest", "claude-3-5-haiku-20241022", "claude-3-haiku-20240307"],
  openrouter: ["openai/gpt-4.1-nano", "openai/gpt-4o-mini", "google/gemini-2.0-flash-001", "anthropic/claude-3.5-haiku", "meta-llama/llama-3.1-8b-instruct"],
  gemini: ["gemini-2.0-flash-lite", "gemini-2.5-flash-lite", "gemini-2.0-flash", "gemini-2.5-flash", "gemini-1.5-flash"],
  groq: ["llama-3.1-8b-instant", "gemma2-9b-it", "llama3-8b-8192", "llama-3.3-70b-versatile"],
  deepseek: ["deepseek-chat"],
  xai: ["grok-3-mini", "grok-3-mini-fast", "grok-4-fast-non-reasoning", "grok-2-latest"],
  ollama: ["llama3.2", "llama3.2:latest", "llama3.1:8b", "qwen2.5:7b", "mistral"],
};

/** Chat-capable IDs reported by the provider key, in reported order. */
export function snippetModelIds(listing?: GatewayModelListing | null): string[] {
  if (!listing || listing.source !== "provider") return [];
  const seen = new Set<string>();
  const ids: string[] = [];
  for (const { id } of listing.models) {
    const trimmed = id.trim();
    if (isChatModelId(trimmed) && !seen.has(trimmed)) {
      seen.add(trimmed);
      ids.push(trimmed);
    }
  }
  return ids;
}

/** Deterministic pick from reported IDs; "" when none is chat-capable. */
export function pickSnippetModel(provider: string, ids: readonly string[]): string {
  const chat = ids.filter(isChatModelId);
  for (const preferred of SNIPPET_MODEL_PREFERENCES[provider] ?? []) {
    const match = chat.find((id) => id === preferred || id === `models/${preferred}`);
    if (match) return match;
  }
  return chat[0] ?? "";
}

/** Model for connection examples: configured → reported pick → "" (placeholder). */
export function resolveSnippetModel(
  provider: string,
  configured: string,
  listing?: GatewayModelListing | null,
): string {
  return configured.trim() || pickSnippetModel(provider, snippetModelIds(listing));
}
