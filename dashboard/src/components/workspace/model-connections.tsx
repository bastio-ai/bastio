import { useEffect, useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { CheckCircle2, KeyRound, Loader2, RefreshCw } from "lucide-react";
import { Button } from "@/components/ui/button";
import { GatewayModelPicker } from "@/components/gateways/gateway-model-picker";
import { useGatewayActions } from "@/components/gateways/use-gateway-actions";
import { defaultListedGatewayModel } from "@/components/gateways/gateway-models";
import { providerLabel, MODEL_PROVIDERS } from "./model-picker";
import { workspaceApi } from "./types";
import { workspaceModelChoices, workspaceModelError } from "./workspace-models";

export function ModelConnections() {
  const qc = useQueryClient();
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
    <section
      className="space-y-4 rounded-xl border border-border bg-card p-4 sm:p-5"
      aria-label="Provider connection and model test"
    >
      <div>
        <h3 className="text-base font-bold tracking-tight">
          Connect and test your AI
        </h3>
        <p className="mt-1 text-sm text-muted-foreground">
          Admins manage provider credentials. Employees use approved models
          without handling keys.
        </p>
      </div>
      <label className="block space-y-1.5 text-sm">
        <span>AI provider</span>
        <select
          aria-label="AI provider"
          value={provider}
          onChange={(event) => {
            setProvider(event.target.value);
            setModel("");
            test.reset();
            approve.reset();
          }}
          className="h-10 w-full cursor-pointer rounded-lg border border-input bg-background px-3"
        >
          {MODEL_PROVIDERS.map((item) => (
            <option key={item} value={item}>
              {providerLabel(item)}
            </option>
          ))}
        </select>
      </label>
      <div className="flex flex-wrap gap-2">
        <Button
          variant="outline"
          size="sm"
          onClick={() => actions.openProviderDialog(provider)}
        >
          <KeyRound />
          {current?.configured
            ? "Add or replace provider key"
            : "Add provider key"}
        </Button>
        <Button
          variant="outline"
          size="sm"
          disabled={!current?.configured || discover.isPending}
          onClick={() => discover.mutate()}
        >
          <RefreshCw className={discover.isPending ? "animate-spin" : ""} />
          Refresh models
        </Button>
      </div>
      {inventory.isPending ? (
        <p role="status" className="text-sm text-muted-foreground">
          Discovering workspace models…
        </p>
      ) : inventory.isError ? (
        <p role="alert" className="text-sm text-destructive">
          {inventory.error.message}
        </p>
      ) : !current?.configured ? (
        <p className="rounded-lg bg-muted/30 p-3 text-sm text-muted-foreground">
          Connect this provider to discover models and send a small test.
        </p>
      ) : (
        <>
          {current.error_code ? (
            <p role="alert" className="text-sm text-muted-foreground">
              Discovery could not complete.{" "}
              {workspaceModelError(current.error_code)} You can still enter a
              custom model ID and test it.
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
          <p className="text-xs text-muted-foreground">
            {current.source === "provider"
              ? "Models reported by this workspace’s provider key. A successful test confirms the selected model can answer."
              : "Suggested models. This provider does not support discovery here; test a model to confirm access."}
          </p>
          {evidence?.checked_at ? (
            <p className="text-xs text-muted-foreground">
              Last check: {new Date(evidence.checked_at).toLocaleString()} ·{" "}
              {evidence.check_status === "passed"
                ? "Reply received"
                : workspaceModelError(evidence.error_code)}
            </p>
          ) : null}
          <div className="flex flex-wrap items-center gap-2">
            <Button
              disabled={!model || test.isPending}
              onClick={() => test.mutate()}
            >
              {test.isPending ? <Loader2 className="animate-spin" /> : null}
              {test.isPending ? "Sending hello…" : "Test selected model"}
            </Button>
            {test.data?.success || evidence?.check_status === "passed" ? (
              <Button
                variant="outline"
                disabled={approve.isPending}
                onClick={() => approve.mutate()}
              >
                <CheckCircle2 />
                Approve for employees
              </Button>
            ) : null}
          </div>
          <p className="text-xs text-muted-foreground">
            Approving a model starts an explicit employee allowlist if none
            exists. Review Models & AI before inviting your team.
          </p>
          <p className="text-xs text-muted-foreground">
            A small live request. Provider usage charges and workspace limits
            apply. Test content is not saved in conversation history.
          </p>
        </>
      )}
      {test.data ? (
        <div
          aria-live="polite"
          className="rounded-lg border border-border bg-muted/20 p-3"
        >
          <p
            className={
              test.data.success
                ? "text-sm font-medium text-success"
                : "text-sm font-medium text-destructive"
            }
          >
            {test.data.success
              ? "Connected. Your model replied."
              : "Model test did not complete"}
          </p>
          <p className="mt-1 break-words text-sm">
            {test.data.success
              ? test.data.reply
              : workspaceModelError(test.data.error_code)}
          </p>
        </div>
      ) : null}
      {test.error || discover.error || approve.error ? (
        <p role="alert" className="text-sm text-destructive">
          {(test.error || discover.error || approve.error)?.message}
        </p>
      ) : null}
      {approve.isSuccess ? (
        <p role="status" className="text-sm text-success">
          This model is approved for employees. Review the full list below
          whenever you need to change access.
        </p>
      ) : null}
      {actions.dialogs}
    </section>
  );
}
