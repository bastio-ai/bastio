import { useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import {
  Gauge,
  KeyRound,
  Plus,
  Search,
  ShieldCheck,
} from "lucide-react";
import {
  AdminPageHeader,
  AdminSummaryStrip,
  FieldLabel,
  MonoValue,
  SecurityNotice,
} from "@/components/admin/admin-primitives";
import { EmptyState } from "@/components/card";
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
import { ConnectionExample } from "@/components/gateways/connection-example";
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
  const [revealedProxyId, setRevealedProxyId] = useState<string | null>(null);
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
    onSuccess: (res: { key?: string }, variables) => {
      queryClient.invalidateQueries({ queryKey: ["api-keys"] });
      setRevealedProxyId(variables.proxy_id || null);
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

  const revealedProxy =
    proxies?.find((proxy: Proxy) => proxy.id === revealedProxyId) ?? null;
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
    <div className="mx-auto max-w-[1180px] pb-10 pt-2 md:pt-4">
      <AdminPageHeader
        eyebrow="Developer API"
        title="Bastio API Keys"
        description="Give each application its own key to access Bastio. Set its gateway, environment, and request limit."
        bordered={false}
        className="mb-4"
        actions={
          <>
            <Button
              nativeButton={false}
              variant="outline"
              size="sm"
              render={<Link to="/proxies" search={{ tab: "providers" }} />}
            >
              Provider keys
            </Button>
            <Button size="sm" className="cursor-pointer" onClick={openCreate}>
              <Plus data-icon="inline-start" /> Create API key
            </Button>
          </>
        }
      />

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

      <p
        className={cn(
          "mb-6 border-l-2 pl-3 text-[12px] leading-relaxed text-muted-foreground",
          globalCount > 0 ? "border-warn/50" : "border-border-default",
        )}
      >
        <span className="font-medium text-foreground">
          {globalCount > 0
            ? `${globalCount} active ${globalCount === 1 ? "credential has" : "credentials have"} global access.`
            : "Least-privilege application keys."}
        </span>{" "}
        {activeCount === 0
          ? "Create a key for each application and restrict gateway workloads to a single gateway."
          : globalCount > 0
            ? "Bind production keys to a single gateway whenever possible and set an explicit request limit."
            : "All active credentials are restricted to a specific gateway and carry their environment boundary automatically."}{" "}
        Store upstream OpenAI or Anthropic credentials under{" "}
        <Link
          to="/proxies"
          search={{ tab: "providers" }}
          className="text-foreground underline underline-offset-4 hover:text-foreground/80"
        >
          Provider keys
        </Link>
        .
      </p>

      <section aria-label="Credentials">
        <div className="mb-4 flex flex-col gap-3 border-b border-border-subtle sm:flex-row sm:items-end sm:justify-between">
          <nav
            aria-label="Credential status filter"
            className="-mb-px flex gap-5 overflow-x-auto"
          >
            {(
              [
                ["active", "Active", activeCount],
                ["revoked", "Revoked", revokedCount],
                ["all", "All", keys?.length ?? 0],
              ] as const
            ).map(([item, label, count]) => {
              const active = filter === item;
              return (
                <button
                  key={item}
                  type="button"
                  aria-pressed={active}
                  onClick={() => setFilter(item)}
                  className={cn(
                    "inline-flex shrink-0 cursor-pointer items-center gap-1.5 border-b-2 px-0.5 pb-3 text-[12px] transition-colors hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
                    active
                      ? "border-foreground/70 text-foreground"
                      : "border-transparent text-muted-foreground",
                  )}
                >
                  <span>{label}</span>
                  <span className="font-mono text-[11px] tabular-nums text-muted-foreground">
                    {count}
                  </span>
                </button>
              );
            })}
          </nav>

          <search className="relative w-full pb-2.5 sm:w-64">
            <label htmlFor="api-keys-search" className="sr-only">
              Search Bastio API keys
            </label>
            <Search
              aria-hidden="true"
              className="pointer-events-none absolute left-2.5 top-[calc(50%-5px)] size-3.5 -translate-y-1/2 text-muted-foreground"
            />
            <Input
              id="api-keys-search"
              type="search"
              autoCorrect="off"
              autoCapitalize="none"
              spellCheck={false}
              value={search}
              onChange={(event) => setSearch(event.target.value)}
              placeholder="Search keys…"
              className="h-8 pl-8 text-xs"
            />
          </search>
        </div>

        {isLoading ? (
          <div className="flex items-center justify-center border-b border-border-subtle py-16 text-xs text-muted-foreground">
            Loading credentials…
          </div>
        ) : keysError ? (
          <div className="py-4">
            <SecurityNotice title="API keys could not be loaded" tone="warning">
              <p role="alert">{keysError.message}</p>
              <Button
                type="button"
                variant="outline"
                size="sm"
                className="mt-3"
                onClick={() => reloadKeys()}
              >
                Retry
              </Button>
            </SecurityNotice>
          </div>
        ) : !keys?.length ? (
          <div className="border-b border-border-subtle">
            <EmptyState
              icon={<KeyRound className="size-5" />}
              title="No API keys created"
              description="Create a scoped credential for a workload, then store the secret in your secrets manager."
              action={
                <Button type="button" size="sm" onClick={openCreate}>
                  Create first key
                </Button>
              }
            />
          </div>
        ) : !visibleKeys.length ? (
          <div className="border-b border-border-subtle">
            <EmptyState
              icon={<Search className="size-5" />}
              title="No matching credentials"
              description="Change the status filter or search term."
            />
          </div>
        ) : (
          <div
            className="overflow-x-auto border-b border-border-subtle"
            tabIndex={0}
            role="region"
            aria-label="Bastio API keys"
          >
            <table className="w-full min-w-[860px] text-left">
              <thead className="border-b border-border-subtle text-[11px] text-muted-foreground">
                <tr>
                  <th
                    scope="col"
                    className="sticky top-0 bg-background py-2.5 pr-4 font-normal"
                  >
                    Credential
                  </th>
                  <th
                    scope="col"
                    className="sticky top-0 bg-background px-4 py-2.5 font-normal"
                  >
                    Environment
                  </th>
                  <th
                    scope="col"
                    className="sticky top-0 bg-background px-4 py-2.5 font-normal"
                  >
                    Access scope
                  </th>
                  <th
                    scope="col"
                    className="sticky top-0 bg-background px-4 py-2.5 font-normal"
                  >
                    Rate limit
                  </th>
                  <th
                    scope="col"
                    className="sticky top-0 bg-background px-4 py-2.5 font-normal"
                  >
                    Last used
                  </th>
                  <th
                    scope="col"
                    className="sticky top-0 bg-background px-4 py-2.5 font-normal"
                  >
                    Created
                  </th>
                  <th
                    scope="col"
                    className="sticky top-0 bg-background py-2.5 pl-4 text-right font-normal"
                  >
                    Actions
                  </th>
                </tr>
              </thead>
              <tbody className="divide-y divide-border-subtle">
                {visibleKeys.map((key: APIKey) => {
                  const scope = getScopeDetails(key, proxies);
                  const displayName = key.name || "Unnamed key";
                  return (
                    <tr
                      key={key.id}
                      className="transition-colors hover:bg-surface-1/60"
                    >
                      <td className="py-3.5 pr-4">
                        <div className="flex items-start gap-2.5">
                          <span
                            aria-hidden="true"
                            className={cn(
                              "mt-1.5 size-1.5 shrink-0 rounded-full",
                              key.is_active
                                ? "bg-success"
                                : "bg-muted-foreground/40",
                            )}
                          />
                          <div className="min-w-0">
                            <div className="flex items-center gap-2">
                              <span className="max-w-52 truncate text-[13px] font-medium text-foreground">
                                {displayName}
                              </span>
                              {!key.is_active ? (
                                <span className="text-[11px] text-muted-foreground">
                                  Revoked
                                </span>
                              ) : null}
                            </div>
                            <code className="mt-0.5 block font-mono text-[11px] text-muted-foreground">
                              {key.key_prefix}••••••••
                            </code>
                          </div>
                        </div>
                      </td>
                      <td className="px-4 py-3.5">
                        <div className="flex items-center gap-2 text-[12px]">
                          <span className="font-mono text-foreground">
                            {key.environment || "Unassigned"}
                          </span>
                          {key.allow_environment_override ? (
                            <Badge
                              variant="outline"
                              className="h-4 px-1.5 text-[10px] font-normal text-muted-foreground"
                            >
                              override
                            </Badge>
                          ) : null}
                        </div>
                      </td>
                      <td className="px-4 py-3.5 text-[12px]">
                        {scope.proxyId ? (
                          <Link
                            to="/proxies"
                            search={{ gateway: scope.proxyId }}
                            className="inline-block max-w-52 truncate text-foreground underline decoration-border-default underline-offset-4 hover:decoration-foreground"
                          >
                            {scope.label}
                          </Link>
                        ) : (
                          <span className="inline-flex items-center gap-1.5 text-muted-foreground">
                            <span
                              aria-hidden="true"
                              className="size-1.5 rounded-full bg-warn"
                            />
                            <span className="max-w-52 truncate text-foreground">
                              {scope.label}
                            </span>
                          </span>
                        )}
                      </td>
                      <td className="px-4 py-3.5 font-mono text-[12px] tabular-nums text-muted-foreground">
                        {key.rate_limit_rpm
                          ? `${key.rate_limit_rpm} rpm`
                          : "Unbounded"}
                      </td>
                      <td className="px-4 py-3.5 text-[12px] tabular-nums text-muted-foreground">
                        {formatDate(key.last_used_at)}
                      </td>
                      <td className="px-4 py-3.5 text-[12px] tabular-nums text-muted-foreground">
                        {formatDate(key.created_at)}
                      </td>
                      <td className="py-3.5 pl-4">
                        {key.is_active ? (
                          <div className="flex justify-end gap-1">
                            <Button
                              type="button"
                              variant="ghost"
                              size="sm"
                              aria-label={`Edit access for ${displayName}`}
                              onClick={() => openEditor(key)}
                            >
                              Edit access
                            </Button>
                            <Button
                              type="button"
                              variant="ghost"
                              size="sm"
                              aria-label={`Revoke ${displayName}`}
                              className="text-muted-foreground hover:text-destructive"
                              onClick={() => {
                                revokeMutation.reset();
                                setRevokeTarget(key);
                              }}
                            >
                              Revoke
                            </Button>
                          </div>
                        ) : (
                          <div className="text-right text-[11px] text-muted-foreground">
                            Revoked
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
                aria-describedby="bastio-key-gateway-hint"
                className="h-9 w-full rounded-md border border-input bg-background px-3 text-xs outline-none focus-visible:ring-2 focus-visible:ring-ring"
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
              <p
                id="bastio-key-gateway-hint"
                className="mt-1.5 text-[11px] leading-relaxed text-muted-foreground"
              >
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
                <Gauge
                  aria-hidden="true"
                  className="pointer-events-none absolute left-2.5 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground"
                />
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
            <div className="mb-1 flex size-8 items-center justify-center rounded-md border border-success-border bg-success-bg text-success">
              <ShieldCheck aria-hidden="true" className="size-4" />
            </div>
            <DialogTitle>Store this secret now</DialogTitle>
            <DialogDescription>
              This is the only time Bastio will show the complete credential.
              Put it in a secrets manager before closing.
            </DialogDescription>
          </DialogHeader>
          <div className="flex items-center gap-2 rounded-md border border-border-subtle bg-surface-1/60 p-2">
            <MonoValue className="min-w-0 flex-1 break-all border-0 bg-transparent">
              {revealedSecretKey}
            </MonoValue>
            <CopyButton
              value={revealedSecretKey ?? ""}
              label="Copy new Bastio API key"
            />
          </div>
          {revealedSecretKey && revealedProxy ? (
            <section aria-label="Send a test request" className="min-w-0">
              <p className="mb-2 text-xs text-muted-foreground">
                Run this to send your first request through{" "}
                {revealedProxy.name}. It contains the key — don’t commit it.
              </p>
              <ConnectionExample
                proxy={revealedProxy}
                apiKey={revealedSecretKey}
                showTest
              />
            </section>
          ) : null}
          <DialogFooter>
            <Button
              type="button"
              onClick={() => setRevealedSecretKey(null)}
            >
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
                className="h-9 w-full rounded-md border border-input bg-background px-3 text-xs outline-none focus-visible:ring-2 focus-visible:ring-ring"
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
                aria-describedby={
                  invalidEditRate
                    ? "edit-key-rate-hint edit-key-rate-error"
                    : "edit-key-rate-hint"
                }
              />
              <p
                id="edit-key-rate-hint"
                className="mt-1.5 text-xs text-muted-foreground"
              >
                Enter a new limit. Leaving this empty keeps the current limit.
              </p>
              {invalidEditRate ? (
                <p
                  id="edit-key-rate-error"
                  role="alert"
                  className="mt-1 text-xs text-destructive"
                >
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
            <Button
              type="button"
              variant="outline"
              onClick={() => setEditingKey(null)}
            >
              Cancel
            </Button>
            <Button
              type="button"
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
            <Button
              type="button"
              variant="outline"
              onClick={() => setRevokeTarget(null)}
            >
              Cancel
            </Button>
            <Button
              type="button"
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
    </div>
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
    <div className="space-y-3 border-y border-border-subtle py-3.5">
      <EnvironmentPicker value={value} onChange={onChange} />
      <label className="flex items-start gap-2.5 text-[11px] leading-relaxed text-muted-foreground">
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
          For shared ingress only.{" "}
          <code className="font-mono text-foreground">X-Bastio-Environment</code>{" "}
          may select another environment already registered in this workspace.
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
