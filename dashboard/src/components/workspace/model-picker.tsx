import * as DropdownMenu from "@radix-ui/react-dropdown-menu";
import { Check, ChevronDown } from "lucide-react";

// ModelChoice carries a provider + model pair the chat sends to the
// server. The OSS workspace backend honors body-level overrides (see
// streaming.go:resolveAssistantConfig) so picking a model in the UI
// is sufficient — no server-side wiring needed.
export type ModelChoice = {
  provider: "openai" | "anthropic" | "bedrock" | "ollama";
  model: string;
  label: string;
};

// Curated catalog of every model the picker can ever surface. The
// employee-facing list is filtered against this catalog by the
// admin's whitelist (workspace_settings.allowed_models) — empty
// whitelist = show all, non-empty = strict subset.
//
// Adding a model: append a new ModelChoice here. Removing one is
// safer per-tenant via the admin's whitelist than via this catalog.
export const MODEL_CATALOG: ModelChoice[] = [
  { provider: "openai", model: "gpt-5.4-mini", label: "GPT-5.4 Mini" },
  { provider: "openai", model: "gpt-5.4", label: "GPT-5.4" },
  { provider: "openai", model: "gpt-4o-mini", label: "GPT-4o Mini" },
  { provider: "openai", model: "gpt-4o", label: "GPT-4o" },
  { provider: "anthropic", model: "claude-haiku-4-5", label: "Claude Haiku 4.5" },
  { provider: "anthropic", model: "claude-sonnet-4-6", label: "Claude Sonnet 4.6" },
  { provider: "anthropic", model: "claude-opus-4-7", label: "Claude Opus 4.7" },
];

// DEFAULT_MODELS keeps the existing import name for back-compat —
// callers that don't have a tenant-specific allowed list (OSS-only
// preview, tests) get the full catalog. The chat surface should
// prefer pickModelChoices() to apply the per-tenant filter.
export const DEFAULT_MODELS: ModelChoice[] = MODEL_CATALOG;

// First entry of DEFAULT_MODELS, narrowed past the `T | undefined`
// strict-mode index access. Asserting with `!` is fine because the
// constant is non-empty by construction; if a future edit empties
// the list, TypeScript catches the runtime risk via this line.
export const DEFAULT_MODEL: ModelChoice = DEFAULT_MODELS[0]!;

// AllowedModelEntry mirrors workspace_settings.allowed_models JSONB
// items. Loose-typed (string provider) on purpose — the catalog might
// not yet know about a provider the admin enabled.
export type AllowedModelEntry = { provider: string; model: string };

// pickModelChoices filters the catalog through the admin's whitelist.
// Empty allowed → return the full catalog (default-open behavior).
// Non-empty → return only entries whose (provider, model) appear in
// the whitelist, preserving catalog order so the admin's UX is the
// same regardless of whitelist arrangement.
export function pickModelChoices(allowed: AllowedModelEntry[]): ModelChoice[] {
  if (!allowed || allowed.length === 0) return MODEL_CATALOG;
  const allowSet = new Set(allowed.map((a) => `${a.provider}/${a.model}`));
  return MODEL_CATALOG.filter((m) => allowSet.has(`${m.provider}/${m.model}`));
}

// ModelPicker is the small dropdown in the chat header that lets the
// user swap models mid-conversation. The selection applies to the
// next message; previously-sent messages keep their own model
// metadata (visible in the message footer).
//
// `available` is the per-tenant filtered catalog. The chat-tab
// computes it from workspace_settings.allowed_models via
// pickModelChoices() and passes it in. Defaults to the full catalog
// when the caller doesn't have settings yet (loading state).
export function ModelPicker({
  value,
  onChange,
  available = DEFAULT_MODELS,
}: {
  value: ModelChoice;
  onChange: (m: ModelChoice) => void;
  available?: ModelChoice[];
}) {
  return (
    <DropdownMenu.Root>
      <DropdownMenu.Trigger asChild>
        <button
          type="button"
          className="inline-flex items-center gap-1.5 rounded-md border border-border bg-background px-2.5 py-1 text-xs font-medium text-foreground transition hover:bg-muted"
        >
          <span
            className="h-1.5 w-1.5 rounded-full bg-cyan-500"
            aria-hidden="true"
          />
          {value.label}
          <ChevronDown className="h-3 w-3 text-muted-foreground" />
        </button>
      </DropdownMenu.Trigger>
      <DropdownMenu.Portal>
        <DropdownMenu.Content
          align="start"
          sideOffset={4}
          className="z-50 min-w-[220px] overflow-hidden rounded-md border border-border bg-popover p-1 text-sm shadow-md"
        >
          <DropdownMenu.Label className="px-2 py-1.5 text-[10px] uppercase tracking-wide text-muted-foreground">
            Model
          </DropdownMenu.Label>
          {available.map((m) => {
            const active =
              value.provider === m.provider && value.model === m.model;
            return (
              <DropdownMenu.Item
                key={`${m.provider}/${m.model}`}
                onSelect={() => onChange(m)}
                className="flex cursor-pointer items-center justify-between rounded px-2 py-1.5 text-xs outline-none data-[highlighted]:bg-muted"
              >
                <span className="flex items-center gap-2">
                  <span className="text-muted-foreground">{providerLabel(m.provider)}</span>
                  <span>{m.label}</span>
                </span>
                {active && <Check className="h-3 w-3 text-cyan-500" />}
              </DropdownMenu.Item>
            );
          })}
        </DropdownMenu.Content>
      </DropdownMenu.Portal>
    </DropdownMenu.Root>
  );
}

function providerLabel(p: ModelChoice["provider"]): string {
  switch (p) {
    case "openai":
      return "OpenAI";
    case "anthropic":
      return "Anthropic";
    case "bedrock":
      return "Bedrock";
    case "ollama":
      return "Ollama";
  }
}
