import * as DropdownMenu from "@radix-ui/react-dropdown-menu";
import { Check, ChevronDown, Sparkles } from "lucide-react";

import { DEFAULT_MODELS, MODEL_PROVIDERS, type ModelChoice, type ModelProvider } from "./model-catalog";
export * from "./model-catalog";

export function ModelPicker({
  value,
  onChange,
  available = DEFAULT_MODELS,
}: {
  value: ModelChoice;
  onChange: (model: ModelChoice) => void;
  available?: ModelChoice[];
}) {
  const groups = MODEL_PROVIDERS.map((provider) => ({
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
    case "xai": return "Grok (xAI)";
    case "openrouter": return "OpenRouter";
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
    case "xai": return "X";
    case "openrouter": return "OR";
    case "groq": return "GQ";
    case "bedrock": return "AWS";
    case "ollama": return "OL";
  }
}
