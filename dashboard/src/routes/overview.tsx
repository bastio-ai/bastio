import { useMemo, useState, type ComponentType } from "react";
import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import {
  Activity,
  AlertTriangle,
  ArrowRight,
  Bot,
  CheckCircle2,
  Clock3,
  KeyRound,
  LayoutDashboard,
  Server,
  ShieldAlert,
  Terminal,
} from "lucide-react";

import { api, type APIKey, type Proxy, type Session, type ThreatEvent, type Trace, type UserAnalytics } from "@/api/client";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { ConnectCliDialog } from "@/components/connect-cli-dialog";
import { DataCell, DataPanel, DataRow, DataTable } from "@/components/data/data-panel";
import { useDashboardControls, type DashboardRange } from "@/components/dashboard-controls-context";
import { LatencyChart } from "@/components/data/latency-chart";
import { Pill } from "@/components/data/pill";
import { RequestVolumeChart } from "@/components/data/request-volume-chart";
import { StatusDot } from "@/components/data/status-dot";
import { useOverviewExtension } from "@/components/overview-extension";
import { WorkspaceSummaryStrip } from "@/components/observe/workspace-summary-strip";
import { Skeleton } from "@/components/skeleton";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { useWorkspaceSidebar } from "@/components/workspace-sidebar-context";
import { cn, formatCost, formatDuration, formatNumber } from "@/lib/utils";

type RangePreset = DashboardRange;
type AttentionTone = "danger" | "warning" | "neutral";
type AttentionRoute = "/threats" | "/traces" | "/api-keys" | "/proxies" | "/settings";

type AttentionItem = {
  title: string;
  detail: string;
  action: string;
  to: AttentionRoute;
  tone: AttentionTone;
  icon: ComponentType<{ className?: string }>;
};

const RANGE_META: Record<RangePreset, { label: string; shortLabel: string; durationMs: number }> = {
  "1h": { label: "Last hour", shortLabel: "1h", durationMs: 60 * 60 * 1000 },
  "24h": { label: "Last 24 hours", shortLabel: "24h", durationMs: 24 * 60 * 60 * 1000 },
  "7d": { label: "Last 7 days", shortLabel: "7d", durationMs: 7 * 24 * 60 * 60 * 1000 },
  "30d": { label: "Last 30 days", shortLabel: "30d", durationMs: 30 * 24 * 60 * 60 * 1000 },
};

const EMPTY_ANALYTICS = {
  total_requests: 0,
  total_threats: 0,
  total_blocked: 0,
  total_cost_cents: 0,
  avg_duration_ms: 0,
  requests_by_hour: [] as Array<{ hour: string; count: number }>,
  threats_by_type: [] as Array<{ type: string; count: number }>,
  top_models: [] as Array<{ model: string; count: number; cost_cents: number }>,
};

export function OverviewPage() {
  const { range, setRange, environment } = useDashboardControls();
  const [connectOpen, setConnectOpen] = useState(false);
  const window = useMemo(() => makeWindow(range), [range]);
  const overviewExtension = useOverviewExtension();

  const health = useQuery({ queryKey: ["health"], queryFn: api.health });
  const analytics = useQuery({
    queryKey: ["overview", "analytics", range, environment, window.current.from, window.current.to],
    queryFn: () => api.analytics.overview({ ...window.current, environment: environment || undefined }),
  });
  const previousAnalytics = useQuery({
    queryKey: ["overview", "analytics", "previous", range, environment, window.previous.from, window.previous.to],
    queryFn: () => api.analytics.overview({ ...window.previous, environment: environment || undefined }),
  });
  const traces = useQuery({
    queryKey: ["overview", "traces", range, environment, window.current.from, window.current.to],
    queryFn: () => api.traces.list({ limit: 500, ...window.current, environment: environment || undefined }),
  });
  const threats = useQuery({
    queryKey: ["overview", "threats", range, environment, window.current.from, window.current.to],
    queryFn: () => api.threats.list({ limit: 500, ...window.current, environment: environment || undefined }),
  });
  const sessions = useQuery({
    queryKey: ["overview", "sessions", range, environment, window.current.from, window.current.to],
    queryFn: () => api.sessions.list({ limit: 200, ...window.current, environment: environment || undefined }),
  });
  const users = useQuery({
    queryKey: ["overview", "users"],
    queryFn: () => api.users.list({ limit: 5, order_by: "threats" }),
  });
  const apiKeys = useQuery({ queryKey: ["overview", "api-keys"], queryFn: api.apiKeys.list });
  const proxies = useQuery({ queryKey: ["overview", "proxies"], queryFn: api.proxies.list });

  const current = analytics.data ?? EMPTY_ANALYTICS;
  const previous = previousAnalytics.data ?? EMPTY_ANALYTICS;
  const traceData = traces.data ?? [];
  const threatData = threats.data ?? [];
  const sessionData = sessions.data ?? [];
  const userData = users.data ?? [];
  const isLoading = analytics.isPending || traces.isPending || threats.isPending;

  const traceMetrics = useMemo(() => computeTraceMetrics(traceData, window.current), [traceData, window.current]);
  const securityMetrics = useMemo(() => computeSecurityMetrics(threatData, current.total_blocked, current.total_threats), [current.total_blocked, current.total_threats, threatData]);
  const activitySeries = useMemo(
    () => makeActivitySeries(current.requests_by_hour, threatData, window.current, range === "1h" ? 12 : range === "24h" ? 24 : range === "7d" ? 28 : 30),
    [current.requests_by_hour, range, threatData, window.current],
  );
  const attention = useMemo(
    () => buildAttentionItems({
      current,
      critical: securityMetrics.critical,
      allowed: securityMetrics.allowed,
      errors: traceMetrics.errorCount,
      globalKeys: countGlobalKeys(apiKeys.data ?? []),
      proxies: proxies.data ?? [],
    }),
    [apiKeys.data, current, proxies.data, securityMetrics.allowed, securityMetrics.critical, traceMetrics.errorCount],
  );

  const requestDelta = describeDelta(current.total_requests, previous.total_requests);
  const costDelta = describeDelta(current.total_cost_cents, previous.total_cost_cents);
  const latencyDelta = describeDelta(current.avg_duration_ms, previous.avg_duration_ms, true);
  const rangeLabel = RANGE_META[range].label;

  const sidebarConfig = useMemo(() => ({
    parentLabel: "Bastio",
    parentTo: "/",
    title: "Overview",
    activeLabel: "Production overview",
    activeIcon: LayoutDashboard,
    timeLabel: rangeLabel,
    views: [],
    filtersLabel: "Window",
    filters: <OverviewRangeFilter value={range} onChange={setRange} />,
  }), [range, rangeLabel]);
  useWorkspaceSidebar(sidebarConfig);

  return (
    <div className="flex h-full min-h-0 flex-col bg-background">
      <WorkspaceSummaryStrip metrics={[
        {
          value: formatNumber(current.total_requests),
          label: "requests",
          sub: `${requestDelta} · ${rangeLabel.toLowerCase()}`,
        },
        {
          value: formatNumber(current.total_threats),
          label: "threats",
          sub: current.total_threats ? `${securityMetrics.preventionRate}% prevented · ${securityMetrics.critical} critical` : "No threats detected",
          tone: securityMetrics.critical ? "danger" : "success",
        },
        {
          value: formatCost(current.total_cost_cents),
          label: "cost",
          sub: `${costDelta} · ${current.total_requests ? `${formatCost(current.total_cost_cents / current.total_requests)} / request` : "No usage"}`,
          tone: current.total_cost_cents > previous.total_cost_cents * 1.25 && previous.total_cost_cents > 0 ? "warning" : undefined,
        },
        {
          value: traceMetrics.p95 ? formatDuration(traceMetrics.p95) : "—",
          label: "p95 latency",
          sub: `${latencyDelta} avg · ${traceMetrics.errorRate}% errors`,
          tone: traceMetrics.errorRate >= 5 ? "danger" : traceMetrics.errorRate > 0 ? "warning" : undefined,
        },
      ]} />

      <div className="flex min-h-14 flex-shrink-0 flex-wrap items-center justify-between gap-3 border-b border-border-subtle px-4 py-2.5">
        <div>
          <h1 className="text-[15px] font-bold tracking-tight text-foreground">Production overview</h1>
          <p className="mt-0.5 text-[10px] text-muted-foreground">What changed, what needs attention, and where usage is moving</p>
        </div>
        <div className="flex items-center gap-2">
          <RangeSelect value={range} onChange={setRange} />
          {health.data ? (
            <span className="inline-flex h-8 items-center gap-2 rounded-md border border-border-subtle bg-surface-1 px-2.5 font-mono text-[10px] text-muted-foreground">
              <StatusDot tone={health.data.status === "healthy" ? "success" : "danger"} pulse size={6} />
              {health.data.status === "healthy" ? "Gateway healthy" : "Gateway issue"}
            </span>
          ) : null}
        </div>
      </div>

      <div className="min-h-0 flex-1 overflow-auto p-3">
        <div className="flex flex-col gap-3">
          <div className="grid gap-3 xl:grid-cols-[minmax(0,1.7fr)_minmax(320px,0.8fr)]">
            {isLoading ? (
              <Skeleton className="h-[214px] rounded-xl" />
            ) : activitySeries.total.some((value) => value > 0) ? (
              <RequestVolumeChart
                total={activitySeries.total}
                blocked={activitySeries.blocked}
                label={`Request activity · ${rangeLabel}`}
                unit="requests / bucket"
                height={112}
                className="order-2 xl:order-1"
              />
            ) : (
              <EmptySurface
                icon={Activity}
                title="No request activity in this window"
                detail="Send traffic through a gateway or select a longer time window."
                to="/settings"
                action="Open quick start"
                className="order-2 xl:order-1"
              />
            )}
            <AttentionCenter items={attention} loading={apiKeys.isPending || proxies.isPending || isLoading} />
          </div>

          <div className="grid gap-3 xl:grid-cols-2">
            <SecurityPosture data={current} metrics={securityMetrics} loading={analytics.isPending || threats.isPending} />
            <ReliabilityPanel traceMetrics={traceMetrics} loading={traces.isPending} rangeLabel={rangeLabel} />
          </div>

          <div className="grid gap-3 xl:grid-cols-[minmax(0,1fr)_minmax(0,1fr)]">
            <ModelEconomics models={current.top_models} requests={current.total_requests} cost={current.total_cost_cents} loading={analytics.isPending} />
            <UsageFootprint sessions={sessionData} users={userData} traces={traceData} loading={sessions.isPending || users.isPending || traces.isPending} />
          </div>

          <QuickDeveloperConnectCard onOpenConnect={() => setConnectOpen(true)} />

          <RecentInvestigations threats={threatData} traces={traceData} loading={threats.isPending || traces.isPending} />

          {overviewExtension.insights}
        </div>
      </div>

      <ConnectCliDialog open={connectOpen} onOpenChange={setConnectOpen} />
    </div>
  );
}

function QuickDeveloperConnectCard({ onOpenConnect }: { onOpenConnect: () => void }) {
  return (
    <div className="rounded-xl border border-border/80 bg-gradient-to-r from-card to-accent/5 p-4 flex flex-col sm:flex-row items-start sm:items-center justify-between gap-4">
      <div className="space-y-1">
        <div className="flex items-center gap-2">
          <span className="flex size-6 items-center justify-center rounded-md bg-accent/10 text-accent">
            <Terminal className="size-3.5" />
          </span>
          <h3 className="text-xs font-semibold text-foreground">
            Connect Local Terminal CLI, MCP Firewalls &amp; Framework SDKs
          </h3>
          <Badge variant="outline" className="text-[10px] text-accent border-accent/30 bg-accent/10">
            Zero-Dep
          </Badge>
        </div>
        <p className="text-[11px] text-muted-foreground">
          Stream traces and threat detections from <code className="text-foreground font-mono">bastio dev</code>, <code className="text-foreground font-mono">bastio scan</code>, or <code className="text-foreground font-mono">bastio mcp-proxy</code> directly into this dashboard.
        </p>
      </div>

      <div className="flex items-center gap-2 shrink-0">
        <Button
          size="sm"
          variant="outline"
          onClick={onOpenConnect}
          className="h-7 text-xs gap-1.5 border-border-subtle bg-background hover:bg-muted font-medium"
        >
          <Terminal className="size-3" />
          CLI Quickstart
        </Button>
        <Link to="/mcp">
          <Button size="sm" className="h-7 text-xs gap-1.5 font-medium">
            <Bot className="size-3" />
            Agent &amp; MCP Hub
          </Button>
        </Link>
      </div>
    </div>
  );
}

function AttentionCenter({ items, loading }: { items: AttentionItem[]; loading: boolean }) {
  return (
    <DataPanel
      title="Attention required"
      sub={loading ? "Checking posture" : items.length ? `${items.length} prioritized` : "No urgent items"}
      className="order-1 min-h-[214px] xl:order-2"
    >
      {loading ? (
        <div className="space-y-2 p-3">{Array.from({ length: 3 }).map((_, index) => <Skeleton key={index} className="h-12" />)}</div>
      ) : items.length ? (
        <div className="divide-y divide-border-subtle">
          {items.slice(0, 4).map((item) => {
            const Icon = item.icon;
            return (
              <Link key={`${item.to}:${item.title}`} to={item.to} className="group flex items-center gap-3 px-3 py-2.5 transition-colors hover:bg-surface-2">
                <span className={cn("flex h-8 w-8 flex-shrink-0 items-center justify-center rounded-md border border-border-subtle bg-surface-1", item.tone === "danger" && "text-danger", item.tone === "warning" && "text-warn", item.tone === "neutral" && "text-muted-foreground")}>
                  <Icon className="h-4 w-4" />
                </span>
                <span className="min-w-0 flex-1">
                  <span className="block truncate text-[11px] font-semibold text-foreground">{item.title}</span>
                  <span className="mt-0.5 block truncate text-[10px] text-muted-foreground">{item.detail}</span>
                </span>
                <span className="inline-flex items-center gap-1 text-[10px] text-muted-foreground transition-colors group-hover:text-foreground">{item.action}<ArrowRight className="h-3 w-3" /></span>
              </Link>
            );
          })}
        </div>
      ) : (
        <div className="flex min-h-40 flex-col items-center justify-center gap-2 px-6 text-center">
          <span className="flex h-9 w-9 items-center justify-center rounded-full border border-success-border bg-success-bg text-success"><CheckCircle2 className="h-4 w-4" /></span>
          <p className="text-[12px] font-semibold text-foreground">No urgent action required</p>
          <p className="max-w-xs text-[10px] leading-relaxed text-muted-foreground">Gateway health, access scope, security outcomes, and request errors look stable.</p>
        </div>
      )}
    </DataPanel>
  );
}

function SecurityPosture({ data, metrics, loading }: { data: typeof EMPTY_ANALYTICS; metrics: ReturnType<typeof computeSecurityMetrics>; loading: boolean }) {
  return (
    <DataPanel title="Security posture" sub="Detection outcomes" action={<PanelLink to="/threats" label="Investigate" />}>
      {loading ? <PanelSkeleton /> : (
        <>
          <div className="grid grid-cols-2 gap-px bg-border-subtle sm:grid-cols-4">
            <PanelMetric label="Detected" value={formatNumber(data.total_threats)} />
            <PanelMetric label="Blocked" value={formatNumber(data.total_blocked)} tone={data.total_blocked ? "danger" : "default"} />
            <PanelMetric label="Prevention" value={`${metrics.preventionRate}%`} tone={metrics.preventionRate === 100 ? "success" : metrics.allowed ? "warning" : "default"} />
            <PanelMetric label="Users affected" value={formatNumber(metrics.affectedUsers)} tone={metrics.affectedUsers ? "warning" : "default"} />
          </div>
          <div className="divide-y divide-border-subtle">
            {data.threats_by_type.slice(0, 4).map((item, index) => (
              <div key={item.type} className="grid grid-cols-[1.5rem_minmax(0,1fr)_auto] items-center gap-2 px-3 py-2.5 text-[10px]">
                <span className="font-mono text-muted-foreground">{index + 1}</span>
                <span className="truncate text-foreground">{humanize(item.type)}</span>
                <span className="font-mono tabular-nums text-muted-foreground">{formatNumber(item.count)} events</span>
              </div>
            ))}
            {!data.threats_by_type.length ? <InlineEmpty label="No threat categories in this window" /> : null}
          </div>
        </>
      )}
    </DataPanel>
  );
}

function ReliabilityPanel({ traceMetrics, loading, rangeLabel }: { traceMetrics: ReturnType<typeof computeTraceMetrics>; loading: boolean; rangeLabel: string }) {
  return (
    <DataPanel title="Reliability" sub={rangeLabel} action={<PanelLink to="/traces" label="View traces" />}>
      {loading ? <PanelSkeleton /> : !traceMetrics.count ? <InlineEmpty label="No latency or error data in this window" /> : (
        <div className="p-4">
          <div className="mb-3 flex flex-wrap items-baseline gap-x-5 gap-y-1 font-mono text-[11px] tabular-nums">
            <span className="text-muted-foreground">p50 <span className="text-foreground">{formatDuration(traceMetrics.p50)}</span></span>
            <span className="text-muted-foreground">p95 <span className="text-foreground">{formatDuration(traceMetrics.p95)}</span></span>
            <span className="text-muted-foreground">p99 <span className="text-foreground">{formatDuration(traceMetrics.p99)}</span></span>
            <span className={cn("ml-auto", traceMetrics.errorRate ? "text-warn" : "text-muted-foreground")}>{traceMetrics.errorRate}% errors</span>
          </div>
          <LatencyChart series={traceMetrics.latencySeries} threshold={traceMetrics.p95 || undefined} />
          <div className="mt-3 grid gap-2 sm:grid-cols-3">
            {traceMetrics.slowest.slice(0, 3).map((item) => (
              <div key={item.label} className="rounded-md border border-border-subtle bg-surface-1 px-2.5 py-2">
                <p className="truncate font-mono text-[10px] text-foreground">{item.label}</p>
                <p className="mt-1 text-[9px] text-muted-foreground">avg {formatDuration(item.average)} · {item.count} requests</p>
              </div>
            ))}
          </div>
        </div>
      )}
    </DataPanel>
  );
}

function ModelEconomics({ models, requests, cost, loading }: { models: Array<{ model: string; count: number; cost_cents: number }>; requests: number; cost: number; loading: boolean }) {
  return (
    <DataPanel title="Model economics" sub="Requests and spend" action={<PanelLink to="/analytics" label="Open analytics" />}>
      {loading ? <PanelSkeleton /> : !models.length ? <InlineEmpty label="Model usage appears after requests are processed" /> : (
        <>
          <div className="grid grid-cols-2 gap-px border-b border-border-subtle bg-border-subtle">
            <PanelMetric label="Cost / request" value={requests ? formatCost(cost / requests) : "—"} />
            <PanelMetric label="Models used" value={formatNumber(models.length)} />
          </div>
          <div className="divide-y divide-border-subtle">
            {models.slice(0, 5).map((model, index) => (
              <div key={model.model} className="grid grid-cols-[1.5rem_minmax(0,1fr)_auto] items-center gap-2 px-3 py-2.5 text-[10px]">
                <span className="font-mono text-muted-foreground">{index + 1}</span>
                <span className="truncate font-mono text-foreground">{model.model}</span>
                <span className="font-mono tabular-nums text-muted-foreground">{formatNumber(model.count)} req · {formatCost(model.cost_cents)}</span>
              </div>
            ))}
          </div>
        </>
      )}
    </DataPanel>
  );
}

function UsageFootprint({ sessions, users, traces, loading }: { sessions: Session[]; users: UserAnalytics[]; traces: Trace[]; loading: boolean }) {
  const uniqueUsers = new Set(traces.map((trace) => trace.end_user_id).filter(Boolean)).size;
  const tokens = traces.reduce((sum, trace) => sum + (trace.total_tokens ?? 0), 0);
  const threatenedSessions = sessions.filter((session) => (session.threat_count ?? 0) > 0).length;
  return (
    <DataPanel title="Usage footprint" sub="Attribution and sessions" action={<PanelLink to="/users" label="View end users" />}>
      {loading ? <PanelSkeleton /> : (
        <>
          <div className="grid grid-cols-2 gap-px bg-border-subtle sm:grid-cols-4">
            <PanelMetric label="End users" value={formatNumber(uniqueUsers)} />
            <PanelMetric label="Sessions" value={formatNumber(sessions.length)} />
            <PanelMetric label="Tokens" value={formatNumber(tokens)} />
            <PanelMetric label="Risky sessions" value={formatNumber(threatenedSessions)} tone={threatenedSessions ? "warning" : "default"} />
          </div>
          <div className="divide-y divide-border-subtle">
            {users.slice(0, 4).map((user, index) => (
              <Link key={user.end_user_id} to="/users/$id" params={{ id: user.end_user_id }} className="grid grid-cols-[1.5rem_minmax(0,1fr)_auto] items-center gap-2 px-3 py-2.5 text-[10px] transition-colors hover:bg-surface-2">
                <span className="font-mono text-muted-foreground">{index + 1}</span>
                <span className="truncate font-mono text-foreground">{user.end_user_id}</span>
                <span className={cn("font-mono tabular-nums text-muted-foreground", user.total_threats && "text-danger")}>{user.total_threats ? `${user.total_threats} threats` : `${user.total_requests} requests`}</span>
              </Link>
            ))}
            {!users.length ? <InlineEmpty label="Pass X-End-User-Id to populate attribution" /> : null}
          </div>
        </>
      )}
    </DataPanel>
  );
}

function RecentInvestigations({ threats, traces, loading }: { threats: ThreatEvent[]; traces: Trace[]; loading: boolean }) {
  const rows = useMemo(() => {
    const threatRows = threats.map((threat) => ({
      id: threat.id,
      time: threat.detected_at,
      kind: "Threat",
      label: threat.threat_type || threat.detector_name || "Security event",
      detail: threat.detector_name || "Detector",
      outcome: threat.action_taken || "detected",
      tone: threat.severity === "critical" || threat.severity === "high" ? "blocked" as const : "warn" as const,
      to: "/threats/$id" as const,
    }));
    const errorRows = traces.filter(isErrorTrace).map((trace) => ({
      id: trace.id,
      time: trace.completed_at,
      kind: "Error",
      label: trace.model || trace.path,
      detail: trace.provider || String(trace.http_status ?? "Request failure"),
      outcome: trace.http_status ? String(trace.http_status) : "error",
      tone: "warn" as const,
      to: "/traces/$id" as const,
    }));
    return [...threatRows, ...errorRows].sort((a, b) => new Date(b.time).getTime() - new Date(a.time).getTime()).slice(0, 6);
  }, [threats, traces]);

  return (
    <DataPanel title="Recent investigations" sub={rows.length ? `${rows.length} highest-signal events` : "No review queue"}>
      {loading ? <div className="p-3"><Skeleton className="h-40" /></div> : !rows.length ? <InlineEmpty label="No threats or request errors in this window" /> : (
        <DataTable headers={[["Time"], ["Type"], ["Signal"], ["Source"], ["Outcome", "right"]]}>
          {rows.map((row) => (
            <DataRow key={`${row.kind}:${row.id}`} rail={row.tone}>
              <DataCell mono>{formatRelativeTime(row.time)}</DataCell>
              <DataCell><Pill tone={row.kind === "Threat" ? "blocked" : "warn"}>{row.kind.toLowerCase()}</Pill></DataCell>
              <DataCell strong><Link to={row.to} params={{ id: row.id }} className="transition-colors hover:text-foreground">{humanize(row.label)}</Link></DataCell>
              <DataCell mono>{row.detail}</DataCell>
              <DataCell num><Pill tone={row.tone}>{row.outcome}</Pill></DataCell>
            </DataRow>
          ))}
        </DataTable>
      )}
    </DataPanel>
  );
}

function PanelMetric({ label, value, tone = "default" }: { label: string; value: string; tone?: "default" | "success" | "warning" | "danger" }) {
  return (
    <div className="min-w-0 bg-background p-3.5">
      <p className="text-[9px] font-medium uppercase tracking-widest text-muted-foreground">{label}</p>
      <p className={cn("mt-1.5 truncate font-mono text-lg font-semibold tabular-nums text-foreground", tone === "success" && "text-success", tone === "warning" && "text-warn", tone === "danger" && "text-danger")}>{value}</p>
    </div>
  );
}

function PanelLink({ to, label }: { to: "/threats" | "/traces" | "/analytics" | "/users"; label: string }) {
  return <Link to={to} className="inline-flex items-center gap-1 text-[10px] text-muted-foreground transition-colors hover:text-foreground">{label}<ArrowRight className="h-3 w-3" /></Link>;
}

function PanelSkeleton() {
  return <div className="grid grid-cols-2 gap-2 p-4 sm:grid-cols-4">{Array.from({ length: 4 }).map((_, index) => <Skeleton key={index} className="h-20" />)}</div>;
}

function InlineEmpty({ label }: { label: string }) {
  return <div className="flex min-h-28 items-center justify-center px-6 text-center text-[11px] text-muted-foreground">{label}</div>;
}

function EmptySurface({ icon: Icon, title, detail, to, action, className }: { icon: ComponentType<{ className?: string }>; title: string; detail: string; to: "/settings"; action: string; className?: string }) {
  return (
    <section className={cn("surface-card flex min-h-[214px] flex-col items-center justify-center px-6 text-center", className)}>
      <Icon className="h-5 w-5 text-muted-foreground" />
      <p className="mt-3 text-[12px] font-semibold text-foreground">{title}</p>
      <p className="mt-1 max-w-sm text-[10px] leading-relaxed text-muted-foreground">{detail}</p>
      <Link to={to} className="mt-3 inline-flex items-center gap-1 text-[10px] font-medium text-foreground">{action}<ArrowRight className="h-3 w-3" /></Link>
    </section>
  );
}

function OverviewRangeFilter({ value, onChange }: { value: RangePreset; onChange: (value: RangePreset) => void }) {
  return (
    <div className="space-y-3">
      <div>
        <p className="mb-1.5 text-[10px] font-medium text-muted-foreground">Time window</p>
        <RangeSelect value={value} onChange={onChange} full />
      </div>
      <p className="text-[10px] leading-relaxed text-muted-foreground">All operational metrics use the same window and compare against the preceding period.</p>
    </div>
  );
}

function RangeSelect({ value, onChange, full = false }: { value: RangePreset; onChange: (value: RangePreset) => void; full?: boolean }) {
  return (
    <Select value={value} onValueChange={(next) => onChange(next as RangePreset)}>
      <SelectTrigger className={cn("h-8 text-[10px]", full ? "w-full" : "w-36")}>
        <Clock3 className="h-3.5 w-3.5" />
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        <SelectItem value="1h">Last hour</SelectItem>
        <SelectItem value="24h">Last 24 hours</SelectItem>
        <SelectItem value="7d">Last 7 days</SelectItem>
        <SelectItem value="30d">Last 30 days</SelectItem>
      </SelectContent>
    </Select>
  );
}

function buildAttentionItems({ current, critical, allowed, errors, globalKeys, proxies }: { current: typeof EMPTY_ANALYTICS; critical: number; allowed: number; errors: number; globalKeys: number; proxies: Proxy[] }): AttentionItem[] {
  const items: AttentionItem[] = [];
  if (critical > 0) items.push({ title: `${critical} critical ${critical === 1 ? "threat" : "threats"}`, detail: "High-impact detections require review", action: "Investigate", to: "/threats", tone: "danger", icon: ShieldAlert });
  if (allowed > 0) items.push({ title: `${allowed} detected ${allowed === 1 ? "threat was" : "threats were"} allowed`, detail: "Validate detector strategy and thresholds", action: "Review policy", to: "/threats", tone: "warning", icon: AlertTriangle });
  if (errors > 0) items.push({ title: `${errors} failed ${errors === 1 ? "request" : "requests"}`, detail: "Inspect provider, model, and release context", action: "View traces", to: "/traces", tone: "warning", icon: Activity });
  if (globalKeys > 0) items.push({ title: `${globalKeys} globally scoped API ${globalKeys === 1 ? "key" : "keys"}`, detail: "Gateway scope reduces credential blast radius", action: "Restrict access", to: "/api-keys", tone: "warning", icon: KeyRound });
  const activeProxies = proxies.filter((proxy) => proxy.is_active).length;
  if (!proxies.length) items.push({ title: "No gateway configured", detail: "Create a secured provider route to start", action: "Create gateway", to: "/proxies", tone: "neutral", icon: Server });
  else if (!activeProxies) items.push({ title: "All gateways are disabled", detail: "Applications cannot route provider traffic", action: "Review gateways", to: "/proxies", tone: "warning", icon: Server });
  if (!current.total_requests && proxies.length && activeProxies) items.push({ title: "No traffic in this window", detail: "Verify the application is using its gateway URL", action: "Open quick start", to: "/settings", tone: "neutral", icon: Activity });
  return items;
}

function computeTraceMetrics(traces: Trace[], window: { from: string; to: string }) {
  const durations = traces.map((trace) => trace.duration_ms).filter(Number.isFinite).sort((a, b) => a - b);
  const percentile = (p: number) => durations.length ? durations[Math.min(durations.length - 1, Math.floor((durations.length - 1) * p))] ?? 0 : 0;
  const errorCount = traces.filter(isErrorTrace).length;
  const latencySeries = bucketAverage(traces, window, 24);
  const groups = new Map<string, { total: number; count: number }>();
  for (const trace of traces) {
    const label = `${trace.provider || "unknown"} · ${trace.model || "default"}`;
    const current = groups.get(label) ?? { total: 0, count: 0 };
    current.total += trace.duration_ms;
    current.count += 1;
    groups.set(label, current);
  }
  const slowest = Array.from(groups, ([label, value]) => ({ label, count: value.count, average: value.total / value.count })).sort((a, b) => b.average - a.average);
  return {
    count: traces.length,
    p50: percentile(0.5),
    p95: percentile(0.95),
    p99: percentile(0.99),
    errorCount,
    errorRate: traces.length ? Number(((errorCount / traces.length) * 100).toFixed(1)) : 0,
    latencySeries,
    slowest,
  };
}

function computeSecurityMetrics(threats: ThreatEvent[], totalBlocked: number, totalThreats: number) {
  const critical = threats.filter((threat) => threat.severity?.toLowerCase() === "critical").length;
  const allowed = threats.filter((threat) => threat.action_taken?.toLowerCase() !== "block").length;
  const affectedUsers = new Set(threats.map((threat) => threat.end_user_id).filter(Boolean)).size;
  return {
    critical,
    allowed,
    affectedUsers,
    preventionRate: totalThreats ? Math.round((totalBlocked / totalThreats) * 100) : 100,
  };
}

function makeActivitySeries(points: Array<{ hour: string; count: number }>, threats: ThreatEvent[], window: { from: string; to: string }, buckets: number) {
  const from = new Date(window.from).getTime();
  const to = new Date(window.to).getTime();
  const width = Math.max(1, (to - from) / buckets);
  const total = Array.from({ length: buckets }, () => 0);
  const blocked = Array.from({ length: buckets }, () => 0);
  const indexFor = (input: string) => Math.min(buckets - 1, Math.max(0, Math.floor((new Date(input).getTime() - from) / width)));
  for (const point of points) {
    const index = indexFor(point.hour);
    total[index] = (total[index] ?? 0) + point.count;
  }
  for (const threat of threats) {
    if (threat.action_taken?.toLowerCase() !== "block") continue;
    const index = indexFor(threat.detected_at);
    blocked[index] = (blocked[index] ?? 0) + 1;
  }
  return { total, blocked };
}

function bucketAverage(traces: Trace[], window: { from: string; to: string }, buckets: number): number[] {
  const from = new Date(window.from).getTime();
  const to = new Date(window.to).getTime();
  const width = Math.max(1, (to - from) / buckets);
  const totals = Array.from({ length: buckets }, () => 0);
  const counts = Array.from({ length: buckets }, () => 0);
  for (const trace of traces) {
    const index = Math.min(buckets - 1, Math.max(0, Math.floor((new Date(trace.started_at).getTime() - from) / width)));
    totals[index] = (totals[index] ?? 0) + trace.duration_ms;
    counts[index] = (counts[index] ?? 0) + 1;
  }
  return totals.map((total, index) => counts[index] ? Math.round(total / (counts[index] ?? 1)) : 0);
}

function makeWindow(range: RangePreset) {
  const duration = RANGE_META[range].durationMs;
  const to = new Date();
  const from = new Date(to.getTime() - duration);
  const previousTo = new Date(from);
  const previousFrom = new Date(previousTo.getTime() - duration);
  return {
    current: { from: from.toISOString(), to: to.toISOString() },
    previous: { from: previousFrom.toISOString(), to: previousTo.toISOString() },
  };
}

function describeDelta(current: number, previous: number, lowerIsBetter = false): string {
  if (!previous) return current ? "New activity" : "No prior activity";
  const delta = Math.round(((current - previous) / previous) * 100);
  if (delta === 0) return "No change";
  const direction = delta > 0 ? "up" : "down";
  const qualifier = lowerIsBetter ? (delta > 0 ? "slower" : "faster") : direction;
  return `${Math.abs(delta)}% ${qualifier} vs previous`;
}

function countGlobalKeys(keys: APIKey[]): number {
  return keys.filter((key) => key.is_active && !key.scopes.some((scope) => scope.startsWith("proxy:"))).length;
}

function isErrorTrace(trace: Trace): boolean {
  return trace.status?.toLowerCase() === "error" || Boolean(trace.error_message) || (trace.http_status ?? 0) >= 400;
}

function humanize(value: string): string {
  return value.replace(/[._-]+/g, " ").replace(/\b\w/g, (letter) => letter.toUpperCase());
}

function formatRelativeTime(input: string): string {
  const deltaSec = Math.max(0, Math.floor((Date.now() - new Date(input).getTime()) / 1000));
  if (deltaSec < 60) return `${deltaSec}s ago`;
  if (deltaSec < 3600) return `${Math.floor(deltaSec / 60)}m ago`;
  if (deltaSec < 86400) return `${Math.floor(deltaSec / 3600)}h ago`;
  return `${Math.floor(deltaSec / 86400)}d ago`;
}
