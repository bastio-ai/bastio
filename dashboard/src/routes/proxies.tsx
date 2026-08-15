import { useState } from "react";
import { Link } from "@tanstack/react-router";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  ArrowUpRight,
  Check,
  ChevronDown,
  Copy,
  KeyRound,
  LockKeyhole,
  Pencil,
  Plus,
  Route,
  Server,
  Trash2,
  X,
} from "lucide-react";
import { AdminSummaryStrip, FieldLabel, MonoValue, SecurityNotice } from "@/components/admin/admin-primitives";
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
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { api } from "@/api/client";
import type { APIKey, CreateProviderKeyRequest, ProviderKey, Proxy, UpdateProxyRequest } from "@/api/client";
import { cn } from "@/lib/utils";

const providerLabels: Record<string, string> = {
  openai: "OpenAI",
  anthropic: "Anthropic",
  deepseek: "DeepSeek (V3 & R1)",
  groq: "Groq LPU (Ultra-low latency)",
  google: "Google Gemini / Vertex",
  ollama: "Ollama (Self-hosted / Local)",
  bedrock: "Amazon Bedrock",
  vertex: "Vertex AI",
  azure: "Azure OpenAI",
};

function GatewayRecord({ proxy, onDelete, onToggle }: { proxy: Proxy; onDelete: () => void; onToggle: () => void }) {
  const queryClient = useQueryClient();
  const [expanded, setExpanded] = useState(false);
  const [showProviderKey, setShowProviderKey] = useState(false);
  const [providerKey, setProviderKey] = useState("");
  const [showGatewayKey, setShowGatewayKey] = useState(false);
  const [gatewayKeyName, setGatewayKeyName] = useState("");
  const [gatewayEnvironment, setGatewayEnvironment] = useState("");
  const [generatedKey, setGeneratedKey] = useState<string | null>(null);
  const [copied, setCopied] = useState<string | null>(null);
  const [editingModel, setEditingModel] = useState(false);
  const [modelValue, setModelValue] = useState(proxy.target_model);

  const { data: providerKeys, isLoading: providerKeysLoading } = useQuery({
    queryKey: ["proxy-provider-keys", proxy.id],
    queryFn: () => api.proxies.providerKeys(proxy.id),
    enabled: expanded,
  });
  const { data: apiKeys } = useQuery({ queryKey: ["api-keys"], queryFn: api.apiKeys.list, enabled: expanded });
  const { data: environments = [] } = useQuery({ queryKey: ["environments"], queryFn: api.environments.list, enabled: expanded });

  const refreshProviderKeys = () => {
    queryClient.invalidateQueries({ queryKey: ["proxy-provider-keys", proxy.id] });
    queryClient.invalidateQueries({ queryKey: ["provider-keys"] });
  };

  const createProviderKey = useMutation({
    mutationFn: api.providerKeys.create,
    onSuccess: () => {
      refreshProviderKeys();
      setShowProviderKey(false);
      setProviderKey("");
    },
  });
  const deleteProviderKey = useMutation({ mutationFn: api.providerKeys.delete, onSuccess: refreshProviderKeys });
  const createApiKey = useMutation({
    mutationFn: api.apiKeys.create,
    onSuccess: (data) => {
      queryClient.invalidateQueries({ queryKey: ["api-keys"] });
      setGeneratedKey(data.key);
      setGatewayKeyName("");
      setGatewayEnvironment("");
    },
  });
  const revokeApiKey = useMutation({ mutationFn: api.apiKeys.revoke, onSuccess: () => queryClient.invalidateQueries({ queryKey: ["api-keys"] }) });
  const updateProxy = useMutation({
    mutationFn: (data: UpdateProxyRequest) => api.proxies.update(proxy.id, data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["proxies"] });
      setEditingModel(false);
    },
  });

  const proxyApiKeys = (apiKeys ?? []).filter((key: APIKey) => {
    const scopes = (key as APIKey & { scopes?: string[] }).scopes ?? [];
    return key.is_active && scopes.includes(`proxy:${proxy.id}`);
  });
  const hasProviderKey = Boolean(providerKeys?.length);

  const copyValue = async (value: string, name: string) => {
    await navigator.clipboard.writeText(value);
    setCopied(name);
    window.setTimeout(() => setCopied(null), 1800);
  };

  return (
    <article className="border-b border-border/60 last:border-b-0">
      <div className="flex flex-col gap-3 px-4 py-3.5 transition-colors hover:bg-muted/20 sm:flex-row sm:items-center">
        <button type="button" className="flex min-w-0 flex-1 items-center gap-3 text-left" onClick={() => setExpanded((value) => !value)} aria-expanded={expanded}>
          <div className={cn("flex size-9 shrink-0 items-center justify-center rounded-lg border", proxy.is_active ? "border-success-border bg-success-bg text-success" : "border-border bg-muted/40 text-muted-foreground")}>
            <Route className="size-4" />
          </div>
          <div className="min-w-0 flex-1">
            <div className="flex flex-wrap items-center gap-2">
              <span className="truncate text-[13px] font-medium text-foreground">{proxy.name}</span>
              <Badge variant={proxy.is_active ? "success" : "secondary"} className="h-4 px-1.5 text-[9px]">{proxy.is_active ? "active" : "inactive"}</Badge>
              {expanded && !providerKeysLoading ? <Badge variant={hasProviderKey ? "outline" : "warning"} className="h-4 px-1.5 text-[9px]">{hasProviderKey ? "provider auth ready" : "provider key required"}</Badge> : null}
            </div>
            <div className="mt-1 flex flex-wrap items-center gap-2 text-[10px] text-muted-foreground">
              <span>{providerLabels[proxy.target_provider] ?? proxy.target_provider}</span><span>·</span>
              <code className="font-mono">{proxy.target_model || "request-defined model"}</code><span>·</span>
              <code className="font-mono">{proxy.listen_path}</code>
            </div>
          </div>
          <ChevronDown className={cn("size-4 shrink-0 text-muted-foreground transition-transform", expanded && "rotate-180")} />
        </button>
        <div className="flex items-center justify-end gap-1 sm:pl-3">
          <Button variant="ghost" size="sm" render={<Link to="/proxies/$id" params={{ id: proxy.id }} />}><ArrowUpRight /> Details</Button>
          <Button variant="ghost" size="sm" onClick={onToggle}>{proxy.is_active ? "Disable" : "Enable"}</Button>
          <Button variant="ghost" size="icon-sm" className="text-muted-foreground hover:text-destructive" onClick={onDelete} aria-label={`Delete ${proxy.name}`}><Trash2 /></Button>
        </div>
      </div>

      {expanded ? (
        <div className="border-t border-border/50 bg-muted/10 px-4 py-4 sm:pl-16">
          <div className="grid gap-4 xl:grid-cols-3">
            <section className="rounded-xl border border-border/70 bg-background p-4">
              <div className="flex items-start justify-between gap-3">
                <div><p className="text-[10px] font-medium uppercase tracking-[0.1em] text-muted-foreground">01 · Provider routing</p><h3 className="mt-1 text-[12px] font-medium">Model destination</h3></div>
                <Badge variant="success" className="h-4 px-1.5 text-[9px]">configured</Badge>
              </div>
              <dl className="mt-4 space-y-3 text-[11px]">
                <div className="flex justify-between gap-4"><dt className="text-muted-foreground">Provider</dt><dd>{providerLabels[proxy.target_provider] ?? proxy.target_provider}</dd></div>
                <div className="flex items-start justify-between gap-4"><dt className="text-muted-foreground">Default model</dt><dd className="text-right font-mono">{proxy.target_model || "Not set"}</dd></div>
                <div className="flex items-center justify-between gap-2"><dt className="text-muted-foreground">Endpoint</dt><dd className="flex items-center gap-1"><code className="font-mono">{proxy.listen_path}</code><Button size="icon-xs" variant="ghost" onClick={() => copyValue(proxy.listen_path, "endpoint")} aria-label="Copy endpoint">{copied === "endpoint" ? <Check /> : <Copy />}</Button></dd></div>
              </dl>
              <div className="mt-4 border-t border-border/60 pt-3">
                {editingModel ? (
                  <div className="flex gap-2"><Input value={modelValue} onChange={(e) => setModelValue(e.target.value)} placeholder="Model identifier" className="h-8 text-xs" /><Button size="sm" onClick={() => updateProxy.mutate({ name: proxy.name, target_provider: proxy.target_provider as UpdateProxyRequest["target_provider"], target_model: modelValue, is_active: proxy.is_active })} disabled={updateProxy.isPending}>{updateProxy.isPending ? "Saving…" : "Save"}</Button><Button variant="ghost" size="icon-sm" onClick={() => setEditingModel(false)}><X /></Button></div>
                ) : <Button variant="ghost" size="sm" onClick={() => setEditingModel(true)}><Pencil /> Edit default model</Button>}
              </div>
            </section>

            <section className="rounded-xl border border-border/70 bg-background p-4">
              <div className="flex items-start justify-between gap-3">
                <div><p className="text-[10px] font-medium uppercase tracking-[0.1em] text-muted-foreground">02 · Provider credential</p><h3 className="mt-1 text-[12px] font-medium">Upstream authentication</h3></div>
                <Badge variant={hasProviderKey ? "success" : "warning"} className="h-4 px-1.5 text-[9px]">{hasProviderKey ? "ready" : "action required"}</Badge>
              </div>
              <p className="mt-2 text-[10px] leading-relaxed text-muted-foreground">Encrypted at rest and never returned after submission.</p>
              <div className="mt-4 space-y-2">
                {providerKeysLoading ? <p className="text-[11px] text-muted-foreground">Checking credential…</p> : providerKeys?.map((key: ProviderKey) => (
                  <div key={key.id} className="flex items-center gap-2 rounded-lg border border-border/60 bg-muted/30 px-2.5 py-2">
                    <KeyRound className="size-3.5 text-muted-foreground" /><code className="min-w-0 flex-1 truncate font-mono text-[10px]">{key.key_masked}</code>{key.is_default ? <Badge variant="secondary" className="h-4 px-1 text-[8px]">default</Badge> : null}
                    <Button variant="ghost" size="icon-xs" className="text-muted-foreground hover:text-destructive" onClick={() => window.confirm("Delete this provider credential? Requests may stop working immediately.") && deleteProviderKey.mutate(key.id)}><Trash2 /></Button>
                  </div>
                ))}
                {showProviderKey ? (
                  <div className="space-y-2"><Input autoFocus type="password" value={providerKey} onChange={(e) => setProviderKey(e.target.value)} placeholder={`${proxy.target_provider} secret key`} className="h-8 text-xs" /><div className="flex gap-2"><Button size="sm" disabled={!providerKey || createProviderKey.isPending} onClick={() => createProviderKey.mutate({ provider: proxy.target_provider as CreateProviderKeyRequest["provider"], key: providerKey, proxy_id: proxy.id })}>{createProviderKey.isPending ? "Saving…" : "Store credential"}</Button><Button variant="ghost" size="sm" onClick={() => { setShowProviderKey(false); setProviderKey(""); }}>Cancel</Button></div></div>
                ) : <Button variant="outline" size="sm" className="w-full" onClick={() => setShowProviderKey(true)}><Plus /> {hasProviderKey ? "Replace credential" : "Add provider credential"}</Button>}
              </div>
            </section>

            <section className="rounded-xl border border-border/70 bg-background p-4">
              <div className="flex items-start justify-between gap-3">
                <div><p className="text-[10px] font-medium uppercase tracking-[0.1em] text-muted-foreground">03 · Client authentication</p><h3 className="mt-1 text-[12px] font-medium">Gateway API keys</h3></div>
                <Badge variant={proxyApiKeys.length ? "success" : "outline"} className="h-4 px-1.5 text-[9px]">{proxyApiKeys.length} active</Badge>
              </div>
              <p className="mt-2 text-[10px] leading-relaxed text-muted-foreground">Only keys explicitly bound to this gateway are shown.</p>
              <div className="mt-4 space-y-2">
                {proxyApiKeys.map((key: APIKey) => (
                  <div key={key.id} className="flex items-center gap-2 rounded-lg border border-border/60 bg-muted/30 px-2.5 py-2"><LockKeyhole className="size-3.5 text-success" /><span className="min-w-0 flex-1 truncate text-[10px] font-medium">{key.name}</span><code className="font-mono text-[9px] text-muted-foreground">{key.key_prefix}…</code><Button variant="ghost" size="xs" className="text-muted-foreground hover:text-destructive" onClick={() => window.confirm(`Revoke key “${key.name}”?`) && revokeApiKey.mutate(key.id)}>Revoke</Button></div>
                ))}
                {generatedKey ? (
                  <div className="rounded-lg border border-success-border bg-success-bg p-2.5"><p className="mb-2 text-[10px] font-medium text-success">Copy now — shown once</p><div className="flex gap-2"><MonoValue className="min-w-0 flex-1 truncate">{generatedKey}</MonoValue><Button variant="outline" size="icon-sm" onClick={() => copyValue(generatedKey, "gateway-key")}>{copied === "gateway-key" ? <Check /> : <Copy />}</Button></div></div>
                ) : null}
                {showGatewayKey && !generatedKey ? (
                  <div className="space-y-2">
                    <Input autoFocus value={gatewayKeyName} onChange={(e) => setGatewayKeyName(e.target.value)} placeholder="Credential name" className="h-8 text-xs" />
                    <select value={gatewayEnvironment} onChange={(event) => setGatewayEnvironment(event.target.value)} className="h-8 w-full rounded-md border border-input bg-background px-2.5 text-xs">
                      <option value="" disabled>Select environment</option>
                      {environments.map((environment) => <option key={environment.id} value={environment.name}>{environment.name} — {environment.kind}</option>)}
                    </select>
                    <div className="flex gap-2"><Button size="sm" disabled={createApiKey.isPending || !gatewayEnvironment} onClick={() => createApiKey.mutate({ name: gatewayKeyName.trim() || `${proxy.name}-key`, proxy_id: proxy.id, environment: gatewayEnvironment, allow_environment_override: false })}>{createApiKey.isPending ? "Generating…" : "Generate scoped key"}</Button><Button variant="ghost" size="sm" onClick={() => setShowGatewayKey(false)}>Cancel</Button></div>
                  </div>
                ) : !generatedKey ? <Button variant="outline" size="sm" className="w-full" disabled={!hasProviderKey} onClick={() => setShowGatewayKey(true)}><Plus /> Generate scoped key</Button> : null}
              </div>
            </section>
          </div>
        </div>
      ) : null}
    </article>
  );
}

export function ProxiesPage() {
  const queryClient = useQueryClient();
  const { data: proxies, isLoading } = useQuery({ queryKey: ["proxies"], queryFn: api.proxies.list });
  const [createOpen, setCreateOpen] = useState(false);
  const [deleteTarget, setDeleteTarget] = useState<Proxy | null>(null);
  const [name, setName] = useState("");
  const [provider, setProvider] = useState<string>("openai");
  const [model, setModel] = useState("");
  const [fallbackChain, setFallbackChain] = useState("");

  const createProxy = useMutation({
    mutationFn: api.proxies.create,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["proxies"] });
      setCreateOpen(false); setName(""); setModel(""); setFallbackChain("");
    },
  });
  const deleteProxy = useMutation({ mutationFn: api.proxies.delete, onSuccess: () => { queryClient.invalidateQueries({ queryKey: ["proxies"] }); setDeleteTarget(null); } });
  const toggleProxy = useMutation({
    mutationFn: ({ proxy, active }: { proxy: Proxy; active: boolean }) => api.proxies.update(proxy.id, { name: proxy.name, target_provider: proxy.target_provider as UpdateProxyRequest["target_provider"], target_model: proxy.target_model, is_active: active }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["proxies"] }),
  });

  const activeCount = proxies?.filter((proxy: Proxy) => proxy.is_active).length ?? 0;
  const providers = new Set(proxies?.map((proxy: Proxy) => proxy.target_provider) ?? []).size;

  return (
    <>
      <PageHeader title="LLM Gateways" description="Secure, authenticated endpoints that route application traffic to model providers with multi-provider failover." action={<Button size="sm" onClick={() => setCreateOpen(true)}><Plus /> Create gateway</Button>} />
      <AdminSummaryStrip items={[
        { label: "Gateways", value: proxies?.length ?? 0, detail: "Configured endpoints" },
        { label: "Active", value: activeCount, detail: `${Math.max((proxies?.length ?? 0) - activeCount, 0)} disabled`, tone: "success" },
        { label: "Providers", value: providers, detail: "Upstream integrations" },
        { label: "Security path", value: "3 stages", detail: "Route · provider auth · client auth" },
      ]} />
      <SecurityNotice title="A gateway is ready only when both sides are authenticated" className="mb-5">Open a gateway to verify its upstream provider credential and issue a gateway-scoped client key. Secrets are never displayed again after creation.</SecurityNotice>

      <section className="overflow-hidden rounded-xl border border-border/70 bg-card">
        <div className="flex items-center justify-between border-b border-border/60 px-4 py-3"><div><h2 className="text-[12px] font-medium">Gateway inventory</h2><p className="mt-0.5 text-[10px] text-muted-foreground">Expand a record to review the complete trust path.</p></div><Badge variant="outline" className="font-mono text-[9px]">{proxies?.length ?? 0} total</Badge></div>
        {isLoading ? <div className="py-16 text-center text-xs text-muted-foreground">Loading gateways…</div> : !proxies?.length ? <EmptyState icon={<Server className="size-5" />} title="No gateways configured" description="Create a gateway, store its upstream provider credential, then issue a scoped key to your application." action={<Button size="sm" onClick={() => setCreateOpen(true)}>Create first gateway</Button>} /> : proxies.map((proxy: Proxy) => <GatewayRecord key={proxy.id} proxy={proxy} onDelete={() => setDeleteTarget(proxy)} onToggle={() => toggleProxy.mutate({ proxy, active: !proxy.is_active })} />)}
      </section>

      <Dialog open={createOpen} onOpenChange={setCreateOpen}>
        <DialogContent className="sm:max-w-lg">
          <DialogHeader><DialogTitle>Create LLM gateway</DialogTitle><DialogDescription>Define the route first. Provider and client credentials are configured after creation.</DialogDescription></DialogHeader>
          <div className="space-y-4">
            <div><FieldLabel>Gateway name</FieldLabel><Input value={name} onChange={(e) => setName(e.target.value)} placeholder="production-chat" /></div>
            <div><FieldLabel>Model provider</FieldLabel><Select value={provider} onValueChange={(value) => value && setProvider(value)}><SelectTrigger className="w-full"><SelectValue>{providerLabels[provider]}</SelectValue></SelectTrigger><SelectContent>{Object.entries(providerLabels).map(([value, label]) => <SelectItem key={value} value={value}>{label}</SelectItem>)}</SelectContent></Select></div>
            <div><FieldLabel optional>Default model</FieldLabel><Input value={model} onChange={(e) => setModel(e.target.value)} placeholder="e.g. gpt-4o" /><p className="mt-1.5 text-[10px] text-muted-foreground">Leave blank to require every request to specify a model.</p></div>
            <div><FieldLabel optional>Auto-Failover Model Chain</FieldLabel><Input value={fallbackChain} onChange={(e) => setFallbackChain(e.target.value)} placeholder="e.g. groq/llama-3.3-70b-versatile, openai/gpt-4o-mini" /><p className="mt-1.5 text-[10px] text-muted-foreground">Fallback sequence to retry automatically on 429, 500, or 529 provider outages.</p></div>
            <SecurityNotice title="Credential setup follows" tone="info">After creation, add an upstream provider key and a gateway-scoped client key before sending production traffic.</SecurityNotice>
          </div>
          <DialogFooter><Button variant="outline" onClick={() => setCreateOpen(false)}>Cancel</Button><Button disabled={!name.trim() || createProxy.isPending} onClick={() => createProxy.mutate({ name: name.trim(), target_provider: provider as CreateProviderKeyRequest["provider"], target_model: model.trim() })}>{createProxy.isPending ? "Creating…" : "Create gateway"}</Button></DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog open={Boolean(deleteTarget)} onOpenChange={(open) => !open && setDeleteTarget(null)}>
        <DialogContent><DialogHeader><DialogTitle>Delete gateway?</DialogTitle><DialogDescription>The <strong>{deleteTarget?.name}</strong> endpoint and its bindings will be removed. Active application requests to this route will fail.</DialogDescription></DialogHeader><DialogFooter><Button variant="outline" onClick={() => setDeleteTarget(null)}>Cancel</Button><Button variant="destructive" disabled={deleteProxy.isPending} onClick={() => deleteTarget && deleteProxy.mutate(deleteTarget.id)}>{deleteProxy.isPending ? "Deleting…" : "Delete gateway"}</Button></DialogFooter></DialogContent>
      </Dialog>
    </>
  );
}
