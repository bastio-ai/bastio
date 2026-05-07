import { useEffect, useMemo, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useNavigate, useSearch } from "@tanstack/react-router";
import {
  AlertTriangle,
  ArrowDown,
  ArrowUp,
  ArrowUpDown,
  ChevronLeft,
  ChevronRight,
  Layers,
  ShieldBan,
  ShieldCheck,
  ShieldX,
  Users,
} from "lucide-react";

import { api } from "@/api/client";
import type { ThreatEvent } from "@/api/client";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
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
import { KpiCard } from "@/components/observe/kpi-card";
import { LiveToggle } from "@/components/observe/live-toggle";
import {
  ThreatFilterBar,
  type ThreatFilters,
} from "@/components/observe/threat-filter-bar";
import { ThreatDetailSheet } from "@/components/observe/threat-detail-sheet";
import { downloadCSV } from "@/lib/csv";
import { formatNumber } from "@/lib/utils";

// Sort columns exposed to the backend. Keep in sync with
// threatSortColumns in internal/observability/handler.go.
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

const SORT_COLUMNS: readonly SortColumn[] = [
  "detected_at",
  "severity",
  "score",
  "confidence",
];

export function validateThreatsSearch(
  raw: Record<string, unknown>,
): ThreatsSearch {
  const str = (k: keyof ThreatsSearch) =>
    typeof raw[k] === "string" && raw[k] ? (raw[k] as string) : undefined;
  const page = Number(raw.page);
  const sort = SORT_COLUMNS.includes(raw.sort as SortColumn)
    ? (raw.sort as SortColumn)
    : undefined;
  const order =
    raw.order === "asc" || raw.order === "desc"
      ? (raw.order as SortOrder)
      : undefined;
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

const PAGE_SIZE = 50;

export function ThreatsPage() {
  const navigate = useNavigate();
  const search = useSearch({ from: "/threats" }) as ThreatsSearch;

  // Local filter state, hydrated from URL on mount + whenever the URL
  // changes (e.g. back button). We keep a local copy so typing into text
  // inputs doesn't push to history on every keystroke.
  const [filters, setFilters] = useState<ThreatFilters>(() =>
    searchToFilters(search),
  );
  useEffect(() => {
    setFilters(searchToFilters(search));
  }, [search]);

  const sort: SortColumn = search.sort ?? "detected_at";
  const order: SortOrder = search.order ?? "desc";
  const page = search.page ?? 1;
  const groupMode = search.group ?? false;
  const [live, setLive] = useState(false);
  const [openThreat, setOpenThreat] = useState<ThreatEvent | null>(null);
  const searchInputRef = useRef<HTMLInputElement | null>(null);

  // "/" focuses the search input, unless the user is already in a text
  // field (otherwise typing "/" anywhere breaks normal input).
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== "/") return;
      const t = e.target as HTMLElement | null;
      const tag = t?.tagName;
      if (tag === "INPUT" || tag === "TEXTAREA" || t?.isContentEditable) return;
      e.preventDefault();
      searchInputRef.current?.focus();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

  // Commit a filter change to the URL: resets to page 1 whenever filters
  // change (a stale page number with new filters is almost always wrong).
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
      replace: false,
    });
  };

  const setSort = (column: SortColumn) => {
    const nextOrder: SortOrder =
      sort === column && order === "desc" ? "asc" : "desc";
    navigate({
      to: "/threats",
      search: { ...search, sort: column, order: nextOrder, page: undefined },
    });
  };

  const setPage = (next: number) => {
    navigate({
      to: "/threats",
      search: { ...search, page: next === 1 ? undefined : next },
    });
  };

  const setGroupMode = (next: boolean) => {
    navigate({
      to: "/threats",
      search: { ...search, group: next ? true : undefined, page: undefined },
    });
  };

  const applyQuickFilter = (patch: Partial<ThreatsSearch>) => {
    navigate({ to: "/threats", search: { ...search, ...patch, page: undefined } });
  };

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
      from: filters.from ? new Date(filters.from).toISOString() : undefined,
      to: filters.to ? new Date(filters.to).toISOString() : undefined,
      sort,
      order,
    }),
    [filters, sort, order, page],
  );

  const { data: threats, isLoading } = useQuery({
    queryKey: ["threats", queryParams, live],
    queryFn: () => api.threats.list(queryParams),
    refetchInterval: live ? 3000 : false,
    placeholderData: (prev) => prev,
  });

  const rows = useMemo(() => threats ?? [], [threats]);
  const kpis = useMemo(() => computeKpis(rows), [rows]);
  const detectors = useMemo(
    () => Array.from(new Set(rows.map((r) => r.detector_name))).sort(),
    [rows],
  );
  const grouped = useMemo(() => (groupMode ? groupRows(rows) : null), [
    rows,
    groupMode,
  ]);
  const hasActiveFilters =
    Object.values(filters).some(Boolean) || Boolean(search.search);

  return (
    <>
      <PageHeader
        title="Threats"
        description="Security threat events detected by the gateway"
        action={
          <div className="flex items-center gap-2">
            <LiveToggle live={live} onToggle={() => setLive((v) => !v)} />
            <Button
              variant={groupMode ? "secondary" : "outline"}
              size="sm"
              className="h-8 text-xs"
              onClick={() => setGroupMode(!groupMode)}
              title="Collapse identical (detector, pattern, severity) into one row"
            >
              <Layers className="h-3 w-3" /> {groupMode ? "Grouped" : "Group"}
            </Button>
          </div>
        }
      />

      <div className="grid grid-cols-2 gap-2 sm:grid-cols-4">
        <KpiCard
          label="Threats"
          value={formatNumber(kpis.total)}
          sub="In current window"
          icon={<AlertTriangle className="h-4 w-4" />}
        />
        <KpiCard
          label="Critical + High"
          value={formatNumber(kpis.critHigh)}
          sub={`${kpis.total ? Math.round((kpis.critHigh / kpis.total) * 100) : 0}% of threats`}
          tone={kpis.critHigh ? "danger" : "success"}
          icon={<ShieldX className="h-4 w-4" />}
        />
        <KpiCard
          label="Blocked"
          value={formatNumber(kpis.blocked)}
          sub={`${kpis.total ? Math.round((kpis.blocked / kpis.total) * 100) : 0}% prevented`}
          tone={kpis.blocked ? "success" : undefined}
          icon={<ShieldBan className="h-4 w-4" />}
        />
        <KpiCard
          label="Users affected"
          value={formatNumber(kpis.uniqueUsers)}
          sub="Distinct end_user_id"
          icon={<Users className="h-4 w-4" />}
        />
      </div>

      <QuickFilterChips
        onLastHour={() =>
          applyQuickFilter({
            from: toLocalInput(new Date(Date.now() - 3600_000)),
            to: undefined,
          })
        }
        onCriticalHigh={() => applyQuickFilter({ severity: "critical,high" })}
        onBlocked={() => applyQuickFilter({ action_taken: "block" })}
      />

      <ThreatFilterBar
        value={filters}
        onChange={commitFilters}
        detectors={detectors}
        searchInputRef={searchInputRef}
        onCSV={() =>
          downloadCSV(
            `bastio-threats-${new Date().toISOString().slice(0, 10)}.csv`,
            rows as unknown as Record<string, unknown>[],
          )
        }
      />

      <Card className="border-border/50 overflow-hidden">
        <CardContent className="p-0">
          {isLoading && !rows.length ? (
            <SkeletonRows count={8} />
          ) : !rows.length ? (
            <ThreatsEmptyState hasActiveFilters={hasActiveFilters} />
          ) : (
            <Table>
              <TableHeader>
                <TableRow className="hover:bg-transparent border-border/50">
                  {groupMode ? (
                    <>
                      <Th>Severity</Th>
                      <Th>Type</Th>
                      <Th>Subtype</Th>
                      <Th>Detector</Th>
                      <Th>Pattern</Th>
                      <Th className="text-right">Count</Th>
                      <Th>Latest</Th>
                    </>
                  ) : (
                    <>
                      <SortableTh
                        label="Time"
                        column="detected_at"
                        currentSort={sort}
                        currentOrder={order}
                        onSort={setSort}
                        className="w-[10rem]"
                      />
                      <SortableTh
                        label="Severity"
                        column="severity"
                        currentSort={sort}
                        currentOrder={order}
                        onSort={setSort}
                        className="w-[6rem]"
                      />
                      <Th>Type</Th>
                      <Th>Subtype</Th>
                      <Th>Detector</Th>
                      <Th>Pattern</Th>
                      <SortableTh
                        label="Confidence"
                        column="confidence"
                        currentSort={sort}
                        currentOrder={order}
                        onSort={setSort}
                        className="text-right w-[6rem]"
                      />
                      <SortableTh
                        label="Score"
                        column="score"
                        currentSort={sort}
                        currentOrder={order}
                        onSort={setSort}
                        className="text-right w-[6rem]"
                      />
                      <Th className="w-[5rem]">Action</Th>
                      <Th>User</Th>
                      <Th>IP</Th>
                    </>
                  )}
                </TableRow>
              </TableHeader>
              <TableBody>
                {groupMode && grouped
                  ? grouped.map((g) => (
                      <TableRow
                        key={g.key}
                        className="cursor-pointer border-border/30 hover:bg-muted/30"
                        onClick={() => setOpenThreat(g.latest)}
                      >
                        <TableCell>
                          <SeverityBadge severity={g.severity} />
                        </TableCell>
                        <TableCell className="text-xs text-foreground/90">
                          {g.threatType}
                        </TableCell>
                        <TableCell className="font-mono text-[11px] text-muted-foreground">
                          {g.latest.threat_subtype || "—"}
                        </TableCell>
                        <TableCell className="font-mono text-xs text-muted-foreground">
                          {g.detectorName}
                        </TableCell>
                        <TableCell className="truncate font-mono text-[11px] text-muted-foreground max-w-[22rem]">
                          {g.matchedPattern}
                        </TableCell>
                        <TableCell className="text-right font-mono tabular-nums text-xs">
                          {g.count}
                        </TableCell>
                        <TableCell
                          className="font-mono tabular-nums text-xs text-muted-foreground"
                          title={new Date(g.latestAt).toLocaleString()}
                        >
                          {relativeTime(g.latestAt)}
                        </TableCell>
                      </TableRow>
                    ))
                  : rows.map((t) => (
                      <TableRow
                        key={t.id}
                        className="cursor-pointer border-border/30 hover:bg-muted/30"
                        onClick={() => setOpenThreat(t)}
                      >
                        <TableCell
                          className="font-mono tabular-nums text-xs text-muted-foreground"
                          title={new Date(t.detected_at).toLocaleString()}
                        >
                          {relativeTime(t.detected_at)}
                        </TableCell>
                        <TableCell>
                          <SeverityBadge severity={t.severity} />
                        </TableCell>
                        <TableCell className="text-xs text-foreground/90">
                          {t.threat_type}
                        </TableCell>
                        <TableCell className="font-mono text-[11px] text-muted-foreground">
                          {t.threat_subtype || "—"}
                        </TableCell>
                        <TableCell className="font-mono text-xs text-muted-foreground">
                          {t.detector_name}
                        </TableCell>
                        <TableCell className="truncate font-mono text-[11px] text-muted-foreground max-w-[18rem]">
                          {t.matched_pattern}
                        </TableCell>
                        <TableCell className="text-right font-mono tabular-nums text-xs text-muted-foreground">
                          {(t.confidence * 100).toFixed(0)}%
                        </TableCell>
                        <TableCell className="text-right font-mono tabular-nums text-xs text-muted-foreground">
                          {(t.score * 100).toFixed(0)}%
                        </TableCell>
                        <TableCell>
                          <Badge
                            variant={
                              t.action_taken === "block"
                                ? "destructive"
                                : "outline"
                            }
                            className="text-[10px] px-1.5 py-0"
                          >
                            {t.action_taken}
                          </Badge>
                        </TableCell>
                        <TableCell className="truncate text-xs text-muted-foreground max-w-[10rem]">
                          {(t as ThreatEvent & { end_user_id?: string })
                            .end_user_id || "—"}
                        </TableCell>
                        <TableCell className="font-mono text-[11px] text-muted-foreground">
                          {(t as ThreatEvent & { ip_address?: string })
                            .ip_address || "—"}
                        </TableCell>
                      </TableRow>
                    ))}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>

      {rows.length > 0 ? (
        <Pagination
          page={page}
          pageSize={PAGE_SIZE}
          returned={rows.length}
          onPage={setPage}
        />
      ) : null}

      <ThreatDetailSheet
        threat={openThreat}
        open={openThreat !== null}
        onOpenChange={(o) => {
          if (!o) setOpenThreat(null);
        }}
      />
    </>
  );
}

function QuickFilterChips({
  onLastHour,
  onCriticalHigh,
  onBlocked,
}: {
  onLastHour: () => void;
  onCriticalHigh: () => void;
  onBlocked: () => void;
}) {
  return (
    <div className="mt-4 flex flex-wrap items-center gap-1.5">
      <span className="text-[10px] font-semibold uppercase tracking-wider text-muted-foreground/60">
        Quick filters
      </span>
      <Chip onClick={onLastHour}>Last hour</Chip>
      <Chip onClick={onCriticalHigh}>
        <ShieldX className="h-3 w-3" /> Critical + High
      </Chip>
      <Chip onClick={onBlocked}>
        <ShieldCheck className="h-3 w-3" /> Blocked only
      </Chip>
    </div>
  );
}

function Chip({
  children,
  onClick,
}: {
  children: React.ReactNode;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className="inline-flex items-center gap-1 rounded-full border border-border/50 bg-muted/30 px-2.5 py-0.5 text-[11px] text-muted-foreground hover:bg-muted/60 hover:text-foreground"
    >
      {children}
    </button>
  );
}

function Th({
  children,
  className,
}: {
  children: React.ReactNode;
  className?: string;
}) {
  return (
    <TableHead
      className={`h-10 text-[11px] font-semibold uppercase tracking-wider text-muted-foreground/60 ${className ?? ""}`}
    >
      {children}
    </TableHead>
  );
}

function SortableTh({
  label,
  column,
  currentSort,
  currentOrder,
  onSort,
  className,
}: {
  label: string;
  column: SortColumn;
  currentSort: SortColumn;
  currentOrder: SortOrder;
  onSort: (c: SortColumn) => void;
  className?: string;
}) {
  const active = currentSort === column;
  const Icon = !active ? ArrowUpDown : currentOrder === "asc" ? ArrowUp : ArrowDown;
  return (
    <TableHead
      className={`h-10 text-[11px] font-semibold uppercase tracking-wider text-muted-foreground/60 ${className ?? ""}`}
    >
      <button
        type="button"
        onClick={() => onSort(column)}
        className={`inline-flex items-center gap-1 ${
          active ? "text-foreground" : ""
        } hover:text-foreground`}
      >
        {label} <Icon className="h-3 w-3" />
      </button>
    </TableHead>
  );
}

function SeverityBadge({ severity }: { severity: string }) {
  const variant =
    severity === "critical"
      ? "destructive"
      : severity === "high"
        ? "warning"
        : "secondary";
  return (
    <Badge
      variant={variant}
      className="text-[10px] px-1.5 py-0 min-w-[52px] justify-center"
    >
      {severity}
    </Badge>
  );
}

function Pagination({
  page,
  pageSize,
  returned,
  onPage,
}: {
  page: number;
  pageSize: number;
  returned: number;
  onPage: (p: number) => void;
}) {
  const start = (page - 1) * pageSize + 1;
  const end = start + returned - 1;
  const canNext = returned === pageSize;
  return (
    <div className="mt-3 flex items-center justify-between text-[11px] text-muted-foreground">
      <span>
        Showing <span className="tabular-nums text-foreground/80">{start}</span>–
        <span className="tabular-nums text-foreground/80">{end}</span>
      </span>
      <div className="flex items-center gap-1.5">
        <Button
          variant="outline"
          size="sm"
          className="h-7 text-xs"
          disabled={page <= 1}
          onClick={() => onPage(page - 1)}
        >
          <ChevronLeft className="h-3 w-3" /> Prev
        </Button>
        <span className="tabular-nums">Page {page}</span>
        <Button
          variant="outline"
          size="sm"
          className="h-7 text-xs"
          disabled={!canNext}
          onClick={() => onPage(page + 1)}
        >
          Next <ChevronRight className="h-3 w-3" />
        </Button>
      </div>
    </div>
  );
}

function ThreatsEmptyState({
  hasActiveFilters,
}: {
  hasActiveFilters: boolean;
}) {
  if (hasActiveFilters) {
    return (
      <EmptyState
        icon={<AlertTriangle className="h-6 w-6" />}
        title="No threats match these filters"
        description="Try loosening or clearing the filters."
      />
    );
  }
  return (
    <EmptyState
      icon={<ShieldCheck className="h-6 w-6" />}
      title="No threats detected"
      description="When the security engine detects threats in gateway traffic, they'll appear here."
    />
  );
}

// --- helpers -------------------------------------------------------------

function searchToFilters(s: ThreatsSearch): ThreatFilters {
  return {
    severity: s.severity ?? "",
    threatType: s.threat_type ?? "",
    threatSubtype: s.threat_subtype ?? "",
    detectorName: s.detector_name ?? "",
    actionTaken: s.action_taken ?? "",
    endUser: s.end_user_id ?? "",
    ipAddress: s.ip_address ?? "",
    search: s.search ?? "",
    from: s.from ?? "",
    to: s.to ?? "",
  };
}

function filtersToSearch(f: ThreatFilters): Partial<ThreatsSearch> {
  const undef = (v: string) => (v === "" ? undefined : v);
  return {
    severity: undef(f.severity),
    threat_type: undef(f.threatType),
    threat_subtype: undef(f.threatSubtype),
    detector_name: undef(f.detectorName),
    action_taken: undef(f.actionTaken),
    end_user_id: undef(f.endUser),
    ip_address: undef(f.ipAddress),
    search: undef(f.search),
    from: undef(f.from),
    to: undef(f.to),
    page: undefined,
  };
}

type Kpis = {
  total: number;
  critHigh: number;
  blocked: number;
  uniqueUsers: number;
};

function computeKpis(rows: ThreatEvent[]): Kpis {
  let critHigh = 0;
  let blocked = 0;
  const users = new Set<string>();
  for (const r of rows) {
    if (r.severity === "critical" || r.severity === "high") critHigh += 1;
    if (r.action_taken === "block") blocked += 1;
    const uid = (r as ThreatEvent & { end_user_id?: string }).end_user_id;
    if (uid) users.add(uid);
  }
  return {
    total: rows.length,
    critHigh,
    blocked,
    uniqueUsers: users.size,
  };
}

type GroupedThreat = {
  key: string;
  severity: string;
  threatType: string;
  detectorName: string;
  matchedPattern: string;
  count: number;
  latestAt: string;
  latest: ThreatEvent;
};

function groupRows(rows: ThreatEvent[]): GroupedThreat[] {
  const bucket = new Map<string, GroupedThreat>();
  for (const r of rows) {
    const key = `${r.severity}|${r.detector_name}|${r.matched_pattern}`;
    const existing = bucket.get(key);
    if (!existing) {
      bucket.set(key, {
        key,
        severity: r.severity,
        threatType: r.threat_type,
        detectorName: r.detector_name,
        matchedPattern: r.matched_pattern,
        count: 1,
        latestAt: r.detected_at,
        latest: r,
      });
      continue;
    }
    existing.count += 1;
    if (r.detected_at > existing.latestAt) {
      existing.latestAt = r.detected_at;
      existing.latest = r;
    }
  }
  return Array.from(bucket.values()).sort((a, b) =>
    b.latestAt.localeCompare(a.latestAt),
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

// datetime-local inputs expect "YYYY-MM-DDTHH:mm" in the *local* zone.
function toLocalInput(d: Date): string {
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
}
