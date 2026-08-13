import { useEffect, useMemo, useState } from "react";
import * as DropdownMenu from "@radix-ui/react-dropdown-menu";
import { useQuery } from "@tanstack/react-query";
import { Link, useNavigate } from "@tanstack/react-router";
import {
  Activity,
  CheckCircle2,
  Check,
  ChevronDown,
  CircleX,
  Columns3,
  Download,
  Pause,
  Play,
  Search,
  ShieldAlert,
} from "lucide-react";

import { api, type Trace } from "@/api/client";
import { EmptyState } from "@/components/card";
import { useDashboardControls } from "@/components/dashboard-controls-context";
import { emptyFilters, parseTagFilter, type ObserveFilters } from "@/components/observe/filter-bar";
import { ObserveSidebarFilters } from "@/components/observe/observe-sidebar-filters";
import { TraceInspector } from "@/components/observe/trace-inspector";
import { WorkspaceSummaryStrip } from "@/components/observe/workspace-summary-strip";
import { SkeletonRows } from "@/components/skeleton";
import { useTracesExtension } from "@/components/traces-extension";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { useWorkspaceSidebar } from "@/components/workspace-sidebar-context";
import { downloadCSV } from "@/lib/csv";
import { cn, formatCost, formatDuration, formatNumber } from "@/lib/utils";

export function TracesPage() {
  const navigate = useNavigate();
  const controls = useDashboardControls();
  const [filters, setFilters] = useState<ObserveFilters>(emptyFilters);
  const [selected, setSelected] = useState<Trace | null>(null);
  const [inspectorDismissed, setInspectorDismissed] = useState(false);
  const [showEndUser, setShowEndUser] = useState(false);
  const [showEnvironment, setShowEnvironment] = useState(() => !controls.environment);
  const isWide = useWideViewport();

  const queryParams = useMemo(() => {
    const params: Record<string, string | number | string[]> = { limit: 100 };
    if (filters.status) params.status = filters.status;
    if (filters.provider) params.provider = filters.provider;
    if (filters.model) params.model = filters.model;
    if (filters.endUser) params.end_user_id = filters.endUser;
    if (filters.search) params.search = filters.search;
    params.from = controls.timeWindow.from;
    params.to = controls.timeWindow.to;
    if (controls.environment) params.environment = controls.environment;
    if (filters.release) params.release = filters.release;
    if (filters.traceName) params.trace_name = filters.traceName;
    const tags = parseTagFilter(filters.tags);
    if (tags.length) params.tag = tags;
    return params;
  }, [controls.environment, controls.timeWindow, filters]);

  const { data: gatewayTraces, isLoading } = useQuery({
    queryKey: ["traces", queryParams, controls.live],
    queryFn: () => api.traces.list(queryParams),
    refetchInterval: controls.live ? 10_000 : false,
  });

  const extension = useTracesExtension();
  const extensionQuery = useQuery({
    queryKey: ["traces:extension", filters, controls.environment, controls.range, controls.live],
    queryFn: () => extension.fetchExtra!(filters),
    enabled: typeof extension.fetchExtra === "function",
    refetchInterval: controls.live ? 10_000 : false,
  });

  const traces = useMemo<Trace[]>(() => {
    const primary = gatewayTraces ?? [];
    const extras = extensionQuery.data ?? [];
    if (!extras.length) return primary;
    return [...primary, ...extras].sort((a, b) => new Date(b.started_at).getTime() - new Date(a.started_at).getTime());
  }, [extensionQuery.data, gatewayTraces]);

  const filtered = useMemo(() => {
    if (filters.security === "threat") return traces.filter((trace) => trace.threat_detected);
    if (filters.security === "clean") return traces.filter((trace) => !trace.threat_detected);
    return traces;
  }, [filters.security, traces]);
  const kpis = useMemo(() => computeKpis(filtered), [filtered]);
  const failed = traces.filter((trace) => trace.status !== "ok").length;
  const threatened = traces.filter((trace) => trace.threat_detected).length;
  const clean = traces.length - threatened;

  const sidebarConfig = useMemo(
    () => ({
      parentLabel: "Observe",
      parentTo: "/",
      title: "Traces",
      activeLabel: "Trace explorer",
      activeIcon: Activity,
      timeLabel: controls.rangeLabel,
      views: [
        { label: "All traces", count: traces.length, icon: Activity, active: !filters.status && !filters.security, onClick: () => setFilters(emptyFilters) },
        { label: "Failed / blocked", count: failed, icon: CircleX, active: filters.status === "blocked", onClick: () => setFilters({ ...filters, status: "blocked" }) },
        { label: "Threat detected", count: threatened, icon: ShieldAlert, active: filters.security === "threat", onClick: () => setFilters({ ...filters, security: "threat" }) },
        { label: "Clean", count: clean, icon: CheckCircle2, active: filters.security === "clean", onClick: () => setFilters({ ...filters, security: "clean" }) },
      ],
      filters: <ObserveSidebarFilters value={filters} onChange={setFilters} environments={controls.environments} mode="traces" range={controls.range} onRangeChange={controls.setRange} environment={controls.environment} onEnvironmentChange={controls.setEnvironment} />,
    }),
    [clean, controls, failed, filters, threatened, traces.length],
  );
  useWorkspaceSidebar(sidebarConfig);

  useEffect(() => {
    if (isWide && !selected && !inspectorDismissed && filtered.length) setSelected(filtered[0]!);
  }, [filtered, inspectorDismissed, isWide, selected]);

  useEffect(() => {
    if (!isWide) {
      setSelected(null);
      setInspectorDismissed(true);
    }
  }, [isWide]);

  useEffect(() => {
    setShowEnvironment(!controls.environment);
  }, [controls.environment]);

  const openTrace = (trace: Trace) => {
    if (!isWide) {
      navigate({ to: "/traces/$id", params: { id: trace.id } });
      return;
    }
    setInspectorDismissed(false);
    setSelected(trace);
  };

  return (
    <div className="flex h-full min-h-0 bg-background">
      <section className="flex min-w-0 flex-1 flex-col">
        <WorkspaceSummaryStrip metrics={[
          { value: formatNumber(kpis.count), label: "traces", sub: "In current window" },
          { value: formatNumber(kpis.tokens), label: "tokens", sub: `${formatNumber(kpis.inputTokens)} in · ${formatNumber(kpis.outputTokens)} out` },
          { value: formatCost(kpis.cost), label: "cost", sub: `avg ${formatCost(kpis.count ? kpis.cost / kpis.count : 0)} / trace` },
          { value: formatNumber(kpis.threats), label: "threats", sub: `${kpis.count ? Math.round((kpis.threats / kpis.count) * 100) : 0}% of traces`, tone: kpis.threats ? "danger" : "success" },
        ]} />

        <div className="flex flex-wrap items-center gap-2 border-b border-border-subtle px-3 py-2">
          <div className="relative min-w-[240px] flex-1">
            <Search className="pointer-events-none absolute left-2.5 top-2 h-3.5 w-3.5 text-muted-foreground" />
            <Input value={filters.search} onChange={(event) => setFilters({ ...filters, search: event.target.value })} placeholder="Search traces…" className="pl-8 text-xs" />
          </div>
          <TraceColumnsMenu
            showEnvironment={showEnvironment}
            showEndUser={showEndUser}
            allEnvironments={!controls.environment}
            onEnvironmentChange={setShowEnvironment}
            onEndUserChange={setShowEndUser}
          />
          <Button variant="outline" size="icon-sm" aria-label="Export traces CSV" onClick={() => downloadCSV(`bastio-traces-${new Date().toISOString().slice(0, 10)}.csv`, filtered as unknown as Record<string, unknown>[])}><Download className="h-3.5 w-3.5" /></Button>
          <Button variant="outline" size="sm" className="h-8 text-[11px]" onClick={() => controls.setLive(!controls.live)}>{controls.live ? <Pause className="h-3.5 w-3.5" /> : <Play className="h-3.5 w-3.5" />} {controls.live ? "Pause" : "Live"}</Button>
        </div>

        <div className="min-h-0 flex-1 overflow-auto">
          {isLoading ? <SkeletonRows count={12} /> : !filtered.length ? <TracesEmptyState hasActiveFilters={Object.values(filters).some(Boolean)} rangeLabel={controls.rangeLabel} onExpandRange={() => controls.setRange("30d")} /> : (
            <Table className={cn("text-[11px]", showEnvironment ? "min-w-[920px]" : "min-w-[820px]")}>
              <TableHeader className="sticky top-0 z-10 bg-background/95 backdrop-blur">
                <TableRow className="border-border-subtle hover:bg-transparent">
                  <Th>Time</Th><Th>Name</Th>{showEnvironment ? <Th>Environment</Th> : null}<Th>Status</Th><Th>Security</Th><Th>Provider</Th><Th className="text-right">Latency</Th><Th className="text-right">Tokens</Th><Th className="text-right">Cost</Th>{showEndUser ? <Th>End user</Th> : null}
                </TableRow>
              </TableHeader>
              <TableBody>
                {filtered.map((trace) => (
                  <TableRow key={trace.id} onClick={() => openTrace(trace)} className={cn("cursor-pointer border-border-subtle hover:bg-surface-2/70", selected?.id === trace.id && "bg-surface-2", trace.threat_detected ? "border-l-2 border-l-danger" : "border-l-2 border-l-transparent")}>
                    <MonoCell title={new Date(trace.started_at).toLocaleString()}>{formatTimestamp(trace.started_at)}</MonoCell>
                    <MonoCell className="max-w-[220px] truncate text-foreground">{trace.trace_name || trace.path || trace.model}</MonoCell>
                    {showEnvironment ? <TableCell><EnvironmentBadge environment={trace.environment} /></TableCell> : null}
                    <TableCell><Badge variant={trace.status === "ok" ? "success" : trace.status === "blocked" ? "destructive" : "warning"} className="px-1.5 py-0 text-[9px]">{trace.status}</Badge></TableCell>
                    <TableCell>{trace.threat_detected ? <Badge variant="destructive" className="px-1.5 py-0 text-[9px]">{(trace.threat_types ?? []).slice(0, 1).join("") || "threat"}</Badge> : <Badge variant="outline" className="px-1.5 py-0 text-[9px] text-muted-foreground">clean</Badge>}</TableCell>
                    <TableCell className="text-[10px] text-muted-foreground"><span className="text-foreground/85">{trace.provider}</span> <span className="font-mono">{trace.model}</span></TableCell>
                    <MonoCell className="text-right">{formatDuration(trace.duration_ms)}</MonoCell>
                    <MonoCell className="text-right">{formatNumber((trace.input_tokens ?? 0) + (trace.output_tokens ?? 0))}</MonoCell>
                    <MonoCell className="text-right">{formatCost(trace.cost_cents)}</MonoCell>
                    {showEndUser ? <TableCell className="max-w-[140px] truncate text-[10px] text-muted-foreground">{trace.end_user_id ? <Link to="/users/$id" params={{ id: trace.end_user_id }} onClick={(event) => event.stopPropagation()}>{trace.end_user_id}</Link> : "—"}</TableCell> : null}
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </div>
        <div className="flex h-11 flex-shrink-0 items-center justify-between border-t border-border-subtle px-3 text-[10px] text-muted-foreground"><span><span className="font-mono text-foreground">{filtered.length}</span> results</span><span>Rows per page <span className="font-mono text-foreground">100</span></span></div>
      </section>

      {selected ? <TraceInspector trace={selected} onClose={() => { setInspectorDismissed(true); setSelected(null); }} /> : null}
    </div>
  );
}

function Th({ children, className }: { children: React.ReactNode; className?: string }) {
  return <TableHead className={cn("h-9 px-2 text-[9px] font-medium uppercase tracking-wider text-muted-foreground", className)}>{children}</TableHead>;
}

function MonoCell({ children, className, title }: { children: React.ReactNode; className?: string; title?: string }) {
  return <TableCell title={title} className={cn("h-9 px-2 py-1.5 font-mono text-[10px] tabular-nums text-muted-foreground", className)}>{children}</TableCell>;
}

function TraceColumnsMenu({
  showEnvironment,
  showEndUser,
  allEnvironments,
  onEnvironmentChange,
  onEndUserChange,
}: {
  showEnvironment: boolean;
  showEndUser: boolean;
  allEnvironments: boolean;
  onEnvironmentChange: (visible: boolean) => void;
  onEndUserChange: (visible: boolean) => void;
}) {
  return (
    <DropdownMenu.Root>
      <DropdownMenu.Trigger asChild>
        <Button variant="outline" size="sm" className="h-8 text-[11px]">
          <Columns3 className="h-3.5 w-3.5" /> Columns <ChevronDown className="h-3 w-3 text-muted-foreground" />
        </Button>
      </DropdownMenu.Trigger>
      <DropdownMenu.Portal>
        <DropdownMenu.Content align="end" sideOffset={5} className="z-50 w-64 rounded-lg border border-border-subtle bg-popover p-1.5 shadow-md">
          <div className="px-2 py-1.5">
            <p className="text-[10px] font-semibold uppercase tracking-widest text-muted-foreground">Visible columns</p>
            <p className="mt-0.5 text-[10px] leading-4 text-muted-foreground">Choose the context shown for every trace.</p>
          </div>
          <DropdownMenu.CheckboxItem
            checked={showEnvironment}
            onCheckedChange={(checked) => onEnvironmentChange(checked === true)}
            onSelect={(event) => event.preventDefault()}
            className="flex cursor-pointer items-start gap-2 rounded-md px-2 py-2 outline-none data-[highlighted]:bg-surface-2"
          >
            <span className="mt-0.5 flex h-4 w-4 items-center justify-center rounded border border-border-default bg-background">
              <DropdownMenu.ItemIndicator><Check className="h-3 w-3" /></DropdownMenu.ItemIndicator>
            </span>
            <span className="min-w-0 flex-1">
              <span className="flex items-center gap-2 text-xs font-medium text-foreground">
                Environment
                {allEnvironments ? <Badge variant="outline" className="px-1.5 py-0 text-[8px] font-medium uppercase tracking-wider">recommended</Badge> : null}
              </span>
              <span className="mt-0.5 block text-[10px] leading-4 text-muted-foreground">Deployment boundary attached to the trace.</span>
            </span>
          </DropdownMenu.CheckboxItem>
          <DropdownMenu.CheckboxItem
            checked={showEndUser}
            onCheckedChange={(checked) => onEndUserChange(checked === true)}
            onSelect={(event) => event.preventDefault()}
            className="flex cursor-pointer items-start gap-2 rounded-md px-2 py-2 outline-none data-[highlighted]:bg-surface-2"
          >
            <span className="mt-0.5 flex h-4 w-4 items-center justify-center rounded border border-border-default bg-background">
              <DropdownMenu.ItemIndicator><Check className="h-3 w-3" /></DropdownMenu.ItemIndicator>
            </span>
            <span className="min-w-0 flex-1">
              <span className="text-xs font-medium text-foreground">End user</span>
              <span className="mt-0.5 block text-[10px] leading-4 text-muted-foreground">Application user identity, when supplied.</span>
            </span>
          </DropdownMenu.CheckboxItem>
        </DropdownMenu.Content>
      </DropdownMenu.Portal>
    </DropdownMenu.Root>
  );
}

function EnvironmentBadge({ environment }: { environment?: string }) {
  if (!environment) return <span className="font-mono text-[10px] text-muted-foreground/60">unassigned</span>;
  const normalized = environment.toLowerCase();
  const dotClass = normalized.includes("prod")
    ? "bg-success"
    : normalized.includes("stag") || normalized.includes("preprod")
      ? "bg-warn"
      : normalized.includes("dev") || normalized.includes("local")
        ? "bg-info"
        : "bg-muted-foreground";
  return (
    <Badge variant="outline" className="max-w-[130px] gap-1.5 px-1.5 py-0 font-mono text-[9px] font-normal text-muted-foreground">
      <span className={cn("h-1.5 w-1.5 flex-shrink-0 rounded-full", dotClass)} />
      <span className="truncate">{environment}</span>
    </Badge>
  );
}

function computeKpis(traces: Trace[]) {
  let inputTokens = 0;
  let outputTokens = 0;
  let cost = 0;
  let threats = 0;
  for (const trace of traces) {
    inputTokens += trace.input_tokens ?? 0;
    outputTokens += trace.output_tokens ?? 0;
    cost += trace.cost_cents ?? 0;
    if (trace.threat_detected) threats += 1;
  }
  return { count: traces.length, tokens: inputTokens + outputTokens, inputTokens, outputTokens, cost, threats };
}

function TracesEmptyState({ hasActiveFilters, rangeLabel, onExpandRange }: { hasActiveFilters: boolean; rangeLabel: string; onExpandRange: () => void }) {
  return <EmptyState icon={<Activity className="h-6 w-6" />} title={hasActiveFilters ? "No traces match these filters" : `No traces in ${rangeLabel.toLowerCase()}`} description={hasActiveFilters ? "Try loosening or clearing the filters." : "This workspace may still contain older telemetry outside the selected time window."} action={!hasActiveFilters ? <Button variant="outline" size="sm" onClick={onExpandRange}>Search last 30 days</Button> : undefined} />;
}

function useWideViewport() {
  const [wide, setWide] = useState(() => typeof window !== "undefined" && window.matchMedia("(min-width: 1280px)").matches);
  useEffect(() => {
    const media = window.matchMedia("(min-width: 1280px)");
    const update = () => setWide(media.matches);
    update();
    media.addEventListener("change", update);
    return () => media.removeEventListener("change", update);
  }, []);
  return wide;
}

function formatTimestamp(iso: string) {
  return new Intl.DateTimeFormat(undefined, { month: "short", day: "2-digit", hour: "2-digit", minute: "2-digit", second: "2-digit" }).format(new Date(iso));
}
