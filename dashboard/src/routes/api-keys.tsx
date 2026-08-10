import { useState } from "react";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { Key, Ban, Plus, Copy, Check, Server, Globe, Settings2, Sparkles } from "lucide-react";
import { Card, CardContent } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { PageHeader, EmptyState } from "@/components/card";
import { api } from "@/api/client";
import type { APIKey, Proxy } from "@/api/client";

export function ApiKeysPage() {
  const queryClient = useQueryClient();
  const [showCreateModal, setShowCreateModal] = useState(false);
  const [editingKey, setEditingKey] = useState<APIKey | null>(null);
  
  // Create Form State
  const [keyName, setKeyName] = useState("");
  const [selectedProxyId, setSelectedProxyId] = useState<string>("global");
  const [rateLimitRpm, setRateLimitRpm] = useState<string>("");

  // Secret Reveal Modal State
  const [revealedSecretKey, setRevealedSecretKey] = useState<string | null>(null);
  const [copied, setCopied] = useState(false);

  const { data: keys, isLoading } = useQuery({
    queryKey: ["api-keys"],
    queryFn: api.apiKeys.list,
  });

  const { data: proxies } = useQuery({
    queryKey: ["proxies"],
    queryFn: api.proxies.list,
  });

  const createMutation = useMutation({
    mutationFn: (data: { name: string; proxy_id?: string; rate_limit_rpm?: number }) =>
      api.apiKeys.create(data),
    onSuccess: (res: any) => {
      queryClient.invalidateQueries({ queryKey: ["api-keys"] });
      setShowCreateModal(false);
      setKeyName("");
      setSelectedProxyId("global");
      setRateLimitRpm("");
      if (res?.key) {
        setRevealedSecretKey(res.key);
      }
    },
  });

  const updateMutation = useMutation({
    mutationFn: (data: { id: string; proxy_id: string; rate_limit_rpm?: number }) =>
      api.apiKeys.update(data.id, { proxy_id: data.proxy_id, rate_limit_rpm: data.rate_limit_rpm }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["api-keys"] });
      setEditingKey(null);
    },
  });

  const revokeMutation = useMutation({
    mutationFn: api.apiKeys.revoke,
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["api-keys"] }),
  });

  const handleCopySecret = (key: string) => {
    navigator.clipboard.writeText(key);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  };

  const handleCreateSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    createMutation.mutate({
      name: keyName.trim() || "Developer API Key",
      proxy_id: selectedProxyId === "global" ? "" : selectedProxyId,
      rate_limit_rpm: rateLimitRpm ? parseInt(rateLimitRpm, 10) : undefined,
    });
  };

  const activeCount = keys?.filter((k: APIKey) => k.is_active).length ?? 0;
  const revokedCount = (keys?.length ?? 0) - activeCount;

  // Helper to determine key scope label & icon
  const getScopeDetails = (k: APIKey) => {
    const scopes = (k as any).scopes || [];
    const proxyScope = scopes.find((s: string) => s.startsWith("proxy:"));
    
    if (proxyScope) {
      const pId = proxyScope.replace("proxy:", "");
      const matchedProxy = proxies?.find((p: Proxy) => p.id === pId);
      return {
        isGlobal: false,
        label: matchedProxy ? `Proxy: ${matchedProxy.name}` : `Proxy Bound (${pId.slice(0, 8)})`,
        proxyId: pId,
      };
    }
    return {
      isGlobal: true,
      label: "Global Access (All Developer APIs & Proxies)",
      proxyId: null,
    };
  };

  return (
    <>
      <PageHeader
        title="API Keys"
        description="Generate secret keys for standalone Developer APIs (/v1/detect, /v1/pii/mask) or proxy endpoints."
        badge={
          keys && keys.length > 0 ? (
            <Badge variant="outline" className="text-[11px] font-normal">
              {activeCount} active{revokedCount > 0 ? ` · ${revokedCount} revoked` : ""}
            </Badge>
          ) : undefined
        }
        action={
          <Button
            onClick={() => setShowCreateModal(true)}
            size="sm"
            className="gap-2 font-medium"
          >
            <Plus className="h-4 w-4" />
            Create API Key
          </Button>
        }
      />

      {/* Secret Key Reveal Dialog */}
      {revealedSecretKey && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-sm p-4 animate-in fade-in duration-200">
          <div className="w-full max-w-lg rounded-2xl border border-emerald-500/40 bg-card p-6 shadow-2xl space-y-5">
            <div className="flex items-center gap-3 text-emerald-500">
              <div className="p-2 rounded-xl bg-emerald-500/10 border border-emerald-500/20">
                <Sparkles className="h-5 w-5" />
              </div>
              <div>
                <h3 className="font-semibold text-foreground text-base">API Key Generated</h3>
                <p className="text-xs text-muted-foreground">Copy your secret key now. You will not be able to see it again!</p>
              </div>
            </div>

            <div className="space-y-2">
              <label className="text-xs font-mono text-muted-foreground uppercase tracking-wider">Secret Key</label>
              <div className="flex items-center gap-2 p-3 rounded-xl bg-muted/70 border border-border font-mono text-xs text-foreground select-all break-all">
                <span>{revealedSecretKey}</span>
                <Button
                  size="icon"
                  variant="ghost"
                  onClick={() => handleCopySecret(revealedSecretKey)}
                  className="ml-auto h-7 w-7 shrink-0"
                >
                  {copied ? <Check className="h-3.5 w-3.5 text-emerald-500" /> : <Copy className="h-3.5 w-3.5 text-muted-foreground" />}
                </Button>
              </div>
            </div>

            <div className="flex justify-end pt-2">
              <Button onClick={() => setRevealedSecretKey(null)} className="px-6 font-medium">
                Done
              </Button>
            </div>
          </div>
        </div>
      )}

      {/* Create Key Modal */}
      {showCreateModal && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-sm p-4 animate-in fade-in duration-200">
          <div className="w-full max-w-md rounded-2xl border border-border bg-card p-6 shadow-2xl space-y-5">
            <div className="flex items-center justify-between border-b border-border/50 pb-3">
              <div className="flex items-center gap-2">
                <Key className="h-4 w-4 text-primary" />
                <h3 className="font-semibold text-foreground text-base">Create New API Key</h3>
              </div>
              <Button variant="ghost" size="sm" className="h-7 w-7 p-0" onClick={() => setShowCreateModal(false)}>
                ✕
              </Button>
            </div>

            <form onSubmit={handleCreateSubmit} className="space-y-4">
              <div className="space-y-1.5">
                <label className="text-xs font-medium text-foreground">Key Name</label>
                <Input
                  placeholder="e.g. Developer API Production Key"
                  value={keyName}
                  onChange={(e) => setKeyName(e.target.value)}
                  className="h-9 text-sm"
                  required
                />
              </div>

              <div className="space-y-1.5">
                <label className="text-xs font-medium text-foreground">Access Scope & Binding</label>
                <select
                  value={selectedProxyId}
                  onChange={(e) => setSelectedProxyId(e.target.value)}
                  className="w-full h-9 rounded-md border border-input bg-background px-3 text-sm text-foreground shadow-sm focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring"
                >
                  <option value="global">🌐 Global Access (All Developer APIs & Proxies)</option>
                  {proxies && proxies.length > 0 && (
                    <optgroup label="Restrict to Specific Proxy">
                      {proxies.map((p: Proxy) => (
                        <option key={p.id} value={p.id}>
                          🎯 Proxy: {p.name} ({p.target_model})
                        </option>
                      ))}
                    </optgroup>
                  )}
                </select>
                <p className="text-[11px] text-muted-foreground leading-relaxed">
                  {selectedProxyId === "global"
                    ? "Global keys work across all standalone REST APIs (/v1/detect, /v1/pii/mask) and LLM proxies."
                    : "Key will be restricted exclusively to requests routing through this proxy endpoint."}
                </p>
              </div>

              <div className="space-y-1.5">
                <label className="text-xs font-medium text-foreground">Rate Limit (RPM — Optional)</label>
                <Input
                  type="number"
                  placeholder="e.g. 60 (Requests Per Minute)"
                  value={rateLimitRpm}
                  onChange={(e) => setRateLimitRpm(e.target.value)}
                  className="h-9 text-sm"
                />
              </div>

              <div className="flex justify-end gap-2 pt-3 border-t border-border/50">
                <Button type="button" variant="outline" size="sm" onClick={() => setShowCreateModal(false)}>
                  Cancel
                </Button>
                <Button type="submit" size="sm" disabled={createMutation.isPending} className="gap-1.5">
                  {createMutation.isPending ? "Creating..." : "Generate Key"}
                </Button>
              </div>
            </form>
          </div>
        </div>
      )}

      {/* Edit Scope Modal */}
      {editingKey && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-sm p-4 animate-in fade-in duration-200">
          <div className="w-full max-w-md rounded-2xl border border-border bg-card p-6 shadow-2xl space-y-5">
            <div className="flex items-center justify-between border-b border-border/50 pb-3">
              <div className="flex items-center gap-2">
                <Settings2 className="h-4 w-4 text-primary" />
                <h3 className="font-semibold text-foreground text-base">Edit Key Scope — {editingKey.name}</h3>
              </div>
              <Button variant="ghost" size="sm" className="h-7 w-7 p-0" onClick={() => setEditingKey(null)}>
                ✕
              </Button>
            </div>

            <div className="space-y-4">
              <div className="space-y-1.5">
                <label className="text-xs font-medium text-foreground">Select Scope Binding</label>
                <select
                  defaultValue={getScopeDetails(editingKey).proxyId || "global"}
                  onChange={(e) => {
                    const val = e.target.value;
                    updateMutation.mutate({
                      id: editingKey.id,
                      proxy_id: val,
                    });
                  }}
                  className="w-full h-9 rounded-md border border-input bg-background px-3 text-sm text-foreground shadow-sm focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring"
                >
                  <option value="global">🌐 Global Access (All Developer APIs & Proxies)</option>
                  {proxies && proxies.length > 0 && (
                    <optgroup label="Restrict to Specific Proxy">
                      {proxies.map((p: Proxy) => (
                        <option key={p.id} value={p.id}>
                          🎯 Proxy: {p.name} ({p.target_model})
                        </option>
                      ))}
                    </optgroup>
                  )}
                </select>
              </div>

              <div className="flex justify-end gap-2 pt-2">
                <Button variant="outline" size="sm" onClick={() => setEditingKey(null)}>
                  Close
                </Button>
              </div>
            </div>
          </div>
        </div>
      )}

      {/* Main Keys List Card */}
      <Card className="border-border/50 shadow-sm">
        <CardContent className="p-0">
          {isLoading ? (
            <div className="flex items-center justify-center py-16">
              <div className="flex items-center gap-2 text-sm text-muted-foreground">
                <div className="h-4 w-4 animate-spin rounded-full border-2 border-muted-foreground/30 border-t-muted-foreground" />
                Loading API keys...
              </div>
            </div>
          ) : !keys?.length ? (
            <EmptyState
              icon={<Key className="h-6 w-6 text-primary" />}
              title="No API keys created yet"
              description="Create a secret key to authenticate your standalone Developer API requests (/v1/detect, /v1/pii/mask) or proxy calls."
              action={
                <Button onClick={() => setShowCreateModal(true)} size="sm" className="gap-2 font-medium">
                  <Plus className="h-4 w-4" />
                  Create your first API Key
                </Button>
              }
            />
          ) : (
            <div className="divide-y divide-border/30">
              {keys.map((k: APIKey) => {
                const scopeInfo = getScopeDetails(k);
                return (
                  <div key={k.id} className="flex flex-col sm:flex-row sm:items-center justify-between gap-4 px-5 py-4 hover:bg-muted/30 transition-colors">
                    <div className="space-y-1.5">
                      <div className="flex flex-wrap items-center gap-2">
                        <span className="text-sm font-semibold text-foreground">{k.name || "Unnamed Key"}</span>
                        <Badge
                          variant={k.is_active ? "success" : "secondary"}
                          className="text-[10px] px-2 py-0.5"
                        >
                          {k.is_active ? "active" : "revoked"}
                        </Badge>
                        
                        <Badge
                          variant="outline"
                          className={
                            scopeInfo.isGlobal
                              ? "text-[10px] px-2 py-0.5 border-emerald-500/30 bg-emerald-500/10 text-emerald-600 dark:text-emerald-400 font-medium"
                              : "text-[10px] px-2 py-0.5 border-sky-500/30 bg-sky-500/10 text-sky-600 dark:text-sky-400 font-medium"
                          }
                        >
                          {scopeInfo.isGlobal ? (
                            <span className="flex items-center gap-1">
                              <Globe className="h-3 w-3" /> Global Access
                            </span>
                          ) : (
                            <span className="flex items-center gap-1">
                              <Server className="h-3 w-3" /> {scopeInfo.label}
                            </span>
                          )}
                        </Badge>
                      </div>

                      <div className="flex flex-wrap items-center gap-2 text-[11px] text-muted-foreground">
                        <code className="font-mono bg-muted/60 px-1.5 py-0.5 rounded text-foreground/80">
                          {k.key_prefix}...
                        </code>
                        {k.rate_limit_rpm && (
                          <>
                            <span className="text-border">|</span>
                            <span>{k.rate_limit_rpm} RPM</span>
                          </>
                        )}
                        {k.last_used_at && (
                          <>
                            <span className="text-border">|</span>
                            <span>Last used {new Date(k.last_used_at).toLocaleDateString()}</span>
                          </>
                        )}
                        <span className="text-border">|</span>
                        <span>Created {new Date(k.created_at).toLocaleDateString()}</span>
                      </div>
                    </div>

                    <div className="flex items-center gap-2 self-start sm:self-center shrink-0">
                      {k.is_active && (
                        <>
                          <Button
                            variant="outline"
                            size="sm"
                            className="h-7 text-xs gap-1"
                            onClick={() => setEditingKey(k)}
                          >
                            <Settings2 className="h-3 w-3" />
                            Scope Scope
                          </Button>

                          <Button
                            variant="ghost"
                            size="sm"
                            className="h-7 gap-1 text-xs text-muted-foreground hover:text-destructive hover:bg-destructive/10"
                            onClick={() => {
                              if (confirm(`Revoke key "${k.name}"? Active calls using this key will immediately fail.`)) {
                                revokeMutation.mutate(k.id);
                              }
                            }}
                            disabled={revokeMutation.isPending}
                          >
                            <Ban className="h-3 w-3" />
                            Revoke
                          </Button>
                        </>
                      )}
                    </div>
                  </div>
                );
              })}
            </div>
          )}
        </CardContent>
      </Card>
    </>
  );
}
