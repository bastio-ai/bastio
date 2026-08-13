import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Activity, BarChart3, Clock3, ShieldAlert, TrendingUp, Users } from "lucide-react";
import { Area, AreaChart, ResponsiveContainer, Tooltip, XAxis, YAxis } from "recharts";

import { api, type UserAnalytics } from "@/api/client";
import { EmptyState } from "@/components/card";
import { dashboardRangeLabel, useDashboardControls, type DashboardRange } from "@/components/dashboard-controls-context";
import { DataPanel } from "@/components/data/data-panel";
import { WorkspaceSummaryStrip } from "@/components/observe/workspace-summary-strip";
import { Skeleton } from "@/components/skeleton";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { useWorkspaceSidebar } from "@/components/workspace-sidebar-context";
import { formatCost, formatDuration, formatNumber } from "@/lib/utils";

type UserSort = "cost" | "requests" | "threats" | "latency";
type RangePreset = DashboardRange;
type AnalyticsView = "traffic" | "security" | "users";

const rangeLabels: Record<RangePreset, string> = { "1h": "Last hour", "24h": "Last 24 hours", "7d": "Last 7 days", "30d": "Last 30 days" };

export function AnalyticsPage() {
  const { range, setRange, environment, timeWindow } = useDashboardControls();
  const [view, setView] = useState<AnalyticsView>("traffic");
  const queryParams = useMemo(() => ({ ...timeWindow, environment: environment || undefined }), [environment, timeWindow]);
  const { data, isLoading } = useQuery({ queryKey: ["analytics", queryParams], queryFn: () => api.analytics.overview(queryParams) });

  const d = data ?? { total_requests: 0, total_threats: 0, total_blocked: 0, total_cost_cents: 0, avg_duration_ms: 0, requests_by_hour: [], threats_by_type: [], top_models: [] };
  const sidebarConfig = useMemo(() => ({
    parentLabel: "Observe",
    parentTo: "/",
    title: "Analytics",
    activeLabel: "Usage analytics",
    activeIcon: BarChart3,
    timeLabel: rangeLabels[range],
    views: [
      { label: "Traffic & cost", count: formatNumber(d.total_requests), icon: Activity, active: view === "traffic", onClick: () => setView("traffic" as const) },
      { label: "Security analytics", count: formatNumber(d.total_threats), icon: ShieldAlert, active: view === "security", onClick: () => setView("security" as const) },
      { label: "End-user usage", icon: Users, active: view === "users", onClick: () => setView("users" as const) },
    ],
    filtersLabel: "Time window",
    filters: <RangeFilter value={range} onChange={setRange} />,
  }), [d.total_requests, d.total_threats, range, view]);
  useWorkspaceSidebar(sidebarConfig);

  return (
    <div className="flex h-full min-h-0 flex-col bg-background">
      <WorkspaceSummaryStrip metrics={[
        { value: formatNumber(d.total_requests), label: "requests", sub: rangeLabels[range] },
        { value: formatNumber(d.total_threats), label: "threats", sub: `${formatNumber(d.total_blocked)} blocked`, tone: d.total_threats ? "danger" : "success" },
        { value: formatCost(d.total_cost_cents), label: "cost", sub: d.total_requests ? `avg ${formatCost(d.total_cost_cents / d.total_requests)} / request` : "No usage" },
        { value: formatDuration(d.avg_duration_ms), label: "avg latency", sub: "Across all providers" },
      ]} />

      <div className="flex h-12 flex-shrink-0 items-center justify-between border-b border-border-subtle px-4">
        <div>
          <h1 className="text-[13px] font-semibold text-foreground">{view === "traffic" ? "Traffic & cost" : view === "security" ? "Security analytics" : "End-user usage"}</h1>
          <p className="text-[9px] text-muted-foreground">{rangeLabels[range]} · live gateway data</p>
        </div>
        <RangeSelect value={range} onChange={setRange} />
      </div>

      <div className="min-h-0 flex-1 overflow-auto p-3">
        {isLoading ? <AnalyticsSkeleton /> : view === "traffic" ? <TrafficView data={d} /> : view === "security" ? <SecurityView data={d} /> : <TopUsersSection />}
      </div>
    </div>
  );
}

function TrafficView({ data: d }: { data: NonNullable<Awaited<ReturnType<typeof api.analytics.overview>>> }) {
  return (
    <div className="grid gap-3 xl:grid-cols-[minmax(0,1.7fr)_minmax(280px,0.8fr)]">
      <DataPanel title="Requests over time" sub="Gateway request volume" className="min-h-[310px]">
        {d.requests_by_hour.length === 0 ? <EmptyState icon={<Activity className="h-5 w-5" />} title="No request activity" description="Send a request through the gateway to populate this chart." /> : (
          <div className="h-[270px] p-4">
            <ResponsiveContainer width="100%" height="100%"><AreaChart data={d.requests_by_hour}>
              <defs><linearGradient id="analytics-request-gradient" x1="0" y1="0" x2="0" y2="1"><stop offset="0%" stopColor="var(--text-secondary)" stopOpacity={0.28} /><stop offset="100%" stopColor="var(--text-secondary)" stopOpacity={0} /></linearGradient></defs>
              <XAxis dataKey="hour" tickFormatter={(value) => new Date(value).toLocaleString([], { month: "short", day: "numeric", hour: "numeric" })} stroke="var(--text-muted)" strokeOpacity={0.25} tick={{ fill: "var(--text-muted)", fontSize: 9, fontFamily: "var(--font-mono)" }} />
              <YAxis stroke="var(--text-muted)" strokeOpacity={0.25} tick={{ fill: "var(--text-muted)", fontSize: 9, fontFamily: "var(--font-mono)" }} allowDecimals={false} />
              <Tooltip contentStyle={{ background: "var(--surface-3)", border: "1px solid var(--border-default)", borderRadius: "6px", fontSize: "11px", fontFamily: "var(--font-mono)" }} labelFormatter={(value) => typeof value === "string" || typeof value === "number" ? new Date(value).toLocaleString() : ""} />
              <Area type="monotone" dataKey="count" stroke="var(--text-secondary)" strokeWidth={1} fill="url(#analytics-request-gradient)" />
            </AreaChart></ResponsiveContainer>
          </div>
        )}
      </DataPanel>
      <TopModels models={d.top_models} />
    </div>
  );
}

function SecurityView({ data: d }: { data: NonNullable<Awaited<ReturnType<typeof api.analytics.overview>>> }) {
  const preventionRate = d.total_threats ? Math.round((d.total_blocked / d.total_threats) * 100) : 0;
  return (
    <div className="grid gap-3 xl:grid-cols-[minmax(0,1.5fr)_minmax(280px,0.8fr)]">
      <DataPanel title="Threats by type" sub="Relative event volume" className="min-h-[320px]">
        {d.threats_by_type.length === 0 ? <EmptyState icon={<ShieldAlert className="h-5 w-5" />} title="No threat data" description="Threat distribution appears after the security engine detects an event." /> : (
          <div className="space-y-4 p-4">{d.threats_by_type.map((item) => {
            const max = Math.max(...d.threats_by_type.map((candidate) => candidate.count));
            const width = max ? `${(item.count / max) * 100}%` : "0%";
            return <div key={item.type}><div className="mb-1.5 flex justify-between text-[11px]"><span>{item.type}</span><span className="font-mono text-muted-foreground">{item.count}</span></div><div className="h-1.5 overflow-hidden rounded-full bg-surface-2"><div className="h-full rounded-full bg-danger" style={{ width }} /></div></div>;
          })}</div>
        )}
      </DataPanel>
      <DataPanel title="Security outcome" sub="Current time window">
        <div className="grid grid-cols-2 gap-px bg-border-subtle">
          <SecurityStat label="Detected" value={formatNumber(d.total_threats)} />
          <SecurityStat label="Blocked" value={formatNumber(d.total_blocked)} />
          <SecurityStat label="Prevention rate" value={`${preventionRate}%`} />
          <SecurityStat label="Allowed" value={formatNumber(Math.max(0, d.total_threats - d.total_blocked))} />
        </div>
      </DataPanel>
    </div>
  );
}

function SecurityStat({ label, value }: { label: string; value: string }) { return <div className="bg-background p-4"><div className="font-mono text-lg font-medium tabular-nums text-foreground">{value}</div><div className="mt-1 text-[10px] text-muted-foreground">{label}</div></div>; }

function TopModels({ models }: { models: Array<{ model: string; count: number; cost_cents: number }> }) {
  return <DataPanel title="Top models" sub="Requests and spend">{models.length === 0 ? <EmptyState icon={<TrendingUp className="h-5 w-5" />} title="No model data" description="Model usage appears after requests are processed." /> : <div className="p-2">{models.map((model, index) => <div key={model.model} className="grid grid-cols-[1.5rem_1fr_auto] items-center gap-2 rounded-md px-2 py-2.5 text-[10px] hover:bg-surface-2"><span className="font-mono text-muted-foreground">{index + 1}</span><span className="truncate font-mono text-[11px] text-foreground">{model.model}</span><span className="text-right font-mono tabular-nums text-muted-foreground">{model.count} req · {formatCost(model.cost_cents)}</span></div>)}</div>}</DataPanel>;
}

function TopUsersSection() {
  const [sort, setSort] = useState<UserSort>("cost");
  const { data: users = [], isLoading } = useQuery({ queryKey: ["analytics", "users", { sort }], queryFn: () => api.analytics.users({ limit: 20, order_by: sort }) });
  return <DataPanel title="Top end-users" sub="Cost, performance, and security attribution" action={<Select value={sort} onValueChange={(value) => setSort(value as UserSort)}><SelectTrigger className="h-7 w-36 text-[10px]"><SelectValue /></SelectTrigger><SelectContent><SelectItem value="cost">Sort by cost</SelectItem><SelectItem value="requests">Sort by requests</SelectItem><SelectItem value="threats">Sort by threats</SelectItem><SelectItem value="latency">Sort by latency</SelectItem></SelectContent></Select>}>
    {isLoading ? <div className="p-4"><Skeleton className="h-56" /></div> : users.length === 0 ? <EmptyState icon={<Users className="h-5 w-5" />} title="No end-user activity" description="Pass X-End-User-Id on gateway requests to populate attribution." /> : <div className="overflow-auto"><div className="min-w-[720px]"><div className="grid grid-cols-[1.5rem_1fr_6rem_6rem_6rem_6rem] gap-3 border-b border-border-subtle px-3 py-2 text-[9px] uppercase tracking-wider text-muted-foreground"><span>#</span><span>End user</span><span className="text-right">Requests</span><span className="text-right">Cost</span><span className="text-right">Latency</span><span className="text-right">Threats</span></div>{users.map((user: UserAnalytics, index) => <div key={user.end_user_id} className="grid grid-cols-[1.5rem_1fr_6rem_6rem_6rem_6rem] items-center gap-3 border-b border-border-subtle px-3 py-2.5 font-mono text-[10px] hover:bg-surface-2"><span className="text-muted-foreground">{index + 1}</span><span className="truncate text-foreground">{user.end_user_id}</span><span className="text-right">{formatNumber(user.total_requests)}</span><span className="text-right">{formatCost(user.total_cost_cents)}</span><span className="text-right">{formatDuration(user.avg_duration_ms)}</span><span className={user.total_threats ? "text-right text-danger" : "text-right text-muted-foreground"}>{user.total_threats || "—"}</span></div>)}</div></div>}
  </DataPanel>;
}

function RangeFilter({ value, onChange }: { value: RangePreset; onChange: (value: RangePreset) => void }) { return <div className="space-y-2"><label className="text-[9px] font-medium text-muted-foreground">Time window</label><RangeSelect value={value} onChange={onChange} full /></div>; }
function RangeSelect({ value, onChange, full = false }: { value: RangePreset; onChange: (value: RangePreset) => void; full?: boolean }) { return <Select value={value} onValueChange={(next) => onChange(next as RangePreset)}><SelectTrigger className={full ? "h-8 w-full text-[10px]" : "h-8 w-36 text-[10px]"}><Clock3 className="h-3.5 w-3.5" /><SelectValue>{dashboardRangeLabel(value)}</SelectValue></SelectTrigger><SelectContent><SelectItem value="1h">Last hour</SelectItem><SelectItem value="24h">Last 24 hours</SelectItem><SelectItem value="7d">Last 7 days</SelectItem><SelectItem value="30d">Last 30 days</SelectItem></SelectContent></Select>; }
function AnalyticsSkeleton() { return <div className="grid gap-3 xl:grid-cols-[minmax(0,1.7fr)_minmax(280px,0.8fr)]"><Skeleton className="h-[310px]" /><Skeleton className="h-[310px]" /></div>; }
