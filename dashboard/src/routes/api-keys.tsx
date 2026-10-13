import { useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import {
  Ban,
  Gauge,
  Globe2,
  KeyRound,
  Plus,
  Search,
  Server,
  Settings2,
  ShieldCheck,
  Waypoints,
} from "lucide-react";
import {
  AdminSummaryStrip,
  FieldLabel,
  MonoValue,
  SecurityNotice,
} from "@/components/admin/admin-primitives";
import { EmptyState, PageHeader } from "@/components/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { api } from "@/api/client";
import type { APIKey, Proxy } from "@/api/client";
import { CopyButton } from "@/components/gateways/primitives";
import { EnvironmentPicker } from "@/components/gateways/environment-picker";
import { cn } from "@/lib/utils";

type KeyFilter = "active" | "revoked" | "all";

function formatDate(value?: string | null) {
  if (!value) return "Never";
  return new Intl.DateTimeFormat(undefined, { dateStyle: "medium" }).format(
    new Date(value),
  );
}

export function ApiKeysPage() {
  const queryClient = useQueryClient();
  const [createOpen, setCreateOpen] = useState(false);
  const [editingKey, setEditingKey] = useState<APIKey | null>(null);
  const [revokeTarget, setRevokeTarget] = useState<APIKey | null>(null);
  const [keyName, setKeyName] = useState("");
  const [selectedProxyId, setSelectedProxyId] = useState("global");
  const [rateLimitRpm, setRateLimitRpm] = useState("");
  const [selectedEnvironment, setSelectedEnvironment] = useState("");
  const [allowEnvironmentOverride, setAllowEnvironmentOverride] =
    useState(false);
  const [editProxyId, setEditProxyId] = useState("global");
  const [editRateLimit, setEditRateLimit] = useState("");
  const [editEnvironment, setEditEnvironment] = useState("");
  const [editAllowEnvironmentOverride, setEditAllowEnvironmentOverride] =
    useState(false);
  const [revealedSecretKey, setRevealedSecretKey] = useState<string | null>(
    null,
  );
  const [filter, setFilter] = useState<KeyFilter>("active");
  const [search, setSearch] = useState("");

  const {
    data: keys,
    isLoading,
    error: keysError,
    refetch: reloadKeys,
  } = useQuery({ queryKey: ["api-keys"], queryFn: api.apiKeys.list });
  const { data: proxies } = useQuery({
    queryKey: ["proxies"],
    queryFn: api.proxies.list,
  });
  const { data: environments = [] } = useQuery({
    queryKey: ["environments"],
    queryFn: api.environments.list,
  });

  const createMutation = useMutation({
    mutationFn: (data: {
      name: string;
      proxy_id?: string;
      rate_limit_rpm?: number;
      environment: string;
      allow_environment_override: boolean;
    }) => api.apiKeys.create(data),
    onSuccess: (res: { key?: string }) => {
      queryClient.invalidateQueries({ queryKey: ["api-keys"] });
      setCreateOpen(false);
      setKeyName("");
      setSelectedProxyId("global");
      setRateLimitRpm("");
      setSelectedEnvironment("");
      setAllowEnvironmentOverride(false);
      if (res.key) setRevealedSecretKey(res.key);
    },
  });

  const updateMutation = useMutation({
    mutationFn: (data: {
      id: string;
      proxy_id: string;
      rate_limit_rpm?: number;
      environment: string;
      allow_environment_override: boolean;
    }) =>
      api.apiKeys.update(data.id, {
        proxy_id: data.proxy_id,
        rate_limit_rpm: data.rate_limit_rpm,
        environment: data.environment,
        allow_environment_override: data.allow_environment_override,
      }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["api-keys"] });
      setEditingKey(null);
    },
  });

  const revokeMutation = useMutation({
    mutationFn: api.apiKeys.revoke,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["api-keys"] });
      setRevokeTarget(null);
    },
  });

  const activeCount = keys?.filter((key: APIKey) => key.is_active).length ?? 0;
  const revokedCount = (keys?.length ?? 0) - activeCount;
  const globalCount =
    keys?.filter(
      (key: APIKey) => key.is_active && getScopeDetails(key, proxies).isGlobal,
    ).length ?? 0;
  const environmentBoundCount =
    keys?.filter((key: APIKey) => key.is_active && key.environment).length ?? 0;

  const visibleKeys = useMemo(() => {
    const term = search.trim().toLowerCase();
    return (keys ?? []).filter((key: APIKey) => {
      if (filter === "active" && !key.is_active) return false;
      if (filter === "revoked" && key.is_active) return false;
      if (!term) return true;
      const scope = getScopeDetails(key, proxies).label;
      return `${key.name} ${key.key_prefix} ${scope}`
        .toLowerCase()
        .includes(term);
    });
  }, [filter, keys, proxies, search]);

  const openEditor = (key: APIKey) => {
    const scope = getScopeDetails(key, proxies);
    updateMutation.reset();
    setEditingKey(key);
    setEditProxyId(scope.proxyId ?? "global");
    setEditRateLimit(key.rate_limit_rpm?.toString() ?? "");
    setEditEnvironment(key.environment);
    setEditAllowEnvironmentOverride(key.allow_environment_override);
  };

  const openCreate = () => {
    createMutation.reset();
    setSelectedProxyId(proxies?.[0]?.id ?? "global");
    setCreateOpen(true);
  };
  const invalidEditRate =
    editRateLimit !== "" &&
    (!Number.isInteger(Number(editRateLimit)) || Number(editRateLimit) < 1);

  return (
    <>
      <PageHeader
        className="flex-col sm:flex-row"
        title="Bastio API Keys"
        description="Give each application its own key to access Bastio. Set its gateway, environment, and request limit."
        action={
          <Button className="cursor-pointer" onClick={openCreate}>
            <Plus data-icon="inline-start" /> Create API key
          </Button>
        }
      />

      <SecurityNotice
        title="These are the keys your applications use"
        className="mb-5"
        action={
          <Button
            nativeButton={false}
            variant="outline"
            render={<Link to="/proxies" search={{ tab: "providers" }} />}
          >
            Provider keys
          </Button>
        }
      >
        A Bastio API key authenticates your application to Bastio. Store keys
        from OpenAI, Claude, or other providers under LLM Gateways → Provider
        keys.
      </SecurityNotice>

      <AdminSummaryStrip
        items={[
          {
            label: "Active credentials",
            value: activeCount,
            detail: `${revokedCount} revoked`,
            tone: "success",
          },
          {
            label: "Environment-bound",
            value: environmentBoundCount,
            detail: `${environments.length} managed boundaries`,
          },
          {
            label: "Global access",
            value: globalCount,
            detail: "Developer APIs + gateways",
            tone: globalCount > 0 ? "warning" : "default",
          },
          {
            label: "Gateway-restricted",
            value: activeCount - globalCount,
            detail: "Access to specific gateways",
          },
        ]}
      />

      <SecurityNotice
        title="Least privilege is the safest default"
        tone={globalCount > 0 ? "warning" : activeCount ? "success" : "info"}
        className="mb-5"
      >
        {activeCount === 0
          ? "Create a key for each application. Restrict gateway applications to a single gateway."
          : globalCount > 0
            ? `${globalCount} active ${globalCount === 1 ? "credential has" : "credentials have"} global access. Bind production keys to a single gateway whenever possible and set an explicit request limit.`
            : "All active credentials are restricted to a specific gateway. Each credential also carries its environment boundary automatically."}
      </SecurityNotice>

      <section className="overflow-hidden rounded-xl border border-border/70 bg-card">
        <div className="flex flex-col gap-3 border-b border-border/60 p-3 sm:flex-row sm:items-center sm:justify-between">
          <div className="flex items-center gap-1 rounded-lg border border-border/70 bg-muted/30 p-0.5">
            {(["active", "revoked", "all"] as KeyFilter[]).map((item) => (
              <button
                key={item}
                type="button"
                onClick={() => setFilter(item)}
                className={cn(
                  "cursor-pointer rounded-md px-2.5 py-1.5 text-[11px] font-medium capitalize text-muted-foreground transition-colors hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
                  filter === item &&
                    "bg-background text-foreground shadow-sm ring-1 ring-border/70",
                )}
                aria-pressed={filter === item}
              >
                {item}{" "}
                <span className="ml-1 font-mono text-[10px] opacity-70">
                  {item === "active"
                    ? activeCount
                    : item === "revoked"
                      ? revokedCount
                      : (keys?.length ?? 0)}
                </span>
              </button>
            ))}
          </div>
          <div className="relative w-full sm:w-64">
            <Search className="pointer-events-none absolute left-2.5 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground" />
            <Input
              aria-label="Search Bastio API keys"
              value={search}
              onChange={(event) => setSearch(event.target.value)}
              placeholder="Search keys…"
              className="h-8 pl-8 text-xs"
            />
          </div>
        </div>

        {isLoading ? (
          <div className="flex items-center justify-center py-16 text-xs text-muted-foreground">
            Loading credentials…
          </div>
        ) : keysError ? (
          <div className="p-5">
            <SecurityNotice title="API keys could not be loaded" tone="warning">
              <p role="alert">{keysError.message}</p>
              <Button
                variant="outline"
                className="mt-3"
                onClick={() => reloadKeys()}
              >
                Retry
              </Button>
            </SecurityNotice>
          </div>
        ) : !keys?.length ? (
          <EmptyState
            icon={<KeyRound className="size-5" />}
            title="No API keys created"
            description="Create a scoped credential for a workload, then store the secret in your secrets manager."
            action={
              <Button size="sm" onClick={openCreate}>
                Create first key
              </Button>
            }
          />
        ) : !visibleKeys.length ? (
          <EmptyState
            icon={<Search className="size-5" />}
            title="No matching credentials"
            description="Change the status filter or search term."
          />
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full min-w-[900px] text-left">
              <thead className="border-b border-border/60 bg-muted/20 text-[10px] uppercase tracking-[0.1em] text-muted-foreground">
                <tr>
                  <th className="px-4 py-2.5 font-medium">Credential</th>
                  <th className="px-4 py-2.5 font-medium">Environment</th>
                  <th className="px-4 py-2.5 font-medium">Access scope</th>
                  <th className="px-4 py-2.5 font-medium">Rate limit</th>
                  <th className="px-4 py-2.5 font-medium">Last used</th>
                  <th className="px-4 py-2.5 font-medium">Created</th>
                  <th className="px-4 py-2.5 text-right font-medium">
                    Controls
                  </th>
                </tr>
              </thead>
              <tbody className="divide-y divide-border/50">
                {visibleKeys.map((key: APIKey) => {
                  const scope = getScopeDetails(key, proxies);
                  return (
                    <tr
                      key={key.id}
                      className="group transition-colors hover:bg-muted/20"
                    >
                      <td className="px-4 py-3">
                        <div className="flex items-center gap-3">
                          <div
                            className={cn(
                              "flex size-8 items-center justify-center rounded-lg border",
                              key.is_active
                                ? "border-success-border bg-success-bg text-success"
                                : "border-border bg-muted/40 text-muted-foreground",
                            )}
                          >
                            <KeyRound className="size-3.5" />
                          </div>
                          <div className="min-w-0">
                            <div className="flex items-center gap-2">
                              <span className="max-w-52 truncate text-[12px] font-medium text-foreground">
                                {key.name || "Unnamed key"}
                              </span>
                              <Badge
                                variant={
                                  key.is_active ? "success" : "secondary"
                                }
                                className="h-4 px-1.5 text-[9px]"
                              >
                                {key.is_active ? "active" : "revoked"}
                              </Badge>
                            </div>
                            <code className="mt-0.5 block font-mono text-[10px] text-muted-foreground">
                              {key.key_prefix}••••••••
                            </code>
                          </div>
                        </div>
                      </td>
                      <td className="px-4 py-3">
                        <div className="flex items-center gap-2 text-[11px]">
                          <Waypoints className="size-3.5 text-muted-foreground" />
                          <span className="font-mono text-foreground">
                            {key.environment || "Unassigned"}
                          </span>
                          {key.allow_environment_override ? (
                            <Badge
                              variant="warning"
                              className="h-4 px-1.5 text-[8px]"
                            >
                              override
                            </Badge>
                          ) : null}
                        </div>
                      </td>
                      <td className="px-4 py-3">
                        <div className="flex items-center gap-2 text-[11px] text-foreground">
                          {scope.isGlobal ? (
                            <Globe2 className="size-3.5 text-warn" />
                          ) : (
                            <Server className="size-3.5 text-muted-foreground" />
                          )}
                          {scope.proxyId ? (
                            <Link
                              to="/proxies"
                              search={{ gateway: scope.proxyId }}
                              className="max-w-52 truncate underline underline-offset-4 hover:text-foreground"
                            >
                              {scope.label}
                            </Link>
                          ) : (
                            <span className="max-w-52 truncate">
                              {scope.label}
                            </span>
                          )}
                        </div>
                      </td>
                      <td className="px-4 py-3 font-mono text-[11px] text-muted-foreground">
                        {key.rate_limit_rpm
                          ? `${key.rate_limit_rpm} rpm`
                          : "Unbounded"}
                      </td>
                      <td className="px-4 py-3 text-[11px] text-muted-foreground">
                        {formatDate(key.last_used_at)}
                      </td>
                      <td className="px-4 py-3 text-[11px] text-muted-foreground">
                        {formatDate(key.created_at)}
                      </td>
                      <td className="px-4 py-3">
                        {key.is_active ? (
                          <div className="flex justify-end gap-1">
                            <Button
                              variant="ghost"
                              size="sm"
                              onClick={() => openEditor(key)}
                            >
                              <Settings2 /> Edit access
                            </Button>
                            <Button
                              variant="ghost"
                              size="sm"
                              className="text-muted-foreground hover:text-destructive"
                              onClick={() => {
                                revokeMutation.reset();
                                setRevokeTarget(key);
                              }}
                            >
                              <Ban /> Revoke
                            </Button>
                          </div>
                        ) : (
                          <div className="text-right text-[10px] text-muted-foreground">
                            No active access
                          </div>
                        )}
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        )}
      </section>

      <Dialog open={createOpen} onOpenChange={setCreateOpen}>
        <DialogContent className="sm:max-w-lg">
          <DialogHeader>
            <DialogTitle>Create a Bastio API key</DialogTitle>
            <DialogDescription>
              Your application uses this key to call Bastio. Restrict it to one
              gateway whenever possible.
            </DialogDescription>
          </DialogHeader>
          <form
            className="space-y-4"
            onSubmit={(event) => {
              event.preventDefault();
              createMutation.mutate({
                name: keyName.trim() || "Developer API key",
                proxy_id: selectedProxyId === "global" ? "" : selectedProxyId,
                rate_limit_rpm: rateLimitRpm
                  ? Number.parseInt(rateLimitRpm, 10)
                  : undefined,
                environment: selectedEnvironment,
                allow_environment_override: allowEnvironmentOverride,
              });
            }}
          >
            <div>
              <FieldLabel htmlFor="bastio-key-name">
                Application key name
              </FieldLabel>
              <Input
                id="bastio-key-name"
                required
                value={keyName}
                onChange={(e) => setKeyName(e.target.value)}
                placeholder="e.g. Support app production"
              />
            </div>
            <EnvironmentBoundaryFields
              value={selectedEnvironment}
              onChange={setSelectedEnvironment}
              allowOverride={allowEnvironmentOverride}
              onAllowOverrideChange={setAllowEnvironmentOverride}
            />
            <div>
              <FieldLabel htmlFor="bastio-key-gateway">
                Gateway access
              </FieldLabel>
              <select
                id="bastio-key-gateway"
                value={selectedProxyId}
                onChange={(e) => setSelectedProxyId(e.target.value)}
                className="h-9 w-full rounded-lg border border-input bg-background px-3 text-xs outline-none focus-visible:ring-3 focus-visible:ring-ring/50"
              >
                <option value="global">
                  Global — all developer APIs and gateways
                </option>
                {proxies?.map((proxy: Proxy) => (
                  <option key={proxy.id} value={proxy.id}>
                    Gateway — {proxy.name}
                  </option>
                ))}
              </select>
              <p className="mt-1.5 text-[10px] leading-relaxed text-muted-foreground">
                {selectedProxyId === "global"
                  ? "Broad access. Use only for trusted platform automation."
                  : "Recommended. The credential can call one gateway only."}
              </p>
            </div>
            <div>
              <FieldLabel htmlFor="bastio-key-rate" optional>
                Requests per minute
              </FieldLabel>
              <div className="relative">
                <Gauge className="absolute left-2.5 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground" />
                <Input
                  id="bastio-key-rate"
                  type="number"
                  min="1"
                  step="1"
                  value={rateLimitRpm}
                  onChange={(e) => setRateLimitRpm(e.target.value)}
                  placeholder="No limit"
                  className="pl-8"
                />
              </div>
            </div>
            {createMutation.isError ? (
              <p role="alert" className="text-[11px] text-destructive">
                {(createMutation.error as Error).message}
              </p>
            ) : null}
            <DialogFooter className="mt-5">
              <Button
                type="button"
                variant="outline"
                onClick={() => setCreateOpen(false)}
              >
                Cancel
              </Button>
              <Button
                type="submit"
                disabled={createMutation.isPending || !selectedEnvironment}
              >
                {createMutation.isPending ? "Generating…" : "Generate key"}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>

      <Dialog
        open={Boolean(revealedSecretKey)}
        onOpenChange={(open) => !open && setRevealedSecretKey(null)}
      >
        <DialogContent className="sm:max-w-lg">
          <DialogHeader>
            <div className="mb-1 flex size-9 items-center justify-center rounded-lg border border-success-border bg-success-bg text-success">
              <ShieldCheck className="size-4" />
            </div>
            <DialogTitle>Store this secret now</DialogTitle>
            <DialogDescription>
              This is the only time Bastio will show the complete credential.
              Put it in a secrets manager before closing.
            </DialogDescription>
          </DialogHeader>
          <div className="flex items-center gap-2 rounded-lg border border-border bg-muted/30 p-2">
            <MonoValue className="min-w-0 flex-1 break-all border-0 bg-transparent">
              {revealedSecretKey}
            </MonoValue>
            <CopyButton
              value={revealedSecretKey ?? ""}
              label="Copy new Bastio API key"
            />
          </div>
          <DialogFooter>
            <Button onClick={() => setRevealedSecretKey(null)}>
              I stored the key
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog
        open={Boolean(editingKey)}
        onOpenChange={(open) => !open && setEditingKey(null)}
      >
        <DialogContent className="sm:max-w-lg">
          <DialogHeader>
            <DialogTitle>Edit access</DialogTitle>
            <DialogDescription>
              {editingKey?.name} · changes apply to new requests immediately.
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-4">
            <EnvironmentBoundaryFields
              value={editEnvironment}
              onChange={setEditEnvironment}
              allowOverride={editAllowEnvironmentOverride}
              onAllowOverrideChange={setEditAllowEnvironmentOverride}
            />
            <div>
              <FieldLabel htmlFor="edit-key-gateway">Gateway access</FieldLabel>
              <select
                id="edit-key-gateway"
                value={editProxyId}
                onChange={(e) => setEditProxyId(e.target.value)}
                className="h-9 w-full rounded-lg border border-input bg-background px-3 text-xs outline-none focus-visible:ring-3 focus-visible:ring-ring/50"
              >
                <option value="global">
                  Global — all developer APIs and gateways
                </option>
                {proxies?.map((proxy: Proxy) => (
                  <option key={proxy.id} value={proxy.id}>
                    Gateway — {proxy.name}
                  </option>
                ))}
              </select>
            </div>
            <div>
              <FieldLabel htmlFor="edit-key-rate" optional>
                Requests per minute
              </FieldLabel>
              <Input
                id="edit-key-rate"
                type="number"
                min="1"
                step="1"
                value={editRateLimit}
                onChange={(e) => setEditRateLimit(e.target.value)}
                placeholder="Keep current limit"
                aria-invalid={invalidEditRate}
              />
              <p className="mt-1.5 text-xs text-muted-foreground">
                Enter a new limit. Leaving this empty keeps the current limit.
              </p>
              {invalidEditRate ? (
                <p role="alert" className="mt-1 text-xs text-destructive">
                  Use a whole number of at least 1.
                </p>
              ) : null}
            </div>
            {updateMutation.isError ? (
              <p role="alert" className="text-sm text-destructive">
                {updateMutation.error.message}
              </p>
            ) : null}
            {editProxyId === "global" ? (
              <SecurityNotice title="This grants broad access" tone="warning">
                Prefer a gateway-scoped key for application workloads.
              </SecurityNotice>
            ) : null}
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setEditingKey(null)}>
              Cancel
            </Button>
            <Button
              disabled={
                updateMutation.isPending || !editEnvironment || invalidEditRate
              }
              onClick={() =>
                editingKey &&
                updateMutation.mutate({
                  id: editingKey.id,
                  proxy_id: editProxyId === "global" ? "" : editProxyId,
                  rate_limit_rpm: editRateLimit
                    ? Number.parseInt(editRateLimit, 10)
                    : undefined,
                  environment: editEnvironment,
                  allow_environment_override: editAllowEnvironmentOverride,
                })
              }
            >
              {updateMutation.isPending ? "Saving…" : "Save changes"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog
        open={Boolean(revokeTarget)}
        onOpenChange={(open) => !open && setRevokeTarget(null)}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Revoke Bastio API key?</DialogTitle>
            <DialogDescription>
              Requests using <strong>{revokeTarget?.name}</strong> will fail
              immediately. This cannot be undone.
            </DialogDescription>
          </DialogHeader>
          {revokeMutation.isError ? (
            <p role="alert" className="text-sm text-destructive">
              {revokeMutation.error.message}
            </p>
          ) : null}
          <DialogFooter>
            <Button variant="outline" onClick={() => setRevokeTarget(null)}>
              Cancel
            </Button>
            <Button
              variant="destructive"
              disabled={revokeMutation.isPending}
              onClick={() =>
                revokeTarget && revokeMutation.mutate(revokeTarget.id)
              }
            >
              {revokeMutation.isPending ? "Revoking…" : "Revoke key"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  );
}

function EnvironmentBoundaryFields({
  value,
  onChange,
  allowOverride,
  onAllowOverrideChange,
}: {
  value: string;
  onChange: (value: string) => void;
  allowOverride: boolean;
  onAllowOverrideChange: (value: boolean) => void;
}) {
  return (
    <div className="space-y-3 rounded-lg border border-border/70 bg-muted/20 p-3">
      <EnvironmentPicker value={value} onChange={onChange} />
      <label className="flex items-start gap-2.5 text-[10px] leading-relaxed text-muted-foreground">
        <input
          type="checkbox"
          checked={allowOverride}
          onChange={(event) => onAllowOverrideChange(event.target.checked)}
          className="mt-0.5 size-3.5 rounded border-input"
        />
        <span>
          <strong className="font-medium text-foreground">
            Allow registered environment override
          </strong>
          <br />
          For shared ingress only. `X-Bastio-Environment` may select another
          environment already registered in this workspace.
        </span>
      </label>
    </div>
  );
}

function getScopeDetails(key: APIKey, proxies?: Proxy[]) {
  const scopes = (key as APIKey & { scopes?: string[] }).scopes ?? [];
  const proxyScope = scopes.find((scope) => scope.startsWith("proxy:"));
  if (proxyScope) {
    const proxyId = proxyScope.replace("proxy:", "");
    const proxy = proxies?.find((item) => item.id === proxyId);
    return {
      isGlobal: false,
      label: proxy?.name ?? `Gateway ${proxyId.slice(0, 8)}`,
      proxyId,
    };
  }
  return { isGlobal: true, label: "Global access", proxyId: null };
}
