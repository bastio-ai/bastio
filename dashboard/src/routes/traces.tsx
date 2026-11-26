import { ObserveSidebarFilters } from "@/components/observe/observe-sidebar-filters";
import { securityDecision } from "@/components/observe/security-decision";
import { useCallback, useEffect, useMemo, useState } from "react";
import * as DropdownMenu from "@radix-ui/react-dropdown-menu";
import { useQuery } from "@tanstack/react-query";
import { Link, useNavigate, useSearch } from "@tanstack/react-router";
import {
  Activity,
  ArrowDownUp,
  CheckCircle2,
  Check,
  ChevronDown,
  CircleX,
  Columns3,
  Download,
  SlidersHorizontal,
  Search,
  ShieldAlert,
} from "lucide-react";

import { api, type Trace } from "@/api/client";
import { EmptyState } from "@/components/card";
import { useDashboardControls } from "@/components/dashboard-controls-context";
import { emptyFilters, parseTagFilter, type ObserveFilters } from "@/components/observe/filter-bar";
import { matchesTraceFilters, mergeTraceRows, traceFiltersFromSearch, traceFiltersToSearch, TRACE_TYPES, traceType } from "@/components/observe/trace-filters";
import { TraceFilterPanel, TraceFilterChips } from "@/components/observe/trace-filter-panel";
import { DashboardRefreshControl } from "@/components/dashboard-refresh-control";
import { TraceStatus } from "@/components/observe/trace-status";
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
  const search = useSearch({ from: "/traces" });
  const filters = useMemo(() => traceFiltersFromSearch(search), [search]);
  const [filtersOpen, setFiltersOpen] = useState(false);
  const [sort, setSort] = useState<"started_at" | "duration_ms" | "cost_cents">("started_at");
  const [descending, setDescending] = useState(true);
  const [searchInput, setSearchInput] = useState(filters.search);
  const setFilters = useCallback((next: ObserveFilters) => {
    void navigate({ to: "/traces", search: traceFiltersToSearch(next), replace: true });
    setSelected(null);
    setInspectorDismissed(false);
  }, [navigate]);
  useEffect(() => setSearchInput(filters.search), [filters.search]);
  useEffect(() => {
    if (searchInput === filters.search) return;
    const timer = window.setTimeout(() => setFilters({ ...filters, search: searchInput }), 350);
    return () => window.clearTimeout(timer);
  }, [filters, searchInput, setFilters]);
  const [selected, setSelected] = useState<Trace | null>(null);
  const [inspectorDismissed, setInspectorDismissed] = useState(false);
  const [showEndUser, setShowEndUser] = useState(false);
  const [showEnvironment, setShowEnvironment] = useState(() => !controls.environment);
  const isWide = useWideViewport();

  const queryParams = useMemo(() => {
    const params: NonNullable<Parameters<typeof api.traces.list>[0]> = { limit: 100 };
    if (filters.status) params.status = filters.status;
    if (filters.provider) params.provider = filters.provider;
    if (filters.model) params.model = filters.model;
    if (filters.endUser) params.end_user_id = filters.endUser;
    if (filters.search) params.search = filters.search;
    params.from = filters.from || controls.timeWindow.from;
    params.to = filters.to || controls.timeWindow.to;
    if (filters.environment || controls.environment) params.environment = filters.environment || controls.environment;
    if (filters.release) params.release = filters.release;
    if (filters.traceName) params.trace_name_contains = filters.traceName;
    if (filters.traceType) params.trace_type = TRACE_TYPES.find((kind) => kind.value === filters.traceType)?.value;
    if (filters.security === "clean" || filters.security === "threat") params.security = filters.security;
    const tags = parseTagFilter(filters.tags);
    if (tags.length) params.tag = tags;
    return params;
  }, [controls.environment, controls.timeWindow, filters]);

  const { data: gatewayTraces, isLoading, isFetching, isError, isPlaceholderData, refetch } = useQuery({
    queryKey: ["traces", queryParams],
    queryFn: () => api.traces.list(queryParams),
    refetchOnWindowFocus: false,
    placeholderData: (previous) => previous,
    meta: { dashboardLive: Boolean(filters.from && filters.to) },
  });

  const facetParams = useMemo(() => ({ from: queryParams.from, to: queryParams.to, environment: queryParams.environment }), [queryParams.from, queryParams.to, queryParams.environment]);
  const facets = useQuery({ queryKey: ["trace-facets", facetParams], queryFn: () => api.traces.facets(facetParams), refetchOnWindowFocus: false, placeholderData: (previous) => previous, staleTime: 60_000 });
  const effectiveFilters = useMemo(() => ({ ...filters, from: queryParams.from || "", to: queryParams.to || "", environment: queryParams.environment || "" }), [filters, queryParams]);
  const extension = useTracesExtension();
  const extensionQuery = useQuery({
    queryKey: ["traces:extension", effectiveFilters],
    queryFn: () => extension.fetchExtra!(effectiveFilters),
    enabled: typeof extension.fetchExtra === "function",
    refetchOnWindowFocus: false,
    placeholderData: (previous) => previous,
    meta: { dashboardLive: Boolean(filters.from && filters.to) },
  });

  const traces = useMemo<Trace[]>(() => {
    const primary = gatewayTraces ?? [];
    const extras = extensionQuery.data ?? [];
    return mergeTraceRows(primary, extras);
  }, [extensionQuery.data, gatewayTraces]);

  const filtered = useMemo(() => traces.filter((trace) => matchesTraceFilters(trace, effectiveFilters)).slice(0, 100), [effectiveFilters, traces]);
  const kpis = useMemo(() => computeKpis(filtered), [filtered]);
  const sorted = useMemo(() => [...filtered].sort((a, b) => {
    const delta = sort === "started_at" ? Date.parse(a.started_at) - Date.parse(b.started_at) : (a[sort] ?? 0) - (b[sort] ?? 0);
    return descending ? -delta : delta;
  }), [filtered, sort, descending]);
  const sortBy = (field: typeof sort) => { setDescending(sort === field ? !descending : true); setSort(field); };
  const traceTypeTabs = [{ value: "", label: "All traces" }, ...TRACE_TYPES];


  const sidebarConfig = useMemo(
    () => ({
      parentLabel: "Observe",
      parentTo: "/",
      title: "Traces",
      activeLabel: "Trace explorer",
      activeIcon: Activity,
      timeLabel: controls.rangeLabel,
      views: [
        { label: "All traces", icon: Activity, active: !Object.values(filters).some(Boolean), onClick: () => setFilters(emptyFilters) },
        { label: "Failed / blocked", icon: CircleX, active: filters.status === "error,blocked,rate_limited", onClick: () => setFilters({ ...filters, status: "error,blocked,rate_limited" }) },
        { label: "Security signals", icon: ShieldAlert, active: filters.security === "threat", onClick: () => setFilters({ ...filters, security: "threat" }) },
        { label: "No signals", icon: CheckCircle2, active: filters.security === "clean", onClick: () => setFilters({ ...filters, security: "clean" }) },
      ],
      filtersLabel: "Scope",
      filters: <ObserveSidebarFilters value={filters} onChange={setFilters} environments={controls.environments} mode="traces" scopeOnly range={controls.range} onRangeChange={controls.setRange} environment={controls.environment} onEnvironmentChange={controls.setEnvironment} />,
    }),
    [controls, filters, setFilters],
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
        <header className="flex flex-shrink-0 items-center justify-between gap-4 px-5 pb-1 pt-6">
          <div>
            <h1 className="text-[25px] font-medium leading-tight tracking-tight">Traces</h1>
            <p className="mt-1.5 text-xs text-muted-foreground">Inspect requests, security decisions, and provider usage.</p>
          </div>
          <div className="xl:hidden"><DashboardRefreshControl /></div>
        </header>
        <WorkspaceSummaryStrip metrics={[
          { value: formatNumber(kpis.count), label: "Requests", sub: "Latest matching results" },
          { value: formatNumber(kpis.threats), label: "Security signals", sub: `${kpis.blocked} blocked · ${kpis.errors} errors`, tone: kpis.threats ? "warning" : undefined },
          { value: kpis.count ? formatDuration(kpis.avgLatency) : "—", label: "Avg. latency", sub: "Across displayed requests" },
          { value: formatNumber(kpis.tokens), label: "Tokens", sub: `${formatNumber(kpis.inputTokens)} in · ${formatNumber(kpis.outputTokens)} out` },
          { value: formatCost(kpis.cost), label: "Provider cost", sub: "Across displayed requests" },
        ]} />
        <div role="tablist" aria-label="Trace type" className="flex min-h-11 flex-shrink-0 gap-5 overflow-x-auto border-b border-border-subtle px-5">
          {traceTypeTabs.map((kind, index) => <button key={kind.value} id={`trace-type-${kind.value || "all"}`} role="tab" aria-selected={filters.traceType === kind.value} aria-controls="trace-results" tabIndex={filters.traceType === kind.value ? 0 : -1} onClick={() => setFilters({ ...filters, traceType: kind.value })} onKeyDown={(event) => {
            const offset = event.key === "ArrowRight" ? 1 : event.key === "ArrowLeft" ? -1 : 0;
            if (offset) { event.preventDefault(); const next = traceTypeTabs[(index + offset + traceTypeTabs.length) % traceTypeTabs.length]!; setFilters({ ...filters, traceType: next.value }); document.getElementById(`trace-type-${next.value || "all"}`)?.focus(); }
          }} className={cn("whitespace-nowrap border-b-2 border-transparent py-3 text-[11px] text-muted-foreground hover:text-foreground", filters.traceType === kind.value && "border-foreground text-foreground")}>{kind.label}</button>)}
        </div>

        <div className="flex flex-wrap items-center gap-2 border-b border-border-subtle px-5 py-3">
          <div className="relative min-w-[200px] max-w-[470px] flex-1">
            <Search className="pointer-events-none absolute left-2.5 top-2 h-3.5 w-3.5 text-muted-foreground" />
            <Input aria-label="Search trace names, routes or IDs" value={searchInput} onChange={(event) => setSearchInput(event.target.value)} placeholder="Search names, routes or trace IDs…" className="pl-8 text-xs" />
          </div>
          <Button variant="outline" size="sm" aria-expanded={filtersOpen} aria-controls="trace-filters-panel" onClick={() => setFiltersOpen(!filtersOpen)}><SlidersHorizontal className="size-3.5" /> Filters {Object.values(filters).filter(Boolean).length || ""}</Button>
          <TraceColumnsMenu
            showEnvironment={showEnvironment}
            showEndUser={showEndUser}
            allEnvironments={!controls.environment}
            onEnvironmentChange={setShowEnvironment}
            onEndUserChange={setShowEndUser}
          />
          <Button variant="outline" size="icon-sm" aria-label="Export traces CSV" onClick={() => downloadCSV(`bastio-traces-${new Date().toISOString().slice(0, 10)}.csv`, sorted as unknown as Record<string, unknown>[])}><Download className="h-3.5 w-3.5" /></Button>

        </div>

        {filtersOpen ? <div id="trace-filters-panel" className="min-h-0 shrink-0"><TraceFilterPanel value={filters} onApply={setFilters} facets={facets.data} loading={facets.isPending} error={facets.isError} /></div> : null}
        <TraceFilterChips filters={filters} onChange={setFilters} />
        {isError || extensionQuery.isError ? <div role="alert" className="border-b border-border-subtle px-3 py-2 text-xs text-destructive">{isError ? "Gateway traces could not be loaded. " : ""}{extensionQuery.isError ? "Browser activity could not be loaded. " : ""}<button type="button" className="cursor-pointer underline" onClick={() => { void refetch(); if (extension.fetchExtra) void extensionQuery.refetch(); }}>Try again</button></div> : null}
        <div id="trace-results" role="tabpanel" aria-labelledby={`trace-type-${filters.traceType || "all"}`} aria-busy={isLoading} className="min-h-0 flex-1 overflow-auto">
          {isLoading || (isPlaceholderData && !filtered.length) ? <SkeletonRows count={12} /> : !filtered.length ? <TracesEmptyState hasActiveFilters={Object.values(filters).some(Boolean)} rangeLabel={controls.rangeLabel} onExpandRange={() => controls.setRange("30d")} /> : (
            <Table className={cn("text-[11px]", showEnvironment ? "min-w-[920px]" : "min-w-[820px]")}>
              <TableHeader className="sticky top-0 z-10 bg-background">
                <TableRow className="border-border-subtle hover:bg-transparent">
                  <Th ariaSort={sort === "started_at" ? descending ? "descending" : "ascending" : "none"}><button type="button" className="inline-flex items-center gap-1.5" onClick={() => sortBy("started_at")}>Time<ArrowDownUp className="h-2.5 w-2.5" /></button></Th><Th>Name / type</Th>{showEnvironment ? <Th>Environment</Th> : null}<Th>Status</Th><Th>Security</Th><Th>Provider / model</Th><Th className="text-right" ariaSort={sort === "duration_ms" ? descending ? "descending" : "ascending" : "none"}><button type="button" className="inline-flex items-center gap-1.5" onClick={() => sortBy("duration_ms")}>Latency<ArrowDownUp className="h-2.5 w-2.5" /></button></Th><Th className="text-right">Tokens</Th><Th className="text-right" ariaSort={sort === "cost_cents" ? descending ? "descending" : "ascending" : "none"}><button type="button" className="inline-flex items-center gap-1.5" onClick={() => sortBy("cost_cents")}>Cost<ArrowDownUp className="h-2.5 w-2.5" /></button></Th>{showEndUser ? <Th>End user</Th> : null}
                </TableRow>
              </TableHeader>
              <TableBody>
                {sorted.map((trace) => (
                  <TableRow key={trace.id} onClick={() => openTrace(trace)} className={cn("cursor-pointer border-border-subtle hover:bg-surface-2/70", selected?.id === trace.id && "bg-sidebar-accent")}>
                    <MonoCell title={new Date(trace.started_at).toLocaleString()}>{formatTimestamp(trace.started_at)}</MonoCell>
                    <TableCell className="max-w-[220px] px-3 py-2.5" title={trace.trace_name || trace.path}><button type="button" onClick={(event) => { event.stopPropagation(); openTrace(trace); }} className="block max-w-full truncate text-left text-[11px] text-foreground hover:underline" aria-label={`Inspect ${trace.trace_name || trace.path || trace.model}`}>{trace.trace_name || trace.path || trace.model}</button><span className="mt-1 block text-[9px] text-muted-foreground">{TRACE_TYPES.find((kind) => kind.value === traceType(trace))?.label}</span></TableCell>
                    {showEnvironment ? <TableCell><EnvironmentBadge environment={trace.environment} /></TableCell> : null}
                    <TableCell><TraceStatus status={trace.status} /></TableCell>
                    <TableCell>{trace.threat_detected ? <span title={securityDecision(trace.security_action).explanation} className={cn("whitespace-nowrap text-[10px]", ["block", "warn"].includes(trace.security_action) ? "text-warn" : "text-text-secondary")}>{(trace.threat_types ?? [])[0]?.replaceAll("_", " ") || "Security signal"}<span className="mt-1 block text-[9px] text-muted-foreground">{securityDecision(trace.security_action).label}</span></span> : <span className="whitespace-nowrap text-[10px] text-muted-foreground">No signals</span>}</TableCell>
                    <TableCell className="max-w-[180px] text-[10px] text-text-secondary"><span>{trace.provider}</span><span className="mt-1 block truncate font-mono text-[9px] text-muted-foreground" title={trace.model}>{trace.model}</span></TableCell>
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
        <div className="flex min-h-11 flex-shrink-0 flex-wrap items-center justify-between gap-2 border-t border-border-subtle px-3 py-2 text-[10px] text-muted-foreground"><span><span className="font-mono text-foreground">{filtered.length}</span> latest matching results{isFetching || extensionQuery.isFetching ? " · Updating…" : ""}</span><span>{filtered.length === 100 ? "Showing the latest 100; narrow the filters to investigate older calls." : "Metrics describe the displayed results."}</span></div>
      </section>

      {selected ? <TraceInspector trace={selected} onClose={() => { setInspectorDismissed(true); setSelected(null); }} /> : null}
    </div>
  );
}

function Th({ children, className, ariaSort }: { children: React.ReactNode; className?: string; ariaSort?: "none" | "ascending" | "descending" }) {
  return <TableHead aria-sort={ariaSort} className={cn("h-9 px-3 text-[10px] font-normal text-muted-foreground first:pl-5 last:pr-5", className)}>{children}</TableHead>;
}

function MonoCell({ children, className, title }: { children: React.ReactNode; className?: string; title?: string }) {
  return <TableCell title={title} className={cn("h-[52px] px-3 py-2.5 font-mono text-[10px] tabular-nums text-text-secondary first:pl-5 last:pr-5", className)}>{children}</TableCell>;
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
  let blocked = 0;
  let errors = 0;
  let duration = 0;
  for (const trace of traces) {
    inputTokens += trace.input_tokens ?? 0;
    outputTokens += trace.output_tokens ?? 0;
    cost += trace.cost_cents ?? 0;
    if (trace.threat_detected) threats += 1;
    if (trace.status === "blocked") blocked += 1;
    if (trace.status === "error" || trace.status === "rate_limited") errors += 1;
    duration += trace.duration_ms ?? 0;
  }
  return { count: traces.length, tokens: inputTokens + outputTokens, inputTokens, outputTokens, cost, threats, blocked, errors, avgLatency: traces.length ? duration / traces.length : 0 };
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
