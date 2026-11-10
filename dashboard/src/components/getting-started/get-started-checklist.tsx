import { useState, type ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import { CheckCircle2, KeyRound, Plus, X } from "lucide-react";

import { api } from "@/api/client";
import { DataPanel } from "@/components/data/data-panel";
import { StatusDot } from "@/components/data/status-dot";
import { REFRESH_INTERVALS } from "@/components/dashboard-refresh-policy";
import { ConnectionExample } from "@/components/gateways/connection-example";
import { useGatewayExtension } from "@/components/gateways/extension-context";
import { useGatewayActions } from "@/components/gateways/use-gateway-actions";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import {
  deriveGetStarted,
  readDismissed,
  shouldShowGetStarted,
  writeDismissed,
  type GetStartedStepId,
} from "./getting-started";

const browserStorage = () =>
  typeof window === "undefined" ? undefined : window.localStorage;

/**
 * First-run setup guide on the Overview page. Renders nothing until every
 * query it depends on has loaded (no flash), once the tenant has any trace,
 * after dismissal, or when a query fails.
 */
export function GetStartedChecklist() {
  const extension = useGatewayExtension();
  const [dismissed, setDismissed] = useState(() => readDismissed(browserStorage));

  // Canonical keys: the gateway dialogs invalidate exactly these, so a step
  // flips to done the moment its dialog succeeds.
  const providerKeys = useQuery({ queryKey: ["provider-keys"], queryFn: api.providerKeys.list, enabled: !dismissed });
  const proxies = useQuery({ queryKey: ["proxies"], queryFn: api.proxies.list });
  const apiKeys = useQuery({ queryKey: ["api-keys"], queryFn: api.apiKeys.list });
  // Unbounded (no time window or environment): "has this tenant ever sent a
  // request". Polls at the dashboard's shortest refresh interval only while
  // the guide is waiting for that first trace; also refetches on window focus.
  const firstTrace = useQuery({
    queryKey: ["traces", "first"],
    queryFn: () => api.traces.list({ limit: 1 }),
    enabled: !dismissed,
    refetchInterval: (query) =>
      dismissed || (query.state.data?.length ?? 0) > 0 ? false : REFRESH_INTERVALS[0].value,
  });

  const actions = useGatewayActions(() => {});

  // The dialogs stay mounted even after the panel hides: the first trace can
  // arrive while the "save your key" dialog is still open.
  if (dismissed || !providerKeys.isSuccess || !proxies.isSuccess || !apiKeys.isSuccess || !firstTrace.isSuccess)
    return actions.dialogs;

  const state = deriveGetStarted({
    providerKeys: providerKeys.data,
    managedProviders: extension.managedProviders,
    proxies: proxies.data,
    apiKeys: apiKeys.data,
    traceCount: firstTrace.data.length,
  });
  if (!shouldShowGetStarted(state, dismissed)) return actions.dialogs;

  const dismiss = () => {
    writeDismissed(browserStorage);
    setDismissed(true);
  };
  const gateway = state.gateway;
  const providerAllowed = extension.providerKeyManagement === "allowed";
  const isCurrent = (id: GetStartedStepId) => state.current === id;

  return (
    <>
      <DataPanel
        title="Get started"
        sub={`${state.completed} of 3 complete`}
        action={
          <Button
            variant="ghost"
            size="icon-sm"
            aria-label="Dismiss setup guide"
            title="Dismiss"
            onClick={dismiss}
          >
            <X />
          </Button>
        }
      >
        <ol aria-label="Setup steps" className="divide-y divide-border-subtle">
          <Step
            index={1}
            title="Add a provider key"
            detail={
              state.done.provider
                ? "Bastio can call your AI provider."
                : providerAllowed
                  ? "Paste a secret from OpenAI, Anthropic or another provider. Bastio uses it to call models for you."
                  : "Provider key changes are unavailable for this account right now."
            }
            done={state.done.provider}
            current={isCurrent("provider")}
            action={
              <Button
                size="sm"
                variant={isCurrent("provider") ? "default" : "outline"}
                disabled={!providerAllowed}
                onClick={() =>
                  actions.openProviderDialog("openai", {
                    thenCreateGateway: proxies.data.length === 0,
                  })
                }
              >
                <Plus /> Add provider key
              </Button>
            }
          />
          <Step
            index={2}
            title="Create a gateway and an API key"
            detail={
              state.done.gateway
                ? "Your application has a gateway and a key to call it."
                : gateway
                  ? `Create the Bastio API key your application uses to call ${gateway.name}.`
                  : "A gateway routes your application’s requests through Bastio’s security controls."
            }
            done={state.done.gateway}
            current={isCurrent("gateway")}
            action={
              gateway ? (
                <Button
                  size="sm"
                  variant={isCurrent("gateway") ? "default" : "outline"}
                  onClick={() => actions.openAppKey(gateway)}
                >
                  <KeyRound /> Create API key
                </Button>
              ) : (
                <Button
                  size="sm"
                  variant={isCurrent("gateway") ? "default" : "outline"}
                  onClick={actions.openCreate}
                >
                  <Plus /> Create gateway
                </Button>
              )
            }
          />
          <Step
            index={3}
            title="Send your first request"
            detail={
              gateway
                ? `Export your key as BASTIO_API_KEY and run this against ${gateway.name}. This step completes when the first trace arrives.`
                : "Once you have a gateway, a ready-to-run request appears here."
            }
            done={state.done.request}
            current={isCurrent("request")}
          >
            {gateway && state.done.gateway ? (
              <div className="mt-3 space-y-2">
                <ConnectionExample proxy={gateway} showTest />
                <p role="status" className="flex items-center gap-2 font-mono text-[11px] text-muted-foreground">
                  <StatusDot tone="neutral" size={6} />
                  Waiting for the first trace · checks every {REFRESH_INTERVALS[0].short}
                </p>
              </div>
            ) : null}
          </Step>
        </ol>
      </DataPanel>
      {actions.dialogs}
    </>
  );
}

function Step({
  index,
  title,
  detail,
  done,
  current,
  action,
  children,
}: {
  index: number;
  title: string;
  detail: string;
  done: boolean;
  current: boolean;
  action?: ReactNode;
  children?: ReactNode;
}) {
  return (
    <li
      aria-current={current ? "step" : undefined}
      className={cn("flex gap-3 px-4 py-3", current && "bg-surface-2/40")}
    >
      <span
        aria-hidden="true"
        className={cn(
          "mt-px flex size-6 shrink-0 items-center justify-center rounded-md border font-mono text-[11px] tabular-nums",
          done
            ? "border-success-border bg-success-bg text-success"
            : current
              ? "border-border-strong text-foreground"
              : "border-border-subtle text-muted-foreground",
        )}
      >
        {done ? <CheckCircle2 className="size-3.5" /> : index}
      </span>
      <div className="min-w-0 flex-1">
        <div className="flex flex-wrap items-start justify-between gap-x-3 gap-y-2">
          <div className="min-w-0">
            <p className={cn("text-[13px] font-medium", done ? "text-muted-foreground" : "text-foreground")}>
              {title}
              <span className="sr-only">{done ? " — done" : " — not done"}</span>
            </p>
            {!done ? <p className="mt-0.5 text-xs leading-relaxed text-muted-foreground">{detail}</p> : null}
          </div>
          {!done && action ? <div className="shrink-0">{action}</div> : null}
        </div>
        {!done ? children : null}
      </div>
    </li>
  );
}
