import { useState } from "react";
import { Link, useNavigate, useSearch } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import { ArrowRight, KeyRound, Plus, Search, Server } from "lucide-react";
import {
  FieldLabel,
  SecurityNotice,
} from "@/components/admin/admin-primitives";
import { EmptyState, PageHeader } from "@/components/card";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { api } from "@/api/client";
import {
  gatewaySetupStatus,
  providerLabels,
  type GatewaySearch,
} from "@/components/gateway-setup";
import { GatewayPanel } from "@/components/gateways/gateway-panel";
import { useGatewayActions } from "@/components/gateways/use-gateway-actions";
import {
  QueryError,
  ProviderKeyList,
  navigationClass,
  controlClass,
} from "@/components/gateways/primitives";
import { cn } from "@/lib/utils";

export function ProxiesPage() {
  const navigate = useNavigate();
  const search = useSearch({ strict: false }) as GatewaySearch;
  const proxies = useQuery({
    queryKey: ["proxies"],
    queryFn: api.proxies.list,
  });
  const providerKeys = useQuery({
    queryKey: ["provider-keys"],
    queryFn: api.providerKeys.list,
  });
  const appKeys = useQuery({
    queryKey: ["api-keys"],
    queryFn: api.apiKeys.list,
  });
  const [filter, setFilter] = useState("");
  const selected =
    proxies.data?.find((p) => p.id === search.gateway) ?? proxies.data?.[0];
  const visibleProxies = (proxies.data ?? []).filter((p) =>
    `${p.name} ${providerLabels[p.target_provider] ?? p.target_provider} ${p.target_model}`
      .toLowerCase()
      .includes(filter.toLowerCase().trim()),
  );
  const selectGateway = (id: string) =>
    navigate({ to: "/proxies", search: { gateway: id } });
  const actions = useGatewayActions((id) => {
    setFilter("");
    void selectGateway(id);
  });
  return (
    <>
      <PageHeader
        className="flex-col xl:flex-row"
        title="LLM Gateways"
        description="Connect your application to an AI provider through Bastio’s security controls."
        action={
          <div className="flex flex-wrap gap-2">
            <Button
              nativeButton={false}
              variant="outline"
              className="cursor-pointer"
              render={<Link to="/api-keys" />}
            >
              <KeyRound /> Manage Bastio API keys
            </Button>
            <Button className="cursor-pointer" onClick={actions.openCreate}>
              <Plus /> Create gateway
            </Button>
          </div>
        }
      />

      <nav
        aria-label="Gateway management"
        className="mb-5 flex gap-1 border-b border-border pb-2"
      >
        <button
          type="button"
          aria-current={!search.tab ? "page" : undefined}
          className={cn(
            navigationClass,
            !search.tab && "bg-muted text-foreground",
          )}
          onClick={() =>
            navigate({ to: "/proxies", search: { gateway: selected?.id } })
          }
        >
          Gateways{" "}
          <span className="ml-1 text-muted-foreground">
            {proxies.data?.length ?? "—"}
          </span>
        </button>
        <button
          type="button"
          aria-current={search.tab === "providers" ? "page" : undefined}
          className={cn(
            navigationClass,
            search.tab === "providers" && "bg-muted text-foreground",
          )}
          onClick={() =>
            navigate({
              to: "/proxies",
              search: { gateway: selected?.id, tab: "providers" },
            })
          }
        >
          Provider keys{" "}
          <span className="ml-1 text-muted-foreground">
            {providerKeys.data?.length ?? "—"}
          </span>
        </button>
      </nav>

      {search.tab === "providers" ? (
        <section className="rounded-xl border border-border bg-card p-5 sm:p-6">
          <div className="mb-5 flex flex-wrap items-start justify-between gap-4">
            <div>
              <h2 className="text-lg font-bold tracking-tight">
                Keys from your AI providers
              </h2>
              <p className="mt-1 max-w-2xl text-sm leading-relaxed text-muted-foreground">
                Bastio uses these secrets to call OpenAI, Claude, OpenRouter,
                and other providers. Your applications use a separate Bastio API
                key.
              </p>
            </div>
            <Button
              className="cursor-pointer"
              onClick={() => actions.openProviderDialog("openai")}
            >
              <Plus /> Add provider key
            </Button>
          </div>
          <SecurityNotice title="Shared within your workspace" className="mb-5">
            New provider keys are saved as shared defaults. Gateways for the
            same provider and workspace AI using your own keys can use them.
            Adding a key does not remove an existing one.
          </SecurityNotice>
          {providerKeys.isPending ? (
            <p className="text-sm text-muted-foreground">
              Loading provider keys…
            </p>
          ) : providerKeys.isError ? (
            <QueryError
              title="Provider keys could not be loaded"
              error={providerKeys.error}
              retry={() => providerKeys.refetch()}
            />
          ) : !providerKeys.data.length ? (
            <EmptyState
              icon={<KeyRound />}
              title="No provider keys yet"
              description="Add the secret from your provider’s account to connect Bastio to its models."
              action={
                <Button onClick={() => actions.openProviderDialog("openai")}>
                  Add first provider key
                </Button>
              }
            />
          ) : (
            <ProviderKeyList
              keys={providerKeys.data}
              onDelete={actions.deleteProvider}
            />
          )}
        </section>
      ) : (
        <>
          <ol
            className="mb-6 grid gap-4 rounded-xl border border-border bg-card p-5 sm:grid-cols-3"
            aria-label="Gateway setup steps"
          >
            {[
              ["Choose a provider", "Create a gateway for your application."],
              [
                "Add the provider key",
                "Bastio uses it to call the AI provider.",
              ],
              ["Connect your app", "Use a Bastio API key and the gateway URL."],
            ].map(([title, description], i) => (
              <li key={title} className="flex gap-3">
                <span className="flex size-7 shrink-0 items-center justify-center rounded-full bg-muted font-mono text-xs">
                  {i + 1}
                </span>
                <div>
                  <p className="text-sm font-medium">{title}</p>
                  <p className="mt-1 text-xs leading-relaxed text-muted-foreground">
                    {description}
                  </p>
                </div>
              </li>
            ))}
          </ol>
          {proxies.isPending ? (
            <p className="py-12 text-center text-sm text-muted-foreground">
              Loading gateways…
            </p>
          ) : proxies.isError ? (
            <QueryError
              title="Gateways could not be loaded"
              error={proxies.error}
              retry={() => proxies.refetch()}
            />
          ) : !proxies.data.length ? (
            <div className="rounded-xl border border-border bg-card">
              <EmptyState
                icon={<Server />}
                title="Connect your first application"
                description="Create a gateway, add your provider key, and copy the connection details into your app."
                action={
                  <Button onClick={actions.openCreate}>
                    <Plus /> Create your first gateway
                  </Button>
                }
              />
            </div>
          ) : (
            <div className="grid items-start gap-5 lg:grid-cols-[260px_minmax(0,1fr)]">
              <div className="lg:hidden">
                <FieldLabel htmlFor="selected-gateway">Your gateway</FieldLabel>
                <select
                  id="selected-gateway"
                  className={controlClass}
                  value={selected?.id ?? ""}
                  onChange={(event) => selectGateway(event.target.value)}
                >
                  {proxies.data.map((proxy) => (
                    <option key={proxy.id} value={proxy.id}>
                      {proxy.name} —{" "}
                      {providerLabels[proxy.target_provider] ??
                        proxy.target_provider}
                    </option>
                  ))}
                </select>
              </div>
              <section
                className="hidden min-w-0 overflow-hidden rounded-xl border border-border bg-card lg:block"
                aria-label="Your gateways"
              >
                <div className="border-b border-border p-4">
                  <h2 className="text-sm font-bold tracking-tight">
                    Your gateways
                  </h2>
                  <div className="relative mt-3">
                    <Search className="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
                    <Input
                      aria-label="Search gateways"
                      value={filter}
                      onChange={(e) => setFilter(e.target.value)}
                      placeholder="Search gateways…"
                      className="h-10 pl-9"
                    />
                  </div>
                </div>
                <div className="max-h-96 overflow-y-auto p-2 lg:max-h-[640px]">
                  {visibleProxies.length ? (
                    visibleProxies.map((p) => {
                      const status = gatewaySetupStatus(
                        p,
                        providerKeys.data,
                        appKeys.data,
                      );
                      return (
                        <button
                          type="button"
                          key={p.id}
                          aria-current={
                            selected?.id === p.id ? "page" : undefined
                          }
                          onClick={() => selectGateway(p.id)}
                          className={cn(
                            "mb-1 flex w-full cursor-pointer items-start gap-3 rounded-lg border border-transparent p-3 text-left transition-colors hover:bg-muted focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring",
                            selected?.id === p.id && "border-border bg-muted",
                          )}
                        >
                          <Server className="mt-0.5 size-4 shrink-0 text-muted-foreground" />
                          <span className="min-w-0 flex-1">
                            <span className="block truncate text-sm font-medium">
                              {p.name}
                            </span>
                            <span className="mt-1 block text-xs text-muted-foreground">
                              {providerLabels[p.target_provider] ??
                                p.target_provider}
                            </span>
                            <span
                              className={cn(
                                "mt-2 block text-xs",
                                status === "Setup complete"
                                  ? "text-success"
                                  : "text-muted-foreground",
                              )}
                            >
                              {providerKeys.isError || appKeys.isError
                                ? "Setup unavailable"
                                : status}
                            </span>
                          </span>
                          <ArrowRight className="mt-0.5 size-4 shrink-0 text-muted-foreground" />
                        </button>
                      );
                    })
                  ) : (
                    <p className="p-3 text-sm text-muted-foreground">
                      No gateways match your search.
                    </p>
                  )}
                </div>
              </section>
              {selected ? (
                <GatewayPanel
                  key={selected.id}
                  proxy={selected}
                  providerKeys={providerKeys.data}
                  appKeys={appKeys.data}
                  providerError={providerKeys.error}
                  appError={appKeys.error}
                  retryProvider={() => providerKeys.refetch()}
                  retryApp={() => appKeys.refetch()}
                  onAddProvider={() =>
                    actions.openProviderDialog(selected.target_provider)
                  }
                  onCreateKey={() => actions.openAppKey(selected)}
                  onDeleteProvider={actions.deleteProvider}
                  onDelete={() => actions.deleteGateway(selected)}
                />
              ) : null}
            </div>
          )}
        </>
      )}

      {actions.dialogs}
    </>
  );
}
