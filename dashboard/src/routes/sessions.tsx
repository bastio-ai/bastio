import { useEffect, useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { Activity, CheckCircle2, Search, ShieldAlert } from "lucide-react";

import { api, type Session } from "@/api/client";
import { EmptyState } from "@/components/card";
import { useDashboardControls } from "@/components/dashboard-controls-context";
import { emptyFilters, type ObserveFilters } from "@/components/observe/filter-bar";
import { ObserveSidebarFilters } from "@/components/observe/observe-sidebar-filters";
import { SessionInspector } from "@/components/observe/session-inspector";
import { WorkspaceSummaryStrip } from "@/components/observe/workspace-summary-strip";
import { SkeletonRows } from "@/components/skeleton";
import { Badge } from "@/components/ui/badge";
import { Input } from "@/components/ui/input";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { useWorkspaceSidebar } from "@/components/workspace-sidebar-context";
import { cn, formatCost, formatDuration, formatNumber } from "@/lib/utils";

export function SessionsPage() {
  const navigate = useNavigate();
  const controls = useDashboardControls();
  const [filters, setFilters] = useState<ObserveFilters>(emptyFilters);
  const [selected, setSelected] = useState<Session | null>(null);
  const [inspectorDismissed, setInspectorDismissed] = useState(false);
  const isWide = useWideViewport();

  const queryParams = useMemo(() => {
    const params: Record<string, string | number> = { limit: 100 };
    if (filters.endUser) params.end_user_id = filters.endUser;
    if (filters.search) params.search = filters.search;
    params.from = controls.timeWindow.from;
    params.to = controls.timeWindow.to;
    if (controls.environment) params.environment = controls.environment;
    return params;
  }, [controls.environment, controls.timeWindow, filters]);

  const { data: sessions = [], isLoading } = useQuery({ queryKey: ["sessions", queryParams], queryFn: () => api.sessions.list(queryParams) });
  const visibleSessions = useMemo(() => {
    if (filters.security === "threat") return sessions.filter((session) => (session.threat_count ?? 0) > 0);
    if (filters.security === "clean") return sessions.filter((session) => !session.threat_count);
    return sessions;
  }, [filters.security, sessions]);
  const kpis = useMemo(() => computeKpis(visibleSessions), [visibleSessions]);
  const withThreats = sessions.filter((session) => (session.threat_count ?? 0) > 0).length;
  const clean = sessions.length - withThreats;

  const sidebarConfig = useMemo(() => ({
    parentLabel: "Observe",
    parentTo: "/",
    title: "Sessions",
    activeLabel: "Session explorer",
    activeIcon: Activity,
    timeLabel: controls.rangeLabel,
    views: [
      { label: "All sessions", count: sessions.length, icon: Activity, active: !filters.security, onClick: () => setFilters(emptyFilters) },
      { label: "Needs review", count: withThreats, icon: ShieldAlert, active: filters.security === "threat", onClick: () => setFilters({ ...filters, security: "threat" }) },
      { label: "Clean", count: clean, icon: CheckCircle2, active: filters.security === "clean", onClick: () => setFilters({ ...filters, security: "clean" }) },
    ],
    filters: <ObserveSidebarFilters value={filters} onChange={setFilters} environments={controls.environments} mode="sessions" range={controls.range} onRangeChange={controls.setRange} environment={controls.environment} onEnvironmentChange={controls.setEnvironment} />,
  }), [clean, controls, filters, sessions.length, withThreats]);
  useWorkspaceSidebar(sidebarConfig);

  useEffect(() => {
    if (isWide && !selected && !inspectorDismissed && visibleSessions.length) setSelected(visibleSessions[0]!);
  }, [inspectorDismissed, isWide, selected, visibleSessions]);
  useEffect(() => {
    if (!isWide) { setSelected(null); setInspectorDismissed(true); }
  }, [isWide]);

  const openSession = (session: Session) => {
    if (!isWide) { navigate({ to: "/sessions/$id", params: { id: session.id } }); return; }
    setInspectorDismissed(false);
    setSelected(session);
  };

  return (
    <div className="flex h-full min-h-0 bg-background">
      <section className="flex min-w-0 flex-1 flex-col">
        <WorkspaceSummaryStrip metrics={[
          { value: formatNumber(visibleSessions.length), label: "sessions", sub: "In current window" },
          { value: visibleSessions.length ? (kpis.traces / visibleSessions.length).toFixed(1) : "0", label: "avg traces", sub: `${formatNumber(kpis.traces)} traces total` },
          { value: formatCost(kpis.cost), label: "cost", sub: visibleSessions.length ? `avg ${formatCost(kpis.cost / visibleSessions.length)} / session` : "No usage" },
          { value: formatNumber(kpis.threats), label: "threats", sub: kpis.threats ? "Sessions need review" : "No threats detected", tone: kpis.threats ? "danger" : "success" },
        ]} />

        <div className="flex items-center gap-2 border-b border-border-subtle px-3 py-2">
          <div className="relative flex-1"><Search className="pointer-events-none absolute left-2.5 top-2 h-3.5 w-3.5 text-muted-foreground" /><Input value={filters.search} onChange={(event) => setFilters({ ...filters, search: event.target.value })} placeholder="Search sessions…" className="pl-8 text-xs" /></div>
          <span className="hidden text-[10px] text-muted-foreground sm:inline">Grouped by <span className="font-mono text-foreground">X-Session-Id</span></span>
        </div>

        <div className="min-h-0 flex-1 overflow-auto">
          {isLoading ? <SkeletonRows count={12} /> : !visibleSessions.length ? <EmptyState icon={<Activity className="h-6 w-6" />} title={Object.values(filters).some(Boolean) ? "No sessions match these filters" : "No sessions yet"} description={Object.values(filters).some(Boolean) ? "Try loosening or clearing the filters." : "Pass X-Session-Id on gateway requests to group traces into sessions."} /> : (
            <Table className="min-w-[780px] text-[11px]">
              <TableHeader className="sticky top-0 z-10 bg-background/95 backdrop-blur"><TableRow className="border-border-subtle hover:bg-transparent"><Th>Session</Th><Th>End user</Th><Th className="text-right">Traces</Th><Th>Security</Th><Th className="text-right">Tokens</Th><Th className="text-right">Cost</Th><Th className="text-right">Wall clock</Th><Th className="text-right">Last activity</Th></TableRow></TableHeader>
              <TableBody>{visibleSessions.map((session) => (
                <TableRow key={session.id} onClick={() => openSession(session)} className={cn("cursor-pointer border-border-subtle hover:bg-surface-2/70", selected?.id === session.id && "bg-surface-2", session.threat_count ? "border-l-2 border-l-danger" : "border-l-2 border-l-transparent")}>
                  <MonoCell className="max-w-[220px] truncate text-foreground">{session.id}</MonoCell>
                  <TableCell className="text-[10px] text-muted-foreground">{session.end_user_id || "—"}</TableCell>
                  <MonoCell className="text-right">{formatNumber(session.trace_count)}</MonoCell>
                  <TableCell>{session.threat_count ? <Badge variant="destructive" className="px-1.5 py-0 text-[9px]">{session.threat_count} threats</Badge> : <Badge variant="outline" className="px-1.5 py-0 text-[9px] text-muted-foreground">clean</Badge>}</TableCell>
                  <MonoCell className="text-right">{formatNumber(session.total_tokens ?? 0)}</MonoCell><MonoCell className="text-right">{formatCost(session.total_cost_cents ?? 0)}</MonoCell><MonoCell className="text-right">{formatDuration(session.wall_clock_ms ?? 0)}</MonoCell><MonoCell className="text-right">{formatTimestamp(session.last_completed_at)}</MonoCell>
                </TableRow>
              ))}</TableBody>
            </Table>
          )}
        </div>
        <div className="flex h-11 flex-shrink-0 items-center justify-between border-t border-border-subtle px-3 text-[10px] text-muted-foreground"><span><span className="font-mono text-foreground">{visibleSessions.length}</span> sessions</span><span>Rows per page <span className="font-mono text-foreground">100</span></span></div>
      </section>
      {selected ? <SessionInspector session={selected} onClose={() => { setInspectorDismissed(true); setSelected(null); }} /> : null}
    </div>
  );
}

function Th({ children, className }: { children: React.ReactNode; className?: string }) { return <TableHead className={cn("h-9 px-2 text-[9px] font-medium uppercase tracking-wider text-muted-foreground", className)}>{children}</TableHead>; }
function MonoCell({ children, className }: { children: React.ReactNode; className?: string }) { return <TableCell className={cn("h-9 px-2 py-1.5 font-mono text-[10px] tabular-nums text-muted-foreground", className)}>{children}</TableCell>; }

function computeKpis(sessions: Session[]) {
  let traces = 0; let cost = 0; let threats = 0;
  for (const session of sessions) { traces += session.trace_count ?? 0; cost += session.total_cost_cents ?? 0; threats += session.threat_count ?? 0; }
  return { traces, cost, threats };
}

function useWideViewport() {
  const [wide, setWide] = useState(() => typeof window !== "undefined" && window.matchMedia("(min-width: 1280px)").matches);
  useEffect(() => { const media = window.matchMedia("(min-width: 1280px)"); const update = () => setWide(media.matches); update(); media.addEventListener("change", update); return () => media.removeEventListener("change", update); }, []);
  return wide;
}

function formatTimestamp(iso: string) { return new Intl.DateTimeFormat(undefined, { month: "short", day: "2-digit", hour: "2-digit", minute: "2-digit" }).format(new Date(iso)); }
