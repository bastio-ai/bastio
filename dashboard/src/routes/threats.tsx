import { useEffect, useMemo, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Link, useNavigate, useSearch } from "@tanstack/react-router";
import {
  ArrowDown,
  ArrowUp,
  ArrowUpDown,
  Check,
  ChevronLeft,
  ChevronRight,
  Columns3,
  Download,
  Layers,
  Pause,
  Play,
  Save,
  Search,
  ShieldAlert,
} from "lucide-react";

import { api, type ThreatEvent } from "@/api/client";
import { EmptyState } from "@/components/card";
import { useDashboardControls } from "@/components/dashboard-controls-context";
import { ThreatDetailSheet } from "@/components/observe/threat-detail-sheet";
import { ThreatInspector } from "@/components/observe/threat-inspector";
import {
  type ThreatFilters,
} from "@/components/observe/threat-filter-bar";
import { SkeletonRows } from "@/components/skeleton";
import { Badge } from "@/components/ui/badge";
import { Button, buttonVariants } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { downloadCSV } from "@/lib/csv";
import { cn, formatNumber, weightedThreatScore } from "@/lib/utils";

type SortColumn = "detected_at" | "severity" | "score" | "confidence";
type SortOrder = "asc" | "desc";

export type ThreatsSearch = {
  severity?: string;
  threat_type?: string;
  threat_subtype?: string;
  detector_name?: string;
  action_taken?: string;
  end_user_id?: string;
  ip_address?: string;
  search?: string;
  from?: string;
  to?: string;
  sort?: SortColumn;
  order?: SortOrder;
  page?: number;
  group?: boolean;
};

const SORT_COLUMNS: readonly SortColumn[] = ["detected_at", "severity", "score", "confidence"];
const PAGE_SIZE = 50;

export function validateThreatsSearch(raw: Record<string, unknown>): ThreatsSearch {
  const str = (key: keyof ThreatsSearch) =>
    typeof raw[key] === "string" && raw[key] ? (raw[key] as string) : undefined;
  const page = Number(raw.page);
  const sort = SORT_COLUMNS.includes(raw.sort as SortColumn) ? (raw.sort as SortColumn) : undefined;
  const order = raw.order === "asc" || raw.order === "desc" ? (raw.order as SortOrder) : undefined;
  return {
    severity: str("severity"),
    threat_type: str("threat_type"),
    threat_subtype: str("threat_subtype"),
    detector_name: str("detector_name"),
    action_taken: str("action_taken"),
    end_user_id: str("end_user_id"),
    ip_address: str("ip_address"),
    search: str("search"),
    from: str("from"),
    to: str("to"),
    sort,
    order,
    page: Number.isFinite(page) && page > 1 ? Math.floor(page) : undefined,
    group: raw.group === true || raw.group === "true" ? true : undefined,
  };
}

export function ThreatsPage() {
  const navigate = useNavigate();
  const controls = useDashboardControls();
  const search = useSearch({ from: "/threats" }) as ThreatsSearch;
  const [filters, setFilters] = useState<ThreatFilters>(() => searchToFilters(search));
  const [selectedThreat, setSelectedThreat] = useState<ThreatEvent | null>(null);
  const [inspectorDismissed, setInspectorDismissed] = useState(false);
  const [columnsOpen, setColumnsOpen] = useState(false);
  const [showType, setShowType] = useState(false);
  const [showIp, setShowIp] = useState(true);
  const [saved, setSaved] = useState(false);
  const searchInputRef = useRef<HTMLInputElement | null>(null);
  const isWide = useWideViewport();
  const wasWideRef = useRef(isWide);

  useEffect(() => setFilters(searchToFilters(search)), [search]);

  const sort = search.sort ?? "detected_at";
  const order = search.order ?? "desc";
  const page = search.page ?? 1;
  const groupMode = search.group ?? false;

  const queryParams = useMemo(
    () => ({
      limit: PAGE_SIZE,
      offset: (page - 1) * PAGE_SIZE,
      severity: filters.severity || undefined,
      threat_type: filters.threatType || undefined,
      threat_subtype: filters.threatSubtype || undefined,
      detector_name: filters.detectorName || undefined,
      action_taken: filters.actionTaken || undefined,
      end_user_id: filters.endUser || undefined,
      ip_address: filters.ipAddress || undefined,
      search: filters.search || undefined,
      from: filters.from ? new Date(filters.from).toISOString() : controls.timeWindow.from,
      to: filters.to ? new Date(filters.to).toISOString() : controls.timeWindow.to,
      environment: controls.environment || undefined,
      sort,
      order,
    }),
    [controls.environment, controls.timeWindow, filters, order, page, sort],
  );

  const { data: threats, isLoading } = useQuery({
    queryKey: ["threats", queryParams, controls.live],
    queryFn: () => api.threats.list(queryParams),
    refetchInterval: controls.live ? 10_000 : false,
    placeholderData: (previous) => previous,
  });

  const rows = useMemo(() => threats ?? [], [threats]);
  const kpis = useMemo(() => computeKpis(rows), [rows]);
  const grouped = useMemo(() => (groupMode ? groupRows(rows) : null), [groupMode, rows]);
  const hasActiveFilters = Object.values(filters).some(Boolean);

  useEffect(() => {
    if (isWide && !selectedThreat && !inspectorDismissed && rows.length) setSelectedThreat(rows[0]!);
  }, [inspectorDismissed, isWide, rows, selectedThreat]);

  useEffect(() => {
    if (wasWideRef.current && !isWide) {
      setSelectedThreat(null);
      setInspectorDismissed(true);
    } else if (!wasWideRef.current && isWide) {
      setInspectorDismissed(false);
    }
    wasWideRef.current = isWide;
  }, [isWide]);

  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if (event.key !== "/") return;
      const target = event.target as HTMLElement | null;
      if (target?.tagName === "INPUT" || target?.tagName === "TEXTAREA" || target?.isContentEditable) return;
      event.preventDefault();
      searchInputRef.current?.focus();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

  const commitFilters = (next: ThreatFilters) => {
    setFilters(next);
    navigate({
      to: "/threats",
      search: {
        ...filtersToSearch(next),
        sort: search.sort,
        order: search.order,
        group: search.group,
      },
    });
  };

  const setSort = (column: SortColumn) => {
    const nextOrder: SortOrder = sort === column && order === "desc" ? "asc" : "desc";
    navigate({ to: "/threats", search: { ...search, sort: column, order: nextOrder, page: undefined } });
  };

  const setPage = (next: number) =>
    navigate({ to: "/threats", search: { ...search, page: next === 1 ? undefined : next } });

  const selectThreat = (threat: ThreatEvent) => {
    setInspectorDismissed(false);
    setSelectedThreat(threat);
  };

  return (
    <div className="flex h-full min-h-0 bg-background">
      <section className="flex min-w-0 flex-1 flex-col">
        <SummaryStrip kpis={kpis} />

        <div className="flex flex-wrap items-center gap-2 border-b border-border-subtle bg-background px-3 py-2">
          <button
            type="button"
            onClick={() => navigate({ to: "/threats", search: { ...search, group: groupMode ? undefined : true, page: undefined } })}
            className={cn("flex h-8 items-center gap-2 rounded-md border border-border-subtle px-2.5 text-[11px] text-muted-foreground hover:bg-surface-2 hover:text-foreground", groupMode && "bg-surface-2 text-foreground")}
          >
            <Layers className="h-3.5 w-3.5" /> Group by
            <span className={cn("relative h-4 w-7 rounded-full border border-border-default transition-colors", groupMode ? "bg-foreground" : "bg-surface-2")}>
              <span className={cn("absolute top-0.5 h-2.5 w-2.5 rounded-full transition-all", groupMode ? "left-3.5 bg-background" : "left-0.5 bg-muted-foreground")} />
            </span>
          </button>

          <div className="relative min-w-[220px] flex-1">
            <Search className="pointer-events-none absolute left-2.5 top-2 h-3.5 w-3.5 text-muted-foreground" />
            <Input
              ref={searchInputRef}
              value={filters.search}
              onChange={(event) => setFilters((current) => ({ ...current, search: event.target.value }))}
              onBlur={() => commitFilters(filters)}
              onKeyDown={(event) => {
                if (event.key === "Enter") commitFilters(filters);
              }}
              placeholder="Search threats…"
              className="pl-8 text-xs"
            />
          </div>

          <div className="relative">
            <Button variant="outline" size="sm" className="h-8 text-[11px]" onClick={() => setColumnsOpen((value) => !value)}>
              <Columns3 className="h-3.5 w-3.5" /> Columns <ChevronDownSmall />
            </Button>
            {columnsOpen ? (
              <div className="absolute right-0 top-9 z-30 w-44 rounded-md border border-border-default bg-popover p-1.5 shadow-xl">
                <ColumnToggle label="Threat type" checked={showType} onChange={setShowType} />
                <ColumnToggle label="IP address" checked={showIp} onChange={setShowIp} />
              </div>
            ) : null}
          </div>

          <Button
            variant="outline"
            size="sm"
            className="h-8 text-[11px]"
            onClick={() => {
              try {
                localStorage.setItem("bastio-threat-view", JSON.stringify(search));
                setSaved(true);
                window.setTimeout(() => setSaved(false), 1500);
              } catch {
                setSaved(false);
              }
            }}
          >
            {saved ? <Check className="h-3.5 w-3.5" /> : <Save className="h-3.5 w-3.5" />} {saved ? "Saved" : "Save view"}
          </Button>
          <Button variant="outline" size="icon-sm" title="Export CSV" onClick={() => downloadCSV(`bastio-threats-${new Date().toISOString().slice(0, 10)}.csv`, rows as unknown as Record<string, unknown>[])}>
            <Download className="h-3.5 w-3.5" />
          </Button>
          <Button variant="outline" size="sm" className="h-8 text-[11px]" onClick={() => controls.setLive(!controls.live)}>
            {controls.live ? <Pause className="h-3.5 w-3.5" /> : <Play className="h-3.5 w-3.5" />} {controls.live ? "Pause" : "Live"}
          </Button>
        </div>

        <div className="min-h-0 flex-1 overflow-auto">
          {isLoading && !rows.length ? (
            <SkeletonRows count={12} />
          ) : !rows.length ? (
            <ThreatsEmptyState hasActiveFilters={hasActiveFilters} />
          ) : (
            <Table className="min-w-[860px] text-[11px]">
              <TableHeader className="sticky top-0 z-10 bg-background/95 backdrop-blur">
                <TableRow className="border-border-subtle hover:bg-transparent">
                  <TableHead className="h-9 w-9 px-3"><input type="checkbox" aria-label="Select all threats" className="h-3.5 w-3.5 rounded border-border-default bg-transparent" /></TableHead>
                  {groupMode ? (
                    <>
                      <Th>Severity</Th><Th>Type</Th><Th>Subtype</Th><Th>Detector</Th><Th>Pattern</Th><Th className="text-right">Count</Th><Th>Latest</Th>
                    </>
                  ) : (
                    <>
                      <SortableTh label="Time" column="detected_at" currentSort={sort} currentOrder={order} onSort={setSort} />
                      <SortableTh label="Severity" column="severity" currentSort={sort} currentOrder={order} onSort={setSort} />
                      {showType ? <Th>Type</Th> : null}
                      <Th>Subtype</Th><Th>Detector</Th><Th>Pattern</Th>
                      <SortableTh label="Score (wtd)" column="score" currentSort={sort} currentOrder={order} onSort={setSort} className="text-right" />
                      <Th>Action</Th>{showIp ? <Th>IP address</Th> : null}
                    </>
                  )}
                </TableRow>
              </TableHeader>
              <TableBody>
                {groupMode && grouped
                  ? grouped.map((group) => (
                      <TableRow key={group.key} onClick={() => selectThreat(group.latest)} className={cn("cursor-pointer border-border-subtle hover:bg-surface-2/70", selectedThreat?.id === group.latest.id && "bg-surface-2")}>
                        <TableCell className="px-3"><input type="checkbox" aria-label={`Select ${group.threatType}`} onClick={(event) => event.stopPropagation()} className="h-3.5 w-3.5" /></TableCell>
                        <TableCell><SeverityBadge severity={group.severity} /></TableCell>
                        <Cell>{group.threatType}</Cell><MonoCell>{group.latest.threat_subtype || "—"}</MonoCell><MonoCell>{group.detectorName}</MonoCell><MonoCell className="max-w-[230px] truncate">{group.matchedPattern}</MonoCell><MonoCell className="text-right">{group.count}</MonoCell><MonoCell>{relativeTime(group.latestAt)}</MonoCell>
                      </TableRow>
                    ))
                  : rows.map((threat) => (
                      <TableRow
                        key={threat.id}
                        onClick={() => selectThreat(threat)}
                        className={cn(
                          "cursor-pointer border-border-subtle hover:bg-surface-2/70",
                          selectedThreat?.id === threat.id && "bg-surface-2",
                          threat.severity === "critical" ? "border-l-2 border-l-danger" : threat.severity === "high" ? "border-l-2 border-l-warn" : "border-l-2 border-l-transparent",
                        )}
                      >
                        <TableCell className="px-3"><input type="checkbox" aria-label={`Select ${threat.threat_type}`} onClick={(event) => event.stopPropagation()} className="h-3.5 w-3.5" /></TableCell>
                        <MonoCell title={new Date(threat.detected_at).toLocaleString()}>{formatTimestamp(threat.detected_at)}</MonoCell>
                        <TableCell><SeverityBadge severity={threat.severity} /></TableCell>
                        {showType ? <Cell>{threat.threat_type}</Cell> : null}
                        <Cell className="max-w-[150px] truncate">{threat.threat_subtype || "—"}</Cell>
                        <MonoCell className="max-w-[150px] truncate">{threat.detector_name}</MonoCell>
                        <MonoCell className="max-w-[180px] truncate">{threat.matched_pattern}</MonoCell>
                        <MonoCell className="text-right text-foreground">{(weightedThreatScore(threat.score, threat.confidence, threat.weighted_score) * 100).toFixed(0)}%</MonoCell>
                        <TableCell><Badge variant={threat.action_taken === "block" ? "destructive" : "outline"} className="px-1.5 py-0 text-[9px]">{threat.action_taken}</Badge></TableCell>
                        {showIp ? <MonoCell>{(threat as ThreatEvent & { ip_address?: string }).ip_address || "—"}</MonoCell> : null}
                      </TableRow>
                    ))}
              </TableBody>
            </Table>
          )}
        </div>

        {rows.length ? <Pagination page={page} returned={rows.length} onPage={setPage} /> : null}
      </section>

      {selectedThreat ? (
        <ThreatInspector
          threat={selectedThreat}
          onClose={() => {
            setInspectorDismissed(true);
            setSelectedThreat(null);
          }}
        />
      ) : null}

      <ThreatDetailSheet
        threat={selectedThreat}
        open={!isWide && selectedThreat !== null}
        onOpenChange={(open) => {
          if (!open) {
            setInspectorDismissed(true);
            setSelectedThreat(null);
          }
        }}
      />
    </div>
  );
}

function SummaryStrip({ kpis }: { kpis: Kpis }) {
  const items = [
    { value: kpis.total, label: "threats", sub: "In current window" },
    { value: kpis.critHigh, label: "critical / high", sub: `${kpis.total ? Math.round((kpis.critHigh / kpis.total) * 100) : 0}% of threats` },
    { value: kpis.blocked, label: "blocked", sub: `${kpis.total ? Math.round((kpis.blocked / kpis.total) * 100) : 0}% prevented` },
    { value: kpis.uniqueUsers, label: "users affected", sub: "Distinct end_user_id" },
  ];
  return (
    <div className="grid flex-shrink-0 grid-cols-2 border-b border-border-subtle bg-surface-1/50 md:grid-cols-4">
      {items.map((item, index) => (
        <div key={item.label} className={cn("px-4 py-3", index > 0 && "border-l border-border-subtle")}>
          <div className="text-[13px] font-semibold tabular-nums"><span className="font-mono">{formatNumber(item.value)}</span> <span className="text-[11px] font-medium text-foreground/85">{item.label}</span></div>
          <div className="mt-0.5 text-[9px] text-muted-foreground">{item.sub}</div>
        </div>
      ))}
    </div>
  );
}

function ColumnToggle({ label, checked, onChange }: { label: string; checked: boolean; onChange: (value: boolean) => void }) {
  return <button type="button" onClick={() => onChange(!checked)} className="flex h-8 w-full items-center gap-2 rounded px-2 text-[11px] hover:bg-surface-2"><span className={cn("flex h-3.5 w-3.5 items-center justify-center rounded border border-border-default", checked && "bg-foreground text-background")}>{checked ? <Check className="h-2.5 w-2.5" /> : null}</span>{label}</button>;
}

function ChevronDownSmall() { return <span className="text-[9px] text-muted-foreground">⌄</span>; }

function Th({ children, className }: { children: React.ReactNode; className?: string }) {
  return <TableHead className={cn("h-9 px-2 text-[9px] font-medium uppercase tracking-wider text-muted-foreground", className)}>{children}</TableHead>;
}

function SortableTh({ label, column, currentSort, currentOrder, onSort, className }: { label: string; column: SortColumn; currentSort: SortColumn; currentOrder: SortOrder; onSort: (column: SortColumn) => void; className?: string }) {
  const active = currentSort === column;
  const Icon = !active ? ArrowUpDown : currentOrder === "asc" ? ArrowUp : ArrowDown;
  return <TableHead className={cn("h-9 px-2 text-[9px] font-medium uppercase tracking-wider text-muted-foreground", className)}><button type="button" onClick={() => onSort(column)} className={cn("inline-flex items-center gap-1 hover:text-foreground", active && "text-foreground")}>{label}<Icon className="h-2.5 w-2.5" /></button></TableHead>;
}

function Cell({ children, className }: { children: React.ReactNode; className?: string }) {
  return <TableCell className={cn("h-9 px-2 py-1.5 text-[10px] text-foreground/85", className)}>{children}</TableCell>;
}

function MonoCell({ children, className, title }: { children: React.ReactNode; className?: string; title?: string }) {
  return <TableCell title={title} className={cn("h-9 px-2 py-1.5 font-mono text-[10px] tabular-nums text-muted-foreground", className)}>{children}</TableCell>;
}

function SeverityBadge({ severity }: { severity: string }) {
  return <Badge variant={severity === "critical" ? "destructive" : severity === "high" ? "warning" : "secondary"} className="min-w-[48px] px-1.5 py-0 text-[9px]">{severity}</Badge>;
}

function Pagination({ page, returned, onPage }: { page: number; returned: number; onPage: (page: number) => void }) {
  const start = (page - 1) * PAGE_SIZE + 1;
  const end = start + returned - 1;
  return (
    <div className="flex h-11 flex-shrink-0 items-center justify-between border-t border-border-subtle px-3 text-[10px] text-muted-foreground">
      <span><span className="font-mono text-foreground/85">{start}–{end}</span> results</span>
      <div className="flex items-center gap-1">
        <span className="mr-2">Rows per page <span className="font-mono text-foreground">{PAGE_SIZE}</span></span>
        <Button variant="ghost" size="icon-xs" disabled={page <= 1} onClick={() => onPage(page - 1)}><ChevronLeft className="h-3 w-3" /></Button>
        <span className="flex h-7 min-w-7 items-center justify-center rounded border border-border-default bg-surface-1 font-mono text-foreground">{page}</span>
        <Button variant="ghost" size="icon-xs" disabled={returned < PAGE_SIZE} onClick={() => onPage(page + 1)}><ChevronRight className="h-3 w-3" /></Button>
      </div>
    </div>
  );
}

function ThreatsEmptyState({ hasActiveFilters }: { hasActiveFilters: boolean }) {
  return (
    <EmptyState
      icon={<ShieldAlert className="h-6 w-6" />}
      title={hasActiveFilters ? "No threats match these filters" : "No threats detected"}
      description={hasActiveFilters ? "Try loosening or resetting the filters." : "Threat events detected by the gateway will appear here."}
      action={!hasActiveFilters ? <Link to="/playground" className={buttonVariants({ variant: "outline", size: "sm" })}>Try a sample in the playground</Link> : undefined}
    />
  );
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

function searchToFilters(search: ThreatsSearch): ThreatFilters {
  return {
    severity: search.severity ?? "",
    threatType: search.threat_type ?? "",
    threatSubtype: search.threat_subtype ?? "",
    detectorName: search.detector_name ?? "",
    actionTaken: search.action_taken ?? "",
    endUser: search.end_user_id ?? "",
    ipAddress: search.ip_address ?? "",
    search: search.search ?? "",
    from: search.from ?? "",
    to: search.to ?? "",
  };
}

function filtersToSearch(filters: ThreatFilters): Partial<ThreatsSearch> {
  const optional = (value: string) => value || undefined;
  return {
    severity: optional(filters.severity),
    threat_type: optional(filters.threatType),
    threat_subtype: optional(filters.threatSubtype),
    detector_name: optional(filters.detectorName),
    action_taken: optional(filters.actionTaken),
    end_user_id: optional(filters.endUser),
    ip_address: optional(filters.ipAddress),
    search: optional(filters.search),
    from: optional(filters.from),
    to: optional(filters.to),
    page: undefined,
  };
}

type Kpis = { total: number; critHigh: number; blocked: number; uniqueUsers: number };

function computeKpis(rows: ThreatEvent[]): Kpis {
  const users = new Set<string>();
  let critHigh = 0;
  let blocked = 0;
  for (const row of rows) {
    if (row.severity === "critical" || row.severity === "high") critHigh += 1;
    if (row.action_taken === "block") blocked += 1;
    const user = (row as ThreatEvent & { end_user_id?: string }).end_user_id;
    if (user) users.add(user);
  }
  return { total: rows.length, critHigh, blocked, uniqueUsers: users.size };
}

type GroupedThreat = { key: string; severity: string; threatType: string; detectorName: string; matchedPattern: string; count: number; latestAt: string; latest: ThreatEvent };

function groupRows(rows: ThreatEvent[]): GroupedThreat[] {
  const groups = new Map<string, GroupedThreat>();
  for (const row of rows) {
    const key = `${row.severity}|${row.detector_name}|${row.matched_pattern}`;
    const current = groups.get(key);
    if (!current) {
      groups.set(key, { key, severity: row.severity, threatType: row.threat_type, detectorName: row.detector_name, matchedPattern: row.matched_pattern, count: 1, latestAt: row.detected_at, latest: row });
    } else {
      current.count += 1;
      if (row.detected_at > current.latestAt) { current.latestAt = row.detected_at; current.latest = row; }
    }
  }
  return Array.from(groups.values()).sort((a, b) => b.latestAt.localeCompare(a.latestAt));
}

function relativeTime(iso: string) {
  const difference = Date.now() - new Date(iso).getTime();
  if (difference < 60_000) return `${Math.max(1, Math.round(difference / 1000))}s ago`;
  if (difference < 3_600_000) return `${Math.round(difference / 60_000)}m ago`;
  if (difference < 86_400_000) return `${Math.round(difference / 3_600_000)}h ago`;
  return `${Math.round(difference / 86_400_000)}d ago`;
}

function formatTimestamp(iso: string) {
  const date = new Date(iso);
  return new Intl.DateTimeFormat(undefined, { month: "short", day: "2-digit", hour: "2-digit", minute: "2-digit", second: "2-digit" }).format(date);
}
