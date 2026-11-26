import { useState } from "react";
import { Link, useNavigate, useSearch } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import { KeyRound, Plus, Search, Server } from "lucide-react";
import {
  AdminPageHeader,
  AdminUnderlineNav,
  FieldLabel,
} from "@/components/admin/admin-primitives";
import { EmptyState } from "@/components/card";
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
  controlClass,
} from "@/components/gateways/primitives";
import { cn } from "@/lib/utils";
import { useGatewayExtension } from "@/components/gateways/extension-context";

export function ProxiesPage() {
  const extension = useGatewayExtension();
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

  const activeTab = search.tab === "providers" ? "providers" : "gateways";

  return (
    <div className="mx-auto max-w-[1180px] pb-10 pt-2 md:pt-4">
      <AdminPageHeader
        eyebrow="Developer API"
        title="LLM Gateways"
        description="Connect your application to an AI provider through Bastio’s security controls."
        bordered={false}
        className="mb-4"
        actions={
          <div className="flex flex-wrap gap-2">
            <Button
              nativeButton={false}
              size="sm"
              variant="outline"
              className="cursor-pointer"
              render={<Link to="/api-keys" />}
            >
              <KeyRound /> Bastio API keys
            </Button>
            <Button
              type="button"
              size="sm"
              className="cursor-pointer"
              onClick={actions.openCreate}
            >
              <Plus /> Create gateway
            </Button>
          </div>
        }
      />

      {extension.accountNotice}

      <AdminUnderlineNav
        ariaLabel="Gateway management"
        value={activeTab}
        onChange={(next) =>
          void navigate({
            to: "/proxies",
            search:
              next === "providers"
                ? { gateway: selected?.id, tab: "providers" }
                : { gateway: selected?.id },
          })
        }
        items={[
          {
            id: "gateways",
            label: "Gateways",
            count: proxies.data?.length ?? "—",
          },
          {
            id: "providers",
            label: "Provider keys",
            count: providerKeys.data?.length ?? "—",
          },
        ]}
      />

      {activeTab === "providers" ? (
        <section aria-label="Provider keys" className="space-y-5">
          <div className="flex flex-wrap items-start justify-between gap-4">
            <div>
              <h2 className="text-[15px] font-medium tracking-tight text-foreground">
                Keys from your AI providers
              </h2>
              <p className="mt-1 max-w-2xl text-[12px] leading-relaxed text-muted-foreground">
                Bastio uses these secrets to call OpenAI, Claude, OpenRouter,
                and other providers. Your applications use a separate Bastio API
                key.
              </p>
            </div>
            <Button
              type="button"
              size="sm"
              className="cursor-pointer"
              onClick={() =>
                actions.openProviderDialog("openai", {
                  thenCreateGateway: proxies.data?.length === 0,
                })
              }
              disabled={extension.providerKeyManagement !== "allowed"}
            >
              <Plus /> Add provider key
            </Button>
          </div>

          <p className="border-l-2 border-border-default pl-3 text-[12px] leading-relaxed text-muted-foreground">
            <span className="font-medium text-foreground">
              Shared within your workspace.
            </span>{" "}
            New provider keys are saved as shared defaults; the newest key is
            used unless a gateway has its own provider key. Adding a key does
            not remove an existing one.
          </p>

          {providerKeys.isPending ? (
            <p className="py-8 text-xs text-muted-foreground">
              Loading provider keys…
            </p>
          ) : providerKeys.isError ? (
            <QueryError
              title="Provider keys could not be loaded"
              error={providerKeys.error}
              retry={() => providerKeys.refetch()}
            />
          ) : !providerKeys.data.length ? (
            <div className="border-y border-border-subtle">
              <EmptyState
                icon={<KeyRound className="size-5" />}
                title="No provider keys yet"
                description="Add the secret from your provider’s account to connect Bastio to its models."
                action={
                  <Button
                    type="button"
                    size="sm"
                    disabled={extension.providerKeyManagement !== "allowed"}
                    onClick={() =>
                      actions.openProviderDialog("openai", {
                        thenCreateGateway: proxies.data?.length === 0,
                      })
                    }
                  >
                    Add first provider key
                  </Button>
                }
              />
            </div>
          ) : (
            <ProviderKeyList
              keys={providerKeys.data}
              onDelete={actions.deleteProvider}
            />
          )}
        </section>
      ) : (
        <>
          {!proxies.data?.length ? (
            <ol
              className="mb-6 grid divide-y divide-border-subtle border-y border-border-subtle sm:grid-cols-3 sm:divide-x sm:divide-y-0"
              aria-label="Gateway setup steps"
            >
              {[
                ["Choose a provider", "Create a gateway for your application."],
                [
                  "Add the provider key",
                  "Bastio uses it to call the AI provider.",
                ],
                [
                  "Connect your app",
                  "Use a Bastio API key and the gateway URL.",
                ],
              ].map(([title, description], i) => (
                <li key={title} className="flex gap-3 py-4 sm:px-4 first:sm:pl-0">
                  <span className="flex size-6 shrink-0 items-center justify-center rounded-full border border-border-default font-mono text-[11px] text-muted-foreground">
                    {i + 1}
                  </span>
                  <div>
                    <p className="text-[13px] font-medium text-foreground">
                      {title}
                    </p>
                    <p className="mt-0.5 text-[12px] leading-relaxed text-muted-foreground">
                      {description}
                    </p>
                  </div>
                </li>
              ))}
            </ol>
          ) : null}

          {proxies.isPending ? (
            <p className="py-12 text-center text-xs text-muted-foreground">
              Loading gateways…
            </p>
          ) : proxies.isError ? (
            <QueryError
              title="Gateways could not be loaded"
              error={proxies.error}
              retry={() => proxies.refetch()}
            />
          ) : !proxies.data.length ? (
            <div className="border-y border-border-subtle">
              <EmptyState
                icon={<Server className="size-5" />}
                title="Connect your first application"
                description="Create a gateway, add your provider key, and copy the connection details into your app."
                action={
                  <Button type="button" size="sm" onClick={actions.openCreate}>
                    <Plus /> Create your first gateway
                  </Button>
                }
              />
            </div>
          ) : (
            <div className="grid items-start gap-6 lg:grid-cols-[240px_minmax(0,1fr)]">
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
                className="hidden min-w-0 lg:block lg:border-r lg:border-border-subtle lg:pr-5"
                aria-label="Your gateways"
              >
                <search className="mb-3 block">
                  <label htmlFor="gateway-search-input" className="sr-only">
                    Search gateways
                  </label>
                  <div className="relative">
                    <Search
                      aria-hidden="true"
                      className="pointer-events-none absolute left-2.5 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground"
                    />
                    <Input
                      id="gateway-search-input"
                      type="search"
                      value={filter}
                      onChange={(e) => setFilter(e.target.value)}
                      placeholder="Search gateways…"
                      autoCorrect="off"
                      autoCapitalize="none"
                      spellCheck={false}
                      className="h-8 pl-8 text-xs"
                    />
                  </div>
                </search>

                <div className="max-h-[640px] space-y-0.5 overflow-y-auto">
                  {visibleProxies.length ? (
                    visibleProxies.map((p) => {
                      const status = gatewaySetupStatus(
                        p,
                        providerKeys.data,
                        appKeys.data,
                        extension.managedProviders,
                      );
                      const isSelected = selected?.id === p.id;
                      const isReady = status === "Setup complete";
                      return (
                        <button
                          type="button"
                          key={p.id}
                          aria-current={isSelected ? "page" : undefined}
                          onClick={() => selectGateway(p.id)}
                          className={cn(
                            "flex w-full cursor-pointer items-start gap-2.5 rounded-md px-2.5 py-2.5 text-left transition-colors hover:bg-surface-2/60 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring",
                            isSelected && "bg-surface-2 text-foreground",
                          )}
                        >
                          <span
                            aria-hidden="true"
                            className={cn(
                              "mt-1.5 size-1.5 shrink-0 rounded-full",
                              !p.is_active
                                ? "bg-muted-foreground/40"
                                : isReady
                                  ? "bg-success"
                                  : "bg-warn",
                            )}
                          />
                          <span className="min-w-0 flex-1">
                            <span className="block truncate text-[13px] font-medium text-foreground">
                              {p.name}
                            </span>
                            <span className="mt-0.5 block truncate text-[11px] text-muted-foreground">
                              {providerLabels[p.target_provider] ??
                                p.target_provider}{" "}
                              ·{" "}
                              {providerKeys.isError || appKeys.isError
                                ? "Setup unavailable"
                                : status === "Add provider key" &&
                                    extension.providerKeyManagement ===
                                      "restricted"
                                  ? "Provider unavailable"
                                  : status}
                            </span>
                          </span>
                        </button>
                      );
                    })
                  ) : (
                    <p className="px-2.5 py-4 text-xs text-muted-foreground">
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
    </div>
  );
}
