import { useEffect, useId, useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { CheckCircle2, KeyRound, Loader2, RefreshCw } from "lucide-react";
import { AdminPanel, FieldLabel } from "@/components/admin/admin-primitives";
import { Button } from "@/components/ui/button";
import { GatewayModelPicker } from "@/components/gateways/gateway-model-picker";
import { useGatewayActions } from "@/components/gateways/use-gateway-actions";
import { defaultListedGatewayModel } from "@/components/gateways/gateway-models";
import { cn } from "@/lib/utils";
import { providerLabel, MODEL_PROVIDERS } from "./model-picker";
import { workspaceApi } from "./types";
import {
  workspaceModelChoices,
  workspaceModelError,
} from "./workspace-models";

export function ModelConnections() {
  const qc = useQueryClient();
  const baseId = useId();
  const actions = useGatewayActions(() => {});
  const [provider, setProvider] = useState("openai");
  const [model, setModel] = useState("");
  const inventory = useQuery({
    queryKey: ["workspace", "models", true],
    queryFn: () => workspaceApi.modelInventory(true),
    staleTime: 60_000,
  });
  const current = inventory.data?.providers.find(
    (item) => item.provider === provider,
  );
  const choices = useMemo(
    () =>
      workspaceModelChoices(
        current ? { providers: [current], restricted: false } : undefined,
      ),
    [current],
  );
  useEffect(() => {
    setModel((current) => current || defaultListedGatewayModel(choices));
  }, [choices]);
  const refresh = async () => {
    await Promise.all([
      qc.invalidateQueries({ queryKey: ["workspace", "models"] }),
      qc.invalidateQueries({ queryKey: ["workspace", "setup"] }),
    ]);
  };
  const discover = useMutation({
    mutationFn: () => workspaceApi.discoverModels(provider),
    onSuccess: refresh,
  });
  const test = useMutation({
    mutationFn: () => workspaceApi.testModel(provider, model),
    onSuccess: refresh,
  });
  const approve = useMutation({
    mutationFn: async () => {
      const settings = await workspaceApi.getSettings();
      const allowed = settings.allowed_models ?? [];
      if (
        !allowed.some(
          (item) => item.provider === provider && item.model === model,
        )
      )
        await workspaceApi.patchSettings({
          allowed_models: [...allowed, { provider, model }],
        });
    },
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: ["workspace", "settings"] });
      await refresh();
    },
  });
  const evidence = current?.models.find((item) => item.id === model);

  return (
    <div role="region" aria-label="Provider connection and model test">
      <AdminPanel
        title="Connect and test your AI"
        description="Admins manage provider credentials. Employees use approved models without handling keys."
        action={
          <div className="flex flex-wrap items-center gap-2">
            <span className="inline-flex items-center gap-1.5 text-xs text-muted-foreground">
              <span
                aria-hidden="true"
                className={cn(
                  "size-1.5 shrink-0 rounded-full",
                  current?.configured
                    ? "bg-success"
                    : "bg-muted-foreground/50",
                )}
              />
              {current?.configured ? "Key configured" : "Not connected"}
            </span>
          </div>
        }
      >
        <div className="space-y-4 border-b border-border-subtle pb-4">
          <div className="flex flex-col gap-3 sm:flex-row sm:items-end">
            <div className="min-w-0 flex-1 sm:max-w-xs">
              <FieldLabel htmlFor={`${baseId}-provider`}>
                AI provider
              </FieldLabel>
              <select
                id={`${baseId}-provider`}
                aria-label="AI provider"
                value={provider}
                onChange={(event) => {
                  setProvider(event.target.value);
                  setModel("");
                  test.reset();
                  approve.reset();
                }}
                className="h-8 w-full cursor-pointer rounded-md border border-input bg-background px-2.5 text-xs text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
              >
                {MODEL_PROVIDERS.map((item) => (
                  <option key={item} value={item}>
                    {providerLabel(item)}
                  </option>
                ))}
              </select>
            </div>

            <div className="flex flex-wrap gap-2">
              <Button
                type="button"
                variant="outline"
                size="sm"
                className="h-8 text-xs"
                onClick={() => actions.openProviderDialog(provider)}
              >
                <KeyRound className="size-3.5" aria-hidden="true" />
                {current?.configured
                  ? "Add or replace provider key"
                  : "Add provider key"}
              </Button>
              <Button
                type="button"
                variant="outline"
                size="sm"
                className="h-8 text-xs"
                disabled={!current?.configured || discover.isPending}
                onClick={() => discover.mutate()}
              >
                <RefreshCw
                  aria-hidden="true"
                  className={cn(
                    "size-3.5",
                    discover.isPending && "animate-spin",
                  )}
                />
                Refresh models
              </Button>
            </div>
          </div>

          {inventory.isPending ? (
            <p role="status" className="text-xs text-muted-foreground">
              Discovering workspace models…
            </p>
          ) : inventory.isError ? (
            <p
              role="alert"
              className="border-l-2 border-destructive pl-3.5 text-xs text-destructive"
            >
              {inventory.error.message}
            </p>
          ) : !current?.configured ? (
            <p className="border-l-2 border-border-subtle pl-3.5 text-xs leading-relaxed text-muted-foreground">
              Connect this provider to discover models and send a small test.
            </p>
          ) : (
            <>
              {current.error_code ? (
                <p
                  role="alert"
                  className="border-l-2 border-warn pl-3.5 text-xs leading-relaxed text-muted-foreground"
                >
                  Discovery could not complete.{" "}
                  {workspaceModelError(current.error_code)} You can still enter
                  a custom model ID and test it.
                </p>
              ) : null}

              <GatewayModelPicker
                key={provider + choices.length}
                provider={provider}
                choices={choices}
                value={model}
                onChange={(next) => {
                  setModel(next);
                  test.reset();
                  approve.reset();
                }}
                disabled={test.isPending}
              />

              <p className="text-xs leading-relaxed text-muted-foreground">
                {current.source === "provider"
                  ? "Models reported by this workspace’s provider key. A successful test confirms the selected model can answer."
                  : "Suggested models. This provider does not support discovery here; test a model to confirm access."}
              </p>

              {evidence?.checked_at ? (
                <p className="flex items-center gap-2 text-xs text-muted-foreground">
                  <span
                    aria-hidden="true"
                    className={cn(
                      "size-1.5 shrink-0 rounded-full",
                      evidence.check_status === "passed"
                        ? "bg-success"
                        : "bg-warn",
                    )}
                  />
                  <span>
                    Last check:{" "}
                    <span className="font-mono tabular-nums text-foreground">
                      {new Date(evidence.checked_at).toLocaleString()}
                    </span>{" "}
                    ·{" "}
                    {evidence.check_status === "passed"
                      ? "Reply received"
                      : workspaceModelError(evidence.error_code)}
                  </span>
                </p>
              ) : null}

              <div className="flex flex-wrap items-center gap-2">
                <Button
                  type="button"
                  size="sm"
                  className="h-8 text-xs"
                  disabled={!model || test.isPending}
                  onClick={() => test.mutate()}
                >
                  {test.isPending ? (
                    <Loader2 className="size-3.5 animate-spin" aria-hidden="true" />
                  ) : null}
                  {test.isPending ? "Sending hello…" : "Test selected model"}
                </Button>
                {test.data?.success || evidence?.check_status === "passed" ? (
                  <Button
                    type="button"
                    size="sm"
                    variant="outline"
                    className="h-8 text-xs"
                    disabled={approve.isPending}
                    onClick={() => approve.mutate()}
                  >
                    <CheckCircle2 className="size-3.5" aria-hidden="true" />
                    Approve for employees
                  </Button>
                ) : null}
              </div>

              <div className="space-y-1 border-l-2 border-border-subtle pl-3.5 text-xs leading-relaxed text-muted-foreground">
                <p>
                  Approving a model starts an explicit employee allowlist if
                  none exists. Review Models & AI before inviting your team.
                </p>
                <p>
                  A small live request. Provider usage charges and workspace
                  limits apply. Test content is not saved in conversation
                  history.
                </p>
              </div>
            </>
          )}

          {test.data ? (
            <div
              aria-live="polite"
              className={cn(
                "space-y-1 border-l-2 pl-3.5",
                test.data.success
                  ? "border-success/70"
                  : "border-destructive",
              )}
            >
              <p
                className={cn(
                  "flex items-center gap-2 text-xs font-medium",
                  test.data.success ? "text-success" : "text-destructive",
                )}
              >
                <span
                  aria-hidden="true"
                  className={cn(
                    "size-1.5 shrink-0 rounded-full",
                    test.data.success ? "bg-success" : "bg-destructive",
                  )}
                />
                {test.data.success
                  ? "Connected. Your model replied."
                  : "Model test did not complete"}
              </p>
              <p className="break-words text-xs leading-relaxed text-foreground">
                {test.data.success
                  ? test.data.reply
                  : workspaceModelError(test.data.error_code)}
              </p>
            </div>
          ) : null}

          {test.error || discover.error || approve.error ? (
            <p
              role="alert"
              className="border-l-2 border-destructive pl-3.5 text-xs text-destructive"
            >
              {(test.error || discover.error || approve.error)?.message}
            </p>
          ) : null}

          {approve.isSuccess ? (
            <p
              role="status"
              className="border-l-2 border-success/70 pl-3.5 text-xs text-success"
            >
              This model is approved for employees. Review the full list below
              whenever you need to change access.
            </p>
          ) : null}
        </div>
      </AdminPanel>
      {actions.dialogs}
    </div>
  );
}
