import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { Key, Ban, ArrowRight } from "lucide-react";
import { Link } from "@tanstack/react-router";
import { Card, CardContent } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { PageHeader, EmptyState } from "@/components/card";
import { api } from "@/api/client";
import type { APIKey } from "@/api/client";

export function ApiKeysPage() {
  const queryClient = useQueryClient();
  const { data: keys, isLoading } = useQuery({ queryKey: ["api-keys"], queryFn: api.apiKeys.list });

  const revokeMutation = useMutation({
    mutationFn: api.apiKeys.revoke,
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["api-keys"] }),
  });

  const activeCount = keys?.filter((k: APIKey) => k.is_active).length ?? 0;
  const revokedCount = (keys?.length ?? 0) - activeCount;

  return (
    <>
      <PageHeader
        title="API Keys"
        description="All gateway keys across your proxies"
        badge={
          keys && keys.length > 0 ? (
            <Badge variant="outline" className="text-[11px] font-normal">
              {activeCount} active{revokedCount > 0 ? ` · ${revokedCount} revoked` : ""}
            </Badge>
          ) : undefined
        }
      />

      <Card className="border-border/50">
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
              icon={<Key className="h-6 w-6" />}
              title="No API keys yet"
              description="API keys are generated from the Proxies page. Create a proxy, add a provider key, then generate a gateway key."
              action={
                <Link to="/proxies">
                  <Button variant="outline" size="sm" className="text-xs gap-1.5">
                    Go to Proxies <ArrowRight className="h-3 w-3" />
                  </Button>
                </Link>
              }
            />
          ) : (
            <div className="divide-y divide-border/30">
              {keys.map((k: APIKey) => (
                <div key={k.id} className="flex items-center justify-between px-5 py-4 hover:bg-muted/30 transition-colors">
                  <div>
                    <div className="flex items-center gap-2.5">
                      <span className="text-[13px] font-medium">{k.name || "Unnamed"}</span>
                      <Badge
                        variant={k.is_active ? "success" : "secondary"}
                        className="text-[10px] px-1.5 py-0"
                      >
                        {k.is_active ? "active" : "revoked"}
                      </Badge>
                      <Badge variant="outline" className="text-[10px] px-1.5 py-0 font-normal">
                        all proxies
                      </Badge>
                    </div>
                    <div className="flex items-center gap-2 mt-1 text-[11px] text-muted-foreground/60">
                      <code className="font-mono">{k.key_prefix}...</code>
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
                    </div>
                  </div>
                  <div className="flex items-center gap-2">
                    <span className="text-[11px] text-muted-foreground/40 tabular-nums">
                      {new Date(k.created_at).toLocaleDateString()}
                    </span>
                    {k.is_active && (
                      <Button
                        variant="ghost" size="sm" className="h-7 gap-1 text-xs text-muted-foreground hover:text-destructive"
                        onClick={() => { if (confirm(`Revoke key "${k.name}"? This cannot be undone.`)) revokeMutation.mutate(k.id); }}
                        disabled={revokeMutation.isPending}
                      >
                        <Ban className="h-3 w-3" />
                        Revoke
                      </Button>
                    )}
                  </div>
                </div>
              ))}
            </div>
          )}
        </CardContent>
      </Card>

      {keys && keys.length > 0 && (
        <p className="text-[11px] text-muted-foreground/50 mt-4 text-center">
          To generate new keys, go to{" "}
          <Link to="/proxies" className="text-primary hover:underline">Proxies</Link>
          {" "}and expand a proxy.
        </p>
      )}
    </>
  );
}
