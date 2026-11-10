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

// ── Discovery request parsing + snippet model state (pure, unit-tested) ─────

export type ParsedModelListing =
  | { ok: true; listing: GatewayModelListing }
  | { ok: false; status: number; message: string };

function errorText(body: unknown): string {
  if (!body || typeof body !== "object" || Array.isArray(body)) return "";
  const data = body as Record<string, unknown>;
  const nested = data.error && typeof data.error === "object" ? (data.error as Record<string, unknown>) : null;
  for (const value of [nested?.message, data.error, data.message, data.reason])
    if (typeof value === "string" && value.trim()) return value.trim();
  return "";
}

/**
 * Parse a `/v1/proxies/{id}/models` response. Tolerates non-JSON bodies
 * (proxy error pages), error envelopes sent with 200, and a `data` array in
 * place of `models`; never throws.
 */
export function parseGatewayModelListing(
  status: number,
  text: string,
  expectedProvider: string,
): ParsedModelListing {
  let body: unknown = null;
  try {
    body = text ? JSON.parse(text) : null;
  } catch {
    body = null;
  }
  const message = errorText(body);
  if (status < 200 || status >= 300)
    return {
      ok: false,
      status,
      message: message || (body === null && text.trim().startsWith("<") ? `The server returned an HTML error page (HTTP ${status}).` : `HTTP ${status}`),
    };
  if (!body || typeof body !== "object" || Array.isArray(body))
    return { ok: false, status, message: "The server returned an unreadable model list." };
  const data = body as Record<string, unknown>;
  const rawModels = Array.isArray(data.models) ? data.models : Array.isArray(data.data) ? data.data : null;
  if (!rawModels) return { ok: false, status, message: message || "The server returned no model list." };
  if (data.provider !== undefined && data.provider !== expectedProvider)
    return { ok: false, status, message: "The server returned models for a different provider." };
  const source = data.source === "catalog" ? "catalog" : "provider";
  const models = rawModels.flatMap((item) => {
    if (typeof item === "string") return item.trim() ? [{ id: item.trim() }] : [];
    if (item && typeof item === "object" && typeof (item as { id?: unknown }).id === "string") {
      const { id, name } = item as { id: string; name?: unknown };
      return id.trim() ? [{ id: id.trim(), ...(typeof name === "string" && name ? { name } : {}) }] : [];
    }
    return [];
  });
  return { ok: true, listing: { provider: expectedProvider, source, models } };
}

type ProxyIdentity = { id: string; target_provider: string; is_active: boolean };

/** Query key: per gateway and provider, so a provider change never reuses a list. */
export function gatewayModelQueryKey(proxy: Pick<ProxyIdentity, "id" | "target_provider">) {
  return ["gateway-models", proxy.id, proxy.target_provider] as const;
}

/** Discovery needs a saved, active gateway. TypeSafe has a fixed model. */
export function gatewayModelQueryEnabled(proxy: ProxyIdentity) {
  return proxy.is_active && proxy.id !== "" && proxy.target_provider !== "typesafe";
}

export type SnippetModelInput = {
  configured: string;
  provider: string;
  listing?: GatewayModelListing | null;
  discovering: boolean;
  error?: string | null;
  /** Model chosen in the picker, if any. */
  picked?: string | null;
  /** Free-text model typed when discovery has nothing to offer. */
  typed?: string;
};

export type SnippetModelState = {
  model: string;
  options: string[];
  /** "manual": no discovered model — show the warning and a free-text input. */
  mode: "loading" | "picker" | "configured" | "manual";
  reason: "error" | "catalog" | "empty" | null;
};

export function snippetModelState(input: SnippetModelInput): SnippetModelState {
  const configured = input.configured.trim();
  const discovered = snippetModelIds(input.listing);
  const options = configured && !discovered.includes(configured) ? [configured, ...discovered] : discovered;
  const picked = input.picked && options.includes(input.picked) ? input.picked : "";
  const resolved = picked || resolveSnippetModel(input.provider, configured, input.listing);
  if (options.length > 1 || (options.length === 1 && !configured))
    return { model: resolved, options, mode: "picker", reason: null };
  if (configured) return { model: configured, options, mode: "configured", reason: null };
  if (input.discovering) return { model: "", options: [], mode: "loading", reason: null };
  const reason = input.error ? "error" : input.listing?.source === "catalog" ? "catalog" : "empty";
  return { model: (input.typed ?? "").trim(), options: [], mode: "manual", reason };
}
