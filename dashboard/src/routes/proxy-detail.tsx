import { useQuery } from "@tanstack/react-query";
import { Link, useNavigate, useParams } from "@tanstack/react-router";
import { ArrowLeft, Activity } from "lucide-react";

import { api } from "@/api/client";
import type { Trace } from "@/api/client";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent } from "@/components/ui/card";
import { EmptyState } from "@/components/card";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { formatCost, formatDuration } from "@/lib/utils";

export function ProxyDetailPage() {
  const { id } = useParams({ from: "/proxies/$id" });
  const navigate = useNavigate();

  const proxy = useQuery({
    queryKey: ["proxy", id],
    queryFn: () => api.proxies.get(id),
  });

  const traces = useQuery({
    queryKey: ["traces", { proxy_id: id, limit: 50 }],
    queryFn: () => api.traces.list({ proxy_id: id, limit: 50 }),
  });

  if (proxy.isLoading) {
    return <LoadingBlock label="Loading proxy..." />;
  }
  if (proxy.error || !proxy.data) {
    return (
      <div className="space-y-4">
        <BackLink />
        <EmptyStateCard title="Proxy not found" description="This id no longer exists, or belongs to another tenant." />
      </div>
    );
  }

  const p = proxy.data;
  const rows = traces.data ?? [];

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <BackLink />
        <div className="flex items-center gap-2">
          <span className="font-mono text-[11px] text-muted-foreground">{p.id}</span>
          <Badge
            variant={p.is_active ? "success" : "secondary"}
            className="text-[10px] px-1.5 py-0"
          >
            {p.is_active ? "active" : "disabled"}
          </Badge>
        </div>
      </div>

      <Card className="border-border/50">
        <CardContent className="p-4">
          <div className="grid grid-cols-2 gap-x-6 gap-y-3 sm:grid-cols-3 lg:grid-cols-5">
            <Field label="Name" value={p.name} />
            <Field label="Slug" value={p.slug} />
            <Field label="Listen path" value={p.listen_path} />
            <Field label="Provider" value={p.target_provider} />
            <Field label="Model" value={p.target_model} />
            <Field label="Created" value={new Date(p.created_at).toLocaleString()} />
          </div>
        </CardContent>
      </Card>

      <Card className="border-border/50">
        <div className="border-b border-border/50 p-3 text-xs font-semibold uppercase tracking-wider text-muted-foreground/60">
          Recent traces ({rows.length})
        </div>
        <CardContent className="p-0">
          {traces.isLoading ? (
            <LoadingBlock label="Loading traces..." />
          ) : rows.length === 0 ? (
            <EmptyState
              icon={<Activity className="h-6 w-6" />}
              title="No traces for this proxy"
              description="Send a request through this proxy to see it appear here."
            />
          ) : (
            <Table>
              <TableHeader>
                <TableRow className="hover:bg-transparent border-border/50">
                  <TableHead className="text-[11px] font-semibold uppercase tracking-wider text-muted-foreground/60 h-10">Status</TableHead>
                  <TableHead className="text-[11px] font-semibold uppercase tracking-wider text-muted-foreground/60 h-10">Model</TableHead>
                  <TableHead className="text-[11px] font-semibold uppercase tracking-wider text-muted-foreground/60 h-10 text-right">Tokens</TableHead>
                  <TableHead className="text-[11px] font-semibold uppercase tracking-wider text-muted-foreground/60 h-10 text-right">Cost</TableHead>
                  <TableHead className="text-[11px] font-semibold uppercase tracking-wider text-muted-foreground/60 h-10 text-right">Latency</TableHead>
                  <TableHead className="text-[11px] font-semibold uppercase tracking-wider text-muted-foreground/60 h-10 text-right">Time</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {rows.map((t: Trace) => (
                  <TableRow
                    key={t.id}
                    className="border-border/30 hover:bg-muted/30 cursor-pointer"
                    onClick={() => navigate({ to: "/traces/$id", params: { id: t.id } })}
                  >
                    <TableCell className="py-3">
                      <Badge
                        variant={t.status === "ok" ? "success" : t.status === "blocked" ? "destructive" : "warning"}
                        className="text-[10px] px-1.5 py-0"
                      >
                        {t.status}
                      </Badge>
                    </TableCell>
                    <TableCell className="font-mono text-xs text-foreground/80">{t.model}</TableCell>
                    <TableCell className="text-xs text-muted-foreground font-mono tabular-nums text-right">
                      {(t.input_tokens + t.output_tokens).toLocaleString()}
                    </TableCell>
                    <TableCell className="text-xs text-muted-foreground font-mono tabular-nums text-right">
                      {formatCost(t.cost_cents)}
                    </TableCell>
                    <TableCell className="text-xs text-muted-foreground font-mono tabular-nums text-right">
                      {formatDuration(t.duration_ms)}
                    </TableCell>
                    <TableCell className="text-xs text-muted-foreground tabular-nums text-right">
                      {new Date(t.started_at).toLocaleTimeString()}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>
    </div>
  );
}

function BackLink() {
  return (
    <Link to="/proxies" className="inline-flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground">
      <ArrowLeft className="h-3.5 w-3.5" /> Back to proxies
    </Link>
  );
}

function LoadingBlock({ label }: { label: string }) {
  return (
    <div className="flex items-center justify-center py-16">
      <div className="flex items-center gap-2 text-sm text-muted-foreground">
        <div className="h-4 w-4 animate-spin rounded-full border-2 border-muted-foreground/30 border-t-muted-foreground" />
        {label}
      </div>
    </div>
  );
}

function EmptyStateCard({ title, description }: { title: string; description: string }) {
  return (
    <Card className="border-border/50">
      <CardContent className="py-12 text-center">
        <p className="text-sm font-medium">{title}</p>
        <p className="mt-1 text-xs text-muted-foreground">{description}</p>
      </CardContent>
    </Card>
  );
}

function Field({ label, value }: { label: string; value?: string }) {
  return (
    <div>
      <p className="text-[10px] font-semibold uppercase tracking-wider text-muted-foreground/60">{label}</p>
      <p className="mt-1 font-mono text-xs">{value || "—"}</p>
    </div>
  );
}
