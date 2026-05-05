import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { useState } from "react";
import { Server, Plus, X, Trash2, KeyRound, Check, Copy, ChevronDown, ChevronRight, Zap, Pencil } from "lucide-react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { PageHeader, EmptyState } from "@/components/card";
import { api } from "@/api/client";
import type { Proxy, ProviderKey, APIKey, UpdateProxyRequest, CreateProviderKeyRequest } from "@/api/client";

function ProxyCard({ proxy, onDelete, onToggle }: {
  proxy: Proxy;
  onDelete: () => void;
  onToggle: () => void;
}) {
  const queryClient = useQueryClient();
  const [expanded, setExpanded] = useState(false);
  const [showAddKey, setShowAddKey] = useState(false);
  const [keyValue, setKeyValue] = useState("");
  const [showGenApiKey, setShowGenApiKey] = useState(false);
  const [apiKeyName, setApiKeyName] = useState("");
  const [generatedKey, setGeneratedKey] = useState<string | null>(null);
  const [copied, setCopied] = useState(false);
  const [editingModel, setEditingModel] = useState(false);
  const [modelValue, setModelValue] = useState(proxy.target_model);

  const { data: providerKeys } = useQuery({
    queryKey: ["proxy-provider-keys", proxy.id],
    queryFn: () => api.proxies.providerKeys(proxy.id),
    enabled: expanded,
  });

  const createKey = useMutation({
    mutationFn: api.providerKeys.create,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["proxy-provider-keys", proxy.id] });
      queryClient.invalidateQueries({ queryKey: ["provider-keys"] });
      setShowAddKey(false);
      setKeyValue("");
    },
  });

  const deleteProviderKey = useMutation({
    mutationFn: api.providerKeys.delete,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["proxy-provider-keys", proxy.id] });
      queryClient.invalidateQueries({ queryKey: ["provider-keys"] });
    },
  });

  const { data: apiKeys } = useQuery({
    queryKey: ["api-keys"],
    queryFn: api.apiKeys.list,
    enabled: expanded,
  });

  const createApiKey = useMutation({
    mutationFn: api.apiKeys.create,
    onSuccess: (data) => {
      queryClient.invalidateQueries({ queryKey: ["api-keys"] });
      setGeneratedKey(data.key);
      setApiKeyName("");
    },
  });

  const revokeApiKey = useMutation({
    mutationFn: api.apiKeys.revoke,
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["api-keys"] }),
  });

  const updateProxy = useMutation({
    mutationFn: (data: UpdateProxyRequest) => api.proxies.update(proxy.id, data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["proxies"] });
      setEditingModel(false);
    },
  });

  const hasProviderKey = (providerKeys?.length ?? 0) > 0;
  const activeApiKeys = apiKeys?.filter((k: APIKey) => k.is_active) ?? [];

  const handleCopy = () => {
    if (generatedKey) {
      navigator.clipboard.writeText(generatedKey);
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    }
  };

  return (
    <div className="border-b border-border/30 last:border-b-0">
      {/* Proxy header row */}
      <div
        className="flex items-center justify-between px-5 py-4 hover:bg-muted/30 transition-colors cursor-pointer"
        onClick={() => setExpanded(!expanded)}
      >
        <div className="flex items-center gap-3">
          <button className="text-muted-foreground/50">
            {expanded ? <ChevronDown className="h-4 w-4" /> : <ChevronRight className="h-4 w-4" />}
          </button>
          <div>
            <div className="flex items-center gap-2.5">
              <span className="text-[13px] font-medium">{proxy.name}</span>
              <Badge variant={proxy.is_active ? "success" : "secondary"} className="text-[10px] px-1.5 py-0">
                {proxy.is_active ? "active" : "inactive"}
              </Badge>
              {expanded && hasProviderKey && (
                <Badge variant="outline" className="text-[10px] px-1.5 py-0 text-success border-success-border">
                  <KeyRound className="h-2.5 w-2.5 mr-1" /> key configured
                </Badge>
              )}
              {expanded && !hasProviderKey && (
                <Badge variant="outline" className="text-[10px] px-1.5 py-0 text-warn border-warn-border">
                  no provider key
                </Badge>
              )}
            </div>
            <div className="flex items-center gap-2 mt-1">
              <span className="text-xs text-muted-foreground">{proxy.target_provider}</span>
              {proxy.target_model && (
                <>
                  <span className="text-border">/</span>
                  <span className="text-xs text-muted-foreground font-mono">{proxy.target_model}</span>
                </>
              )}
              <span className="text-border">-</span>
              <code className="text-[11px] text-muted-foreground/60 font-mono">{proxy.listen_path}</code>
            </div>
          </div>
        </div>
        <div className="flex items-center gap-2" onClick={(e) => e.stopPropagation()}>
          <Link
            to="/proxies/$id"
            params={{ id: proxy.id }}
            className="text-xs text-muted-foreground hover:text-foreground px-2 py-1"
          >
            Details
          </Link>
          <Button variant="ghost" size="sm" className="h-7 text-xs text-muted-foreground" onClick={onToggle}>
            {proxy.is_active ? "Disable" : "Enable"}
          </Button>
          <Button
            variant="ghost" size="sm" className="h-7 w-7 p-0 text-muted-foreground/50 hover:text-destructive"
            onClick={() => { if (confirm(`Delete proxy "${proxy.name}"?`)) onDelete(); }}
          >
            <Trash2 className="h-3.5 w-3.5" />
          </Button>
        </div>
      </div>

      {/* Expanded panel */}
      {expanded && (
        <div className="px-5 pb-5 pl-12 space-y-4">
          {/* Model Configuration */}
          <div>
            <div className="flex items-center justify-between mb-2">
              <p className="text-xs font-medium text-muted-foreground">Default Model</p>
              {!editingModel && (
                <Button variant="ghost" size="sm" className="h-6 text-[11px] gap-1" onClick={() => { setEditingModel(true); setModelValue(proxy.target_model); }}>
                  <Pencil className="h-3 w-3" /> Edit
                </Button>
              )}
            </div>
            {editingModel ? (
              <div className="flex items-center gap-2">
                <Input
                  placeholder="Model name (e.g., gpt-4o, claude-sonnet-4-20250514)"
                  value={modelValue} onChange={(e) => setModelValue(e.target.value)}
                  className="h-8 text-xs"
                />
                <Button
                  size="sm" className="h-8 text-xs shrink-0"
                  onClick={() => updateProxy.mutate({ name: proxy.name, target_provider: proxy.target_provider as UpdateProxyRequest["target_provider"], target_model: modelValue, is_active: proxy.is_active })}
                  disabled={updateProxy.isPending}
                >
                  {updateProxy.isPending ? "Saving..." : "Save"}
                </Button>
                <Button variant="ghost" size="sm" className="h-8 w-8 p-0 shrink-0" onClick={() => setEditingModel(false)}>
                  <X className="h-3.5 w-3.5" />
                </Button>
              </div>
            ) : (
              <p className="text-[11px] text-muted-foreground/80 py-1 font-mono">
                {proxy.target_model || <span className="text-muted-foreground/40 font-sans italic">No default model — requests must specify a model</span>}
              </p>
            )}
          </div>

          {/* Provider Keys Section */}
          <div>
            <div className="flex items-center justify-between mb-2">
              <p className="text-xs font-medium text-muted-foreground">Provider API Key ({proxy.target_provider})</p>
              {!showAddKey && (
                <Button variant="ghost" size="sm" className="h-6 text-[11px] gap-1" onClick={() => setShowAddKey(true)}>
                  <Plus className="h-3 w-3" /> {hasProviderKey ? "Replace" : "Add Key"}
                </Button>
              )}
            </div>

            {showAddKey && (
              <div className="flex items-center gap-2 mb-3">
                <Input
                  type="password" placeholder={`${proxy.target_provider} API key (sk-...)`}
                  value={keyValue} onChange={(e) => setKeyValue(e.target.value)}
                  className="h-8 text-xs"
                />
                <Button
                  size="sm" className="h-8 text-xs shrink-0"
                  onClick={() => createKey.mutate({ provider: proxy.target_provider as CreateProviderKeyRequest["provider"], key: keyValue, proxy_id: proxy.id })}
                  disabled={!keyValue || createKey.isPending}
                >
                  {createKey.isPending ? "Saving..." : "Save"}
                </Button>
                <Button variant="ghost" size="sm" className="h-8 w-8 p-0 shrink-0" onClick={() => { setShowAddKey(false); setKeyValue(""); }}>
                  <X className="h-3.5 w-3.5" />
                </Button>
              </div>
            )}

            {providerKeys?.map((k: ProviderKey) => (
              <div key={k.id} className="flex items-center justify-between py-1.5 px-3 rounded-lg bg-muted/30 text-xs">
                <div className="flex items-center gap-2">
                  <KeyRound className="h-3 w-3 text-muted-foreground/50" />
                  <code className="text-[11px] text-muted-foreground font-mono">{k.key_masked}</code>
                  {k.is_default && <Badge variant="secondary" className="text-[9px] px-1 py-0">default</Badge>}
                </div>
                <Button
                  variant="ghost" size="sm" className="h-6 w-6 p-0 text-muted-foreground/40 hover:text-destructive"
                  onClick={() => { if (confirm("Delete this provider key?")) deleteProviderKey.mutate(k.id); }}
                >
                  <Trash2 className="h-3 w-3" />
                </Button>
              </div>
            ))}

            {!hasProviderKey && !showAddKey && (
              <p className="text-[11px] text-warn py-1">
                Add your {proxy.target_provider} API key to start proxying requests.
              </p>
            )}
          </div>

          {/* Gateway API Keys */}
          {hasProviderKey && (
            <div>
              <div className="flex items-center justify-between mb-2">
                <p className="text-xs font-medium text-muted-foreground">Gateway API Keys</p>
                {!showGenApiKey && (
                  <Button variant="ghost" size="sm" className="h-6 text-[11px] gap-1" onClick={() => { setShowGenApiKey(true); setGeneratedKey(null); }}>
                    <Zap className="h-3 w-3" /> Generate Key
                  </Button>
                )}
              </div>

              {/* Generate form */}
              {showGenApiKey && !generatedKey && (
                <div className="flex items-center gap-2 mb-3">
                  <Input
                    placeholder="Key name (e.g., my-app)" value={apiKeyName}
                    onChange={(e) => setApiKeyName(e.target.value)} className="h-8 text-xs"
                  />
                  <Button
                    size="sm" className="h-8 text-xs shrink-0"
                    onClick={() => createApiKey.mutate({ name: apiKeyName || `${proxy.name}-key` })}
                    disabled={createApiKey.isPending}
                  >
                    {createApiKey.isPending ? "Generating..." : "Generate"}
                  </Button>
                  <Button variant="ghost" size="sm" className="h-8 w-8 p-0 shrink-0" onClick={() => setShowGenApiKey(false)}>
                    <X className="h-3.5 w-3.5" />
                  </Button>
                </div>
              )}

              {/* Just-generated key banner */}
              {generatedKey && (
                <div className="p-3 rounded-lg bg-success-bg border border-success-border mb-3">
                  <div className="flex items-center gap-2 mb-2">
                    <Check className="h-3.5 w-3.5 text-success" />
                    <span className="text-xs font-medium text-success">API Key Generated</span>
                  </div>
                  <div className="flex items-center gap-2">
                    <code className="flex-1 text-[11px] font-mono bg-muted/50 px-2 py-1.5 rounded border border-border/30 break-all">
                      {generatedKey}
                    </code>
                    <Button variant="outline" size="sm" className="h-7 text-[11px] shrink-0 gap-1" onClick={handleCopy}>
                      {copied ? <Check className="h-3 w-3" /> : <Copy className="h-3 w-3" />}
                      {copied ? "Copied" : "Copy"}
                    </Button>
                  </div>
                  <p className="text-[10px] text-muted-foreground/60 mt-2">
                    Copy this key now — you won't be able to see the full key again.
                  </p>
                </div>
              )}

              {/* Existing API keys list */}
              {activeApiKeys.length > 0 && (
                <div className="space-y-1.5">
                  {activeApiKeys.map((k: APIKey) => (
                    <div key={k.id} className="flex items-center justify-between py-1.5 px-3 rounded-lg bg-muted/30 text-xs">
                      <div className="flex items-center gap-2">
                        <Zap className="h-3 w-3 text-muted-foreground/50" />
                        <span className="text-[11px] font-medium">{k.name || "Unnamed"}</span>
                        <code className="text-[11px] text-muted-foreground font-mono">{k.key_prefix}...</code>
                        <Badge variant="success" className="text-[9px] px-1 py-0">active</Badge>
                      </div>
                      <Button
                        variant="ghost" size="sm" className="h-6 text-[10px] text-muted-foreground/40 hover:text-destructive"
                        onClick={() => { if (confirm(`Revoke key "${k.name}"?`)) revokeApiKey.mutate(k.id); }}
                      >
                        Revoke
                      </Button>
                    </div>
                  ))}
                </div>
              )}

              {activeApiKeys.length === 0 && !generatedKey && !showGenApiKey && (
                <p className="text-[11px] text-muted-foreground/60 py-1">
                  No gateway keys yet. Generate one to authenticate requests.
                </p>
              )}
            </div>
          )}
        </div>
      )}
    </div>
  );
}

export function ProxiesPage() {
  const queryClient = useQueryClient();
  const { data: proxies, isLoading } = useQuery({ queryKey: ["proxies"], queryFn: api.proxies.list });

  const [showCreate, setShowCreate] = useState(false);
  const [name, setName] = useState("");
  const [provider, setProvider] = useState<
    "openai" | "anthropic" | "bedrock" | "vertex" | "azure" | "ollama"
  >("openai");
  const [model, setModel] = useState("");

  const createProxy = useMutation({
    mutationFn: api.proxies.create,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["proxies"] });
      setShowCreate(false);
      setName("");
      setModel("");
    },
  });

  const deleteProxy = useMutation({
    mutationFn: api.proxies.delete,
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["proxies"] }),
  });

  const toggleProxy = useMutation({
    mutationFn: ({ p, active }: { p: Proxy; active: boolean }) =>
      api.proxies.update(p.id, {
        name: p.name,
        target_provider: p.target_provider as UpdateProxyRequest["target_provider"],
        target_model: p.target_model,
        is_active: active,
      }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["proxies"] }),
  });

  return (
    <>
      <PageHeader
        title="Proxies"
        description="Gateway endpoints that route to LLM providers"
        action={
          <Button size="sm" onClick={() => setShowCreate(!showCreate)} className="gap-1.5 text-xs">
            {showCreate ? <X className="h-3.5 w-3.5" /> : <Plus className="h-3.5 w-3.5" />}
            {showCreate ? "Cancel" : "Create Proxy"}
          </Button>
        }
      />

      {showCreate && (
        <Card className="mb-6 border-border/50">
          <CardHeader className="pb-3">
            <CardTitle className="text-sm font-semibold">New Proxy</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="grid grid-cols-1 sm:grid-cols-3 gap-3">
              <Input placeholder="Name" value={name} onChange={(e) => setName(e.target.value)} className="h-9 text-sm" />
              <Select value={provider} onValueChange={(v) => v && setProvider(v as typeof provider)}>
                <SelectTrigger className="h-9 text-sm"><SelectValue /></SelectTrigger>
                <SelectContent>
                  <SelectItem value="openai">OpenAI</SelectItem>
                  <SelectItem value="anthropic">Anthropic</SelectItem>
                  <SelectItem value="bedrock">Bedrock</SelectItem>
                  <SelectItem value="vertex">Vertex AI</SelectItem>
                  <SelectItem value="azure">Azure OpenAI</SelectItem>
                  <SelectItem value="ollama">Ollama</SelectItem>
                </SelectContent>
              </Select>
              <Input placeholder="Default model (e.g., gpt-4o)" value={model} onChange={(e) => setModel(e.target.value)} className="h-9 text-sm" />
            </div>
            <Button
              size="sm" className="mt-3 text-xs"
              onClick={() => createProxy.mutate({ name, target_provider: provider, target_model: model })}
              disabled={!name || createProxy.isPending}
            >
              {createProxy.isPending ? "Creating..." : "Create Proxy"}
            </Button>
            <p className="text-[11px] text-muted-foreground mt-2">
              After creating, expand the proxy to add your provider API key and generate a gateway key.
            </p>
          </CardContent>
        </Card>
      )}

      <Card className="border-border/50">
        <CardContent className="p-0">
          {isLoading ? (
            <div className="flex items-center justify-center py-16">
              <div className="flex items-center gap-2 text-sm text-muted-foreground">
                <div className="h-4 w-4 animate-spin rounded-full border-2 border-muted-foreground/30 border-t-muted-foreground" />
                Loading proxies...
              </div>
            </div>
          ) : !proxies?.length ? (
            <EmptyState
              icon={<Server className="h-6 w-6" />}
              title="No proxies configured"
              description="Create a proxy to route requests through the gateway with security scanning. You'll add your provider API key and generate a gateway key in the next steps."
              action={<Button variant="outline" size="sm" className="text-xs" onClick={() => setShowCreate(true)}>Create First Proxy</Button>}
            />
          ) : (
            proxies.map((p: Proxy) => (
              <ProxyCard
                key={p.id}
                proxy={p}
                onDelete={() => deleteProxy.mutate(p.id)}
                onToggle={() => toggleProxy.mutate({ p, active: !p.is_active })}
              />
            ))
          )}
        </CardContent>
      </Card>
    </>
  );
}
