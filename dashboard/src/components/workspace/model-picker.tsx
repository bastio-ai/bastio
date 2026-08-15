import * as DropdownMenu from "@radix-ui/react-dropdown-menu";
import { Check, ChevronDown, Sparkles } from "lucide-react";

export type ModelProvider =
  | "openai"
  | "anthropic"
  | "gemini"
  | "deepseek"
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

// Current production catalogue, verified against provider documentation.
// Keep this focused: image, audio, moderation, and embedding-only models
// do not belong in a text-first employee chat picker.
export const MODEL_CATALOG: ModelChoice[] = [
  {
    provider: "openai",
    model: "gpt-5.6-terra",
    label: "GPT-5.6 Terra",
    description: "Balanced intelligence, speed, and cost for daily work",
    badge: "Recommended",
  },
  {
    provider: "openai",
    model: "gpt-5.6-sol",
    label: "GPT-5.6 Sol",
    description: "Highest capability for complex analysis and coding",
    badge: "Advanced",
  },
  {
    provider: "openai",
    model: "gpt-5.6-luna",
    label: "GPT-5.6 Luna",
    description: "Fast, cost-efficient model for high-volume tasks",
    badge: "Fast",
  },
  {
    provider: "anthropic",
    model: "claude-sonnet-5",
    label: "Claude Sonnet 5",
    description: "Fast, capable model for everyday professional work",
    badge: "Popular",
  },
  {
    provider: "anthropic",
    model: "claude-fable-5",
    label: "Claude Fable 5",
    description: "Long-running agents and complex enterprise workflows",
    badge: "Flagship",
  },
  {
    provider: "anthropic",
    model: "claude-opus-5",
    label: "Claude Opus 5",
    description: "Deep reasoning for demanding professional tasks",
    badge: "Advanced",
  },
  {
    provider: "anthropic",
    model: "claude-haiku-4-5",
    label: "Claude Haiku 4.5",
    description: "Anthropic's fastest model for lightweight tasks",
    badge: "Fast",
  },
  {
    provider: "gemini",
    model: "gemini-3.7-flash",
    label: "Gemini 3.7 Flash",
    description: "Google's latest fast multimodal model",
    badge: "New",
  },
  {
    provider: "gemini",
    model: "gemini-3.6-flash",
    label: "Gemini 3.6 Flash",
    description: "Stable, high-throughput multimodal reasoning",
  },
  {
    provider: "gemini",
    model: "gemini-3.5-flash-lite",
    label: "Gemini 3.5 Flash-Lite",
    description: "Efficient model for frequent, lightweight work",
    badge: "Efficient",
  },
  {
    provider: "deepseek",
    model: "deepseek-v4-pro",
    label: "DeepSeek V4 Pro",
    description: "Reasoning-first model for complex work",
    badge: "Reasoning",
  },
  {
    provider: "deepseek",
    model: "deepseek-v4-flash",
    label: "DeepSeek V4 Flash",
    description: "High-speed model with optional thinking",
    badge: "Fast",
  },
  {
    provider: "groq",
    model: "openai/gpt-oss-120b",
    label: "GPT OSS 120B",
    description: "Large open-weight model served at Groq speed",
  },
  {
    provider: "groq",
    model: "llama-3.3-70b-versatile",
    label: "Llama 3.3 70B",
    description: "Versatile production model for general work",
  },
  {
    provider: "groq",
    model: "llama-3.1-8b-instant",
    label: "Llama 3.1 8B Instant",
    description: "Ultra-fast model for concise, lightweight tasks",
    badge: "Fast",
  },

  // Backwards compatibility for existing assistants and explicit
  // tenant whitelists. These never appear in a default-open workspace.
  { provider: "openai", model: "gpt-5.4-mini", label: "GPT-5.4 Mini", description: "Previous-generation compact model", legacy: true },
  { provider: "openai", model: "gpt-5.4", label: "GPT-5.4", description: "Previous-generation flagship model", legacy: true },
  { provider: "openai", model: "gpt-4o-mini", label: "GPT-4o Mini", description: "Legacy compact multimodal model", legacy: true },
  { provider: "openai", model: "gpt-4o", label: "GPT-4o", description: "Legacy multimodal model", legacy: true },
  { provider: "anthropic", model: "claude-sonnet-4-6", label: "Claude Sonnet 4.6", description: "Previous-generation balanced Claude model", legacy: true },
  { provider: "anthropic", model: "claude-opus-4-7", label: "Claude Opus 4.7", description: "Previous-generation advanced Claude model", legacy: true },
];

// Default-open workspaces see only the current catalogue. Legacy models
// remain available when an administrator explicitly whitelists them.
export const DEFAULT_MODELS: ModelChoice[] = MODEL_CATALOG.filter((model) => !model.legacy);
export const DEFAULT_MODEL: ModelChoice = DEFAULT_MODELS[0]!;

export type AllowedModelEntry = { provider: string; model: string };

export function pickModelChoices(allowed: AllowedModelEntry[]): ModelChoice[] {
  if (!allowed || allowed.length === 0) return DEFAULT_MODELS;
  const allowSet = new Set(allowed.map((entry) => `${entry.provider}/${entry.model}`));
  return MODEL_CATALOG.filter((model) => allowSet.has(`${model.provider}/${model.model}`));
}

const PROVIDER_ORDER: ModelProvider[] = [
  "openai",
  "anthropic",
  "gemini",
  "deepseek",
  "groq",
  "bedrock",
  "ollama",
];

export function ModelPicker({
  value,
  onChange,
  available = DEFAULT_MODELS,
}: {
  value: ModelChoice;
  onChange: (model: ModelChoice) => void;
  available?: ModelChoice[];
}) {
  const groups = PROVIDER_ORDER.map((provider) => ({
    provider,
    models: available.filter((model) => model.provider === provider),
  })).filter((group) => group.models.length > 0);

  return (
    <DropdownMenu.Root>
      <DropdownMenu.Trigger asChild>
        <button
          type="button"
          className="group inline-flex h-8 items-center gap-2 rounded-[5px] border border-border bg-background px-2.5 text-xs font-medium text-foreground transition-colors hover:border-foreground/20 hover:bg-muted focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
          aria-label={`Change model. Current model: ${value.label}`}
        >
          <span className="flex h-4 w-4 items-center justify-center rounded-[4px] bg-cyan-500/10 text-cyan-500">
            <Sparkles className="h-3 w-3" />
          </span>
          <span className="max-w-[150px] truncate">{value.label}</span>
          <ChevronDown className="h-3 w-3 text-muted-foreground transition-transform group-data-[state=open]:rotate-180" />
        </button>
      </DropdownMenu.Trigger>
      <DropdownMenu.Portal>
        <DropdownMenu.Content
          align="start"
          sideOffset={8}
          collisionPadding={12}
          className="z-50 max-h-[min(520px,70vh)] w-[min(360px,calc(100vw-24px))] overflow-y-auto rounded-[7px] border border-border bg-popover p-1.5 text-sm shadow-lg"
        >
          <div className="px-2.5 pb-2 pt-1.5">
            <p className="text-[13px] font-semibold tracking-tight text-foreground">Choose a model</p>
            <p className="mt-0.5 text-[11px] leading-4 text-muted-foreground">Applies to your next message. Your workspace policy still controls access.</p>
          </div>
          <DropdownMenu.Separator className="mb-1 h-px bg-border" />
          {groups.map((group, groupIndex) => (
            <div key={group.provider}>
              {groupIndex > 0 ? <DropdownMenu.Separator className="my-1 h-px bg-border/70" /> : null}
              <DropdownMenu.Label className="flex items-center gap-2 px-2.5 py-1.5 text-[10px] font-medium uppercase tracking-[0.11em] text-muted-foreground">
                <span className="flex h-4 w-4 items-center justify-center rounded-[3px] border border-border bg-muted/40 text-[8px] font-semibold text-foreground">
                  {providerMark(group.provider)}
                </span>
                {providerLabel(group.provider)}
              </DropdownMenu.Label>
              {group.models.map((model) => {
                const active = value.provider === model.provider && value.model === model.model;
                return (
                  <DropdownMenu.Item
                    key={`${model.provider}/${model.model}`}
                    onSelect={() => onChange(model)}
                    className="group/item grid cursor-pointer grid-cols-[1fr_auto] gap-3 rounded-[5px] px-2.5 py-2 outline-none data-[highlighted]:bg-muted"
                  >
                    <span className="min-w-0">
                      <span className="flex items-center gap-2">
                        <span className="truncate text-[12px] font-medium text-foreground">{model.label}</span>
                        {model.badge ? (
                          <span className="shrink-0 rounded-[3px] border border-border bg-background px-1.5 py-0.5 text-[8px] font-medium uppercase tracking-[0.08em] text-muted-foreground">
                            {model.badge}
                          </span>
                        ) : null}
                      </span>
                      <span className="mt-0.5 block truncate text-[10px] leading-4 text-muted-foreground">{model.description}</span>
                    </span>
                    <span className="flex h-7 w-4 items-center justify-center">
                      {active ? <Check className="h-3.5 w-3.5 text-cyan-500" /> : null}
                    </span>
                  </DropdownMenu.Item>
                );
              })}
            </div>
          ))}
        </DropdownMenu.Content>
      </DropdownMenu.Portal>
    </DropdownMenu.Root>
  );
}

export function providerLabel(provider: ModelProvider): string {
  switch (provider) {
    case "openai": return "OpenAI";
    case "anthropic": return "Anthropic";
    case "gemini": return "Google Gemini";
    case "deepseek": return "DeepSeek";
    case "groq": return "Groq";
    case "bedrock": return "Amazon Bedrock";
    case "ollama": return "Ollama";
  }
}

function providerMark(provider: ModelProvider): string {
  switch (provider) {
    case "openai": return "O";
    case "anthropic": return "A";
    case "gemini": return "G";
    case "deepseek": return "D";
    case "groq": return "GQ";
    case "bedrock": return "AWS";
    case "ollama": return "OL";
  }
}
