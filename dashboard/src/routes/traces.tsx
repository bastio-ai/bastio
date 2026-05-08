import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Link, useNavigate } from "@tanstack/react-router";
import { Activity, AlertTriangle, CircleDollarSign, Cpu, MessagesSquare } from "lucide-react";

import { api } from "@/api/client";
import type { Trace } from "@/api/client";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent } from "@/components/ui/card";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { EmptyState, PageHeader } from "@/components/card";
import { SkeletonRows } from "@/components/skeleton";
import { FilterBar, emptyFilters, parseTagFilter, type ObserveFilters } from "@/components/observe/filter-bar";
import { KpiCard } from "@/components/observe/kpi-card";
import { LiveToggle } from "@/components/observe/live-toggle";
import { useTracesExtension } from "@/components/traces-extension";
import { downloadCSV } from "@/lib/csv";
import { formatCost, formatDuration, formatNumber } from "@/lib/utils";

export function TracesPage() {
  const navigate = useNavigate();
  const [filters, setFilters] = useState<ObserveFilters>(emptyFilters);
  const [live, setLive] = useState(false);

  const queryParams = useMemo(() => {
    const params: Record<string, string | number | string[]> = { limit: 100 };
    if (filters.status) params.status = filters.status;
    if (filters.provider) params.provider = filters.provider;
    if (filters.model) params.model = filters.model;
    if (filters.endUser) params.end_user_id = filters.endUser;
    if (filters.search) params.search = filters.search;
    if (filters.from) params.from = new Date(filters.from).toISOString();
    if (filters.to) params.to = new Date(filters.to).toISOString();
    if (filters.environment) params.environment = filters.environment;
    if (filters.release) params.release = filters.release;
    if (filters.traceName) params.trace_name = filters.traceName;
    const tags = parseTagFilter(filters.tags);
    if (tags.length) params.tag = tags;
    return params;
  }, [filters]);

  const { data: gatewayTraces, isLoading } = useQuery({
    queryKey: ["traces", queryParams, live],
    queryFn: () => api.traces.list(queryParams),
    refetchInterval: live ? 3000 : false,
  });

  // Optional extra rows fed by a downstream consumer (cloud-dashboard
  // injects browser-extension governance events as Trace-shaped rows
  // here). OSS standalone leaves the extension empty and the fetcher
  // never runs.
  const ext = useTracesExtension();
  const extQ = useQuery({
    queryKey: ["traces:extension", filters, live],
    queryFn: () => ext.fetchExtra!(filters),
    enabled: typeof ext.fetchExtra === "function",
    refetchInterval: live ? 3000 : false,
  });

  // Merge primary traces with extension rows, sorted by started_at desc
  // so the timeline is contiguous regardless of which source emitted
  // the row. Empty array fallbacks keep this stable while either query
  // is still in-flight.
  const traces = useMemo<Trace[]>(() => {
    const primary = gatewayTraces ?? [];
    const extras = extQ.data ?? [];
    if (extras.length === 0) return primary;
    const merged = [...primary, ...extras];
    merged.sort((a, b) => {
      const ta = a.started_at ? new Date(a.started_at).getTime() : 0;
      const tb = b.started_at ? new Date(b.started_at).getTime() : 0;
      return tb - ta;
    });
    return merged;
  }, [gatewayTraces, extQ.data]);

  const filtered = useMemo(() => {
    if (!traces) return [] as Trace[];
    if (filters.security === "threat") return traces.filter((t) => t.threat_detected);
    if (filters.security === "clean") return traces.filter((t) => !t.threat_detected);
    return traces;
  }, [traces, filters.security]);

  const kpis = useMemo(() => computeKpis(filtered), [filtered]);
  const hasActiveFilters = Object.values(filters).some(Boolean);
  const environments = useMemo(
    () => Array.from(new Set((traces ?? []).map((t) => t.environment ?? "").filter(Boolean))).sort(),
    [traces],
  );

  return (
    <>
      <PageHeader
        title="Traces"
        description="Every request flowing through your proxies, with full security context."
        action={<LiveToggle live={live} onToggle={() => setLive((v) => !v)} />}
      />

      <div className="grid grid-cols-2 gap-2 sm:grid-cols-4">
        <KpiCard
          label="Traces"
          value={formatNumber(kpis.count)}
          sub="In current window"
          icon={<Activity className="h-4 w-4" />}
        />
        <KpiCard
          label="Tokens"
          value={formatNumber(kpis.tokens)}
          sub={`${formatNumber(kpis.inputTokens)} in · ${formatNumber(kpis.outputTokens)} out`}
          icon={<Cpu className="h-4 w-4" />}
        />
        <KpiCard
          label="Cost"
          value={formatCost(kpis.cost)}
          sub={`avg ${formatCost(kpis.count ? kpis.cost / kpis.count : 0)} / trace`}
          icon={<CircleDollarSign className="h-4 w-4" />}
        />
        <KpiCard
          label="Threats"
          value={formatNumber(kpis.threats)}
          sub={`${kpis.count ? Math.round((kpis.threats / kpis.count) * 100) : 0}% of traces`}
          tone={kpis.threats ? "danger" : "success"}
          icon={<AlertTriangle className="h-4 w-4" />}
        />
      </div>

      <div className="mt-4">
        <FilterBar
          value={filters}
          onChange={setFilters}
          environments={environments}
          onCSV={() =>
            downloadCSV(
              `bastio-traces-${new Date().toISOString().slice(0, 10)}.csv`,
              (filtered ?? []) as unknown as Record<string, unknown>[],
            )
          }
        />
      </div>

      <Card className="border-border/50 overflow-hidden">
        <CardContent className="p-0">
          {isLoading ? (
            <SkeletonRows count={8} />
          ) : !filtered.length ? (
            <TracesEmptyState hasActiveFilters={hasActiveFilters} />
          ) : (
            <Table>
              <TableHeader>
                <TableRow className="hover:bg-transparent border-border/50">
                  <TableHead className="h-10 w-[7rem] text-[11px] font-semibold uppercase tracking-wider text-muted-foreground/60">
                    Time
                  </TableHead>
                  <TableHead className="h-10 text-[11px] font-semibold uppercase tracking-wider text-muted-foreground/60">
                    Name
                  </TableHead>
                  <TableHead className="h-10 w-[5.5rem] text-[11px] font-semibold uppercase tracking-wider text-muted-foreground/60">
                    Status
                  </TableHead>
                  <TableHead className="h-10 text-[11px] font-semibold uppercase tracking-wider text-muted-foreground/60">
                    Security
                  </TableHead>
                  <TableHead className="h-10 text-[11px] font-semibold uppercase tracking-wider text-muted-foreground/60">
                    Provider
                  </TableHead>
                  <TableHead className="h-10 text-right text-[11px] font-semibold uppercase tracking-wider text-muted-foreground/60">
                    Latency
                  </TableHead>
                  <TableHead className="h-10 text-right text-[11px] font-semibold uppercase tracking-wider text-muted-foreground/60">
                    Tokens
                  </TableHead>
                  <TableHead className="h-10 text-right text-[11px] font-semibold uppercase tracking-wider text-muted-foreground/60">
                    Cost
                  </TableHead>
                  <TableHead className="h-10 text-[11px] font-semibold uppercase tracking-wider text-muted-foreground/60">
                    End user
                  </TableHead>
                  <TableHead className="h-10 w-10 text-[11px] font-semibold uppercase tracking-wider text-muted-foreground/60" />
                </TableRow>
              </TableHeader>
              <TableBody>
                {filtered.map((t: Trace) => (
                  <TableRow
                    key={t.id}
                    className="cursor-pointer border-border/30 hover:bg-muted/30"
                    onClick={() => navigate({ to: "/traces/$id", params: { id: t.id } })}
                  >
                    <TableCell
                      className="font-mono tabular-nums text-xs text-muted-foreground"
                      title={new Date(t.started_at).toLocaleString()}
                    >
                      {relativeTime(t.started_at)}
                    </TableCell>
                    <TableCell className="font-mono text-xs text-foreground/90">
                      <div className="flex items-center gap-2">
                        <span className="truncate">{t.trace_name || t.path || t.model}</span>
                        {t.environment ? (
                          <Badge variant="outline" className="text-[9px] px-1 py-0 text-muted-foreground">
                            {t.environment}
                          </Badge>
                        ) : null}
                        {t.release ? (
                          <span className="font-mono text-[10px] text-muted-foreground/70">
                            {t.release}
                          </span>
                        ) : null}
                      </div>
                    </TableCell>
                    <TableCell>
                      <Badge
                        variant={
                          t.status === "ok"
                            ? "success"
                            : t.status === "blocked"
                            ? "destructive"
                            : "warning"
                        }
                        className="text-[10px] px-1.5 py-0"
                      >
                        {t.status}
                      </Badge>
                    </TableCell>
                    <TableCell>
                      {t.threat_detected ? (
                        <Badge variant="destructive" className="text-[10px] px-1.5 py-0">
                          {(t.threat_types ?? []).slice(0, 2).join(", ") || "threat"} ·{" "}
                          {((t.threat_score ?? 0) * 100).toFixed(0)}%
                        </Badge>
                      ) : (
                        <Badge variant="outline" className="text-[10px] px-1.5 py-0 text-muted-foreground">
                          clean
                        </Badge>
                      )}
                    </TableCell>
                    <TableCell className="text-xs text-muted-foreground">
                      <span className="font-medium text-foreground/80">{t.provider}</span>
                      <span className="ml-1 font-mono text-muted-foreground/70">{t.model}</span>
                    </TableCell>
                    <TableCell className="text-right font-mono tabular-nums text-xs text-muted-foreground">
                      {formatDuration(t.duration_ms)}
                    </TableCell>
                    <TableCell className="text-right font-mono tabular-nums text-xs text-muted-foreground">
                      {(t.input_tokens + t.output_tokens).toLocaleString()}
                    </TableCell>
                    <TableCell className="text-right font-mono tabular-nums text-xs text-muted-foreground">
                      {formatCost(t.cost_cents)}
                    </TableCell>
                    <TableCell className="truncate text-xs text-muted-foreground">
                      {t.end_user_id ? (
                        <Link
                          to="/users/$id"
                          params={{ id: t.end_user_id }}
                          onClick={(e) => e.stopPropagation()}
                          className="hover:text-foreground hover:underline"
                        >
                          {t.end_user_id}
                        </Link>
                      ) : (
                        "—"
                      )}
                    </TableCell>
                    <TableCell className="text-right">
                      {t.session_id ? (
                        <Link
                          to="/sessions/$id"
                          params={{ id: t.session_id }}
                          onClick={(e) => e.stopPropagation()}
                          className="inline-flex h-6 w-6 items-center justify-center rounded text-muted-foreground hover:bg-muted/50 hover:text-foreground"
                          aria-label="Open session"
                          title="Open session"
                        >
                          <MessagesSquare className="h-3.5 w-3.5" />
                        </Link>
                      ) : null}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>
    </>
  );
}

function computeKpis(traces: Trace[]) {
  let tokens = 0;
  let inputTokens = 0;
  let outputTokens = 0;
  let cost = 0;
  let threats = 0;
  for (const t of traces) {
    inputTokens += t.input_tokens ?? 0;
    outputTokens += t.output_tokens ?? 0;
    tokens += (t.input_tokens ?? 0) + (t.output_tokens ?? 0);
    cost += t.cost_cents ?? 0;
    if (t.threat_detected) threats += 1;
  }
  return { count: traces.length, tokens, inputTokens, outputTokens, cost, threats };
}

function TracesEmptyState({ hasActiveFilters }: { hasActiveFilters: boolean }) {
  if (hasActiveFilters) {
    return (
      <EmptyState
        icon={<Activity className="h-6 w-6" />}
        title="No traces match these filters"
        description="Try loosening or clearing the filters."
      />
    );
  }
  return (
    <div className="px-6 py-12">
      <div className="mx-auto max-w-2xl space-y-6">
        <div className="text-center">
          <Activity className="mx-auto h-6 w-6 text-muted-foreground" />
          <h3 className="mt-3 text-sm font-medium">No traces yet</h3>
          <p className="mt-1 text-xs text-muted-foreground">
            Three ways to start feeding Bastio.
          </p>
        </div>
        <div className="space-y-4">
          <OnboardingBlock
            title="1 · Point your OpenAI SDK at the gateway"
            body={`export OPENAI_BASE_URL=http://localhost:4000/v1
export OPENAI_API_KEY=sk-bastio-...`}
          />
          <OnboardingBlock
            title="2 · Or curl directly"
            body={`curl http://localhost:4000/v1/chat/completions \\
  -H "Authorization: Bearer sk-bastio-..." \\
  -H "X-Bastio-Environment: production" \\
  -H "X-Session-Id: chat-42" \\
  -d '{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}]}'`}
          />
          <OnboardingBlock
            title="3 · Or ship OpenTelemetry spans"
            body={`# Any OTEL exporter targeting:
# POST http://localhost:4000/v1/traces  (OTLP/HTTP + JSON)
# Example (OpenLLMetry, Logfire, ...):
export OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4000`}
          />
        </div>
      </div>
    </div>
  );
}

function OnboardingBlock({ title, body }: { title: string; body: string }) {
  return (
    <div className="rounded border border-border/50 bg-muted/10">
      <div className="border-b border-border/50 px-3 py-2 text-[10px] font-semibold uppercase tracking-wider text-muted-foreground/60">
        {title}
      </div>
      <pre className="overflow-auto whitespace-pre p-3 font-mono text-[11px] leading-relaxed">
        {body}
      </pre>
    </div>
  );
}

function relativeTime(iso: string): string {
  const then = new Date(iso).getTime();
  if (Number.isNaN(then)) return iso;
  const diff = Date.now() - then;
  if (diff < 60_000) return `${Math.max(1, Math.round(diff / 1000))}s ago`;
  if (diff < 3_600_000) return `${Math.round(diff / 60_000)}m ago`;
  if (diff < 86_400_000) return `${Math.round(diff / 3_600_000)}h ago`;
  return `${Math.round(diff / 86_400_000)}d ago`;
}
