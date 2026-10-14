import { useMemo, useState } from "react";
import { Combobox } from "@base-ui/react/combobox";
import { Check, ChevronDown, Search } from "lucide-react";
import { FieldLabel } from "@/components/admin/admin-primitives";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import type { ModelChoice } from "@/components/workspace/model-catalog";
import { providerLabels } from "@/components/gateway-setup";
import {
  defaultGatewayTestModel,
  gatewayModelChoices,
  matchesGatewayModel,
} from "./gateway-models";

export function GatewayModelPicker({
  provider,
  configuredModel,
  value,
  onChange,
  disabled,
}: {
  provider: string;
  configuredModel: string;
  value: string;
  onChange: (value: string) => void;
  disabled: boolean;
}) {
  const choices = useMemo(
    () => gatewayModelChoices(provider, configuredModel),
    [provider, configuredModel],
  );
  const [custom, setCustom] = useState(choices.length === 0);
  return (
    <div className="space-y-2">
      {custom ? (
        <div>
          <FieldLabel htmlFor="test-gateway-custom-model">
            Custom model ID
          </FieldLabel>
          <Input
            id="test-gateway-custom-model"
            required
            disabled={disabled}
            value={value}
            onChange={(event) => onChange(event.target.value)}
            placeholder="Enter the exact ID from your provider"
          />
        </div>
      ) : (
        <div>
          <FieldLabel htmlFor="test-gateway-model">Model</FieldLabel>
          <Combobox.Root<ModelChoice>
            items={choices}
            value={choices.find((choice) => choice.model === value) ?? null}
            onValueChange={(choice) => {
              if (choice) onChange(choice.model);
            }}
            itemToStringLabel={(choice) => choice.label}
            itemToStringValue={(choice) => choice.model}
            isItemEqualToValue={(choice, selected) =>
              choice.model === selected.model
            }
            filter={matchesGatewayModel}
            disabled={disabled}
          >
            <Combobox.Trigger
              id="test-gateway-model"
              aria-label="Choose test model"
              className="flex h-10 w-full cursor-pointer items-center justify-between gap-3 rounded-lg border border-input bg-background px-3 text-left text-sm transition-colors hover:bg-muted focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:cursor-default disabled:opacity-50"
            >
              <span className="truncate">
                <Combobox.Value placeholder="Choose a model" />
              </span>
              <ChevronDown className="size-4 shrink-0 text-muted-foreground" />
            </Combobox.Trigger>
            <Combobox.Portal>
              <Combobox.Positioner
                sideOffset={6}
                className="z-[60] w-[var(--anchor-width)] max-w-[calc(100vw-2rem)]"
              >
                <Combobox.Popup className="overflow-hidden rounded-lg border border-border bg-popover text-popover-foreground shadow-sm">
                  <div className="relative border-b border-border p-2">
                    <Search className="pointer-events-none absolute left-5 top-5 size-4 text-muted-foreground" />
                    <Combobox.Input
                      aria-label="Search models"
                      placeholder="Search models…"
                      className="h-10 w-full rounded-md border border-input bg-background pl-9 pr-3 text-sm outline-none focus-visible:ring-2 focus-visible:ring-ring"
                    />
                  </div>
                  <Combobox.Empty>
                    <p className="p-4 text-sm text-muted-foreground">
                      No matching models. You can enter a custom model ID
                      instead.
                    </p>
                  </Combobox.Empty>
                  <Combobox.List className="max-h-[min(280px,var(--available-height))] overflow-y-auto p-1">
                    {(choice: ModelChoice) => (
                      <Combobox.Item
                        key={choice.model}
                        value={choice}
                        className="flex cursor-pointer items-center justify-between gap-3 rounded-md px-3 py-2 text-sm outline-none data-highlighted:bg-muted"
                      >
                        <span className="min-w-0">
                          <span className="block font-medium">
                            {choice.label}
                          </span>
                          <span className="mt-0.5 block text-xs text-muted-foreground">
                            {choice.description}
                          </span>
                        </span>
                        <Combobox.ItemIndicator>
                          <Check className="size-4" />
                        </Combobox.ItemIndicator>
                      </Combobox.Item>
                    )}
                  </Combobox.List>
                </Combobox.Popup>
              </Combobox.Positioner>
            </Combobox.Portal>
          </Combobox.Root>
        </div>
      )}
      <div className="flex flex-wrap items-center justify-between gap-x-3 gap-y-1">
        <p className="text-xs text-muted-foreground">
          {providerLabels[provider] ?? provider} · Your provider account
          controls availability.
        </p>
        {choices.length > 0 ? (
          <Button
            type="button"
            variant="ghost"
            size="sm"
            disabled={disabled}
            className="h-auto px-0 py-1 text-xs underline underline-offset-4"
            onClick={() => {
              if (custom && !choices.some((choice) => choice.model === value))
                onChange(defaultGatewayTestModel(provider, configuredModel));
              setCustom(!custom);
            }}
          >
            {custom ? "Choose from list" : "Enter a custom model ID"}
          </Button>
        ) : null}
      </div>
    </div>
  );
}
