import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { AlertTriangle, CircleDollarSign, Cpu, MessagesSquare } from "lucide-react";

import { api } from "@/api/client";
import type { Session } from "@/api/client";
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
import { FilterBar, emptyFilters, type ObserveFilters } from "@/components/observe/filter-bar";
import { KpiCard } from "@/components/observe/kpi-card";
import { formatCost, formatDuration, formatNumber } from "@/lib/utils";

export function SessionsPage() {
  const navigate = useNavigate();
  const [filters, setFilters] = useState<ObserveFilters>(emptyFilters);

  const queryParams = useMemo(() => {
    const params: Record<string, string | number> = { limit: 100 };
    if (filters.endUser) params.end_user_id = filters.endUser;
    if (filters.search) params.search = filters.search;
    if (filters.from) params.from = new Date(filters.from).toISOString();
    if (filters.to) params.to = new Date(filters.to).toISOString();
    if (filters.environment) params.environment = filters.environment;
    return params;
  }, [filters]);

  const { data: sessions = [], isLoading } = useQuery({
    queryKey: ["sessions", queryParams],
    queryFn: () => api.sessions.list(queryParams),
  });

  const kpis = useMemo(() => computeKpis(sessions), [sessions]);
  const hasActiveFilters = Object.values(filters).some(Boolean);

  return (
    <>
      <PageHeader
        title="Sessions"
        description="Conversations grouped by session id — every trace the same end-user made, in order."
      />

      <div className="mt-4 grid grid-cols-2 gap-3 sm:grid-cols-4">
        <KpiCard label="Sessions" value={formatNumber(sessions.length)} icon={<MessagesSquare className="h-4 w-4" />} />
        <KpiCard
          label="Avg traces / session"
          value={sessions.length ? (kpis.traces / sessions.length).toFixed(1) : "0"}
          sub={`${formatNumber(kpis.traces)} traces total`}
        />
        <KpiCard
          label="Cost"
          value={formatCost(kpis.cost)}
          icon={<CircleDollarSign className="h-4 w-4" />}
          sub={sessions.length ? `avg ${formatCost(kpis.cost / sessions.length)} / session` : ""}
        />
        <KpiCard
          label="Tokens"
          value={formatNumber(kpis.tokens)}
          icon={<Cpu className="h-4 w-4" />}
          sub={
            kpis.threats
              ? `${formatNumber(kpis.threats)} threats detected`
              : "No threats detected"
          }
          tone={kpis.threats ? "danger" : "success"}
        />
      </div>

      <div className="mt-4">
        <FilterBar value={filters} onChange={setFilters} showStatus={false} showSecurity={false} />
      </div>

      <Card className="border-border/50 overflow-hidden">
        <CardContent className="p-0">
          {isLoading ? (
            <SkeletonRows count={8} />
          ) : !sessions.length ? (
            <EmptyState
              icon={<MessagesSquare className="h-6 w-6" />}
              title={hasActiveFilters ? "No sessions match these filters" : "No sessions yet"}
              description={
                hasActiveFilters
                  ? "Try loosening or clearing the filters."
                  : "Pass X-Session-Id on gateway requests to group traces into sessions."
              }
            />
          ) : (
            <Table>
              <TableHeader>
                <TableRow className="hover:bg-transparent border-border/50">
                  <TableHead className="h-10 text-[11px] font-semibold uppercase tracking-wider text-muted-foreground/60">
                    Session
                  </TableHead>
                  <TableHead className="h-10 text-[11px] font-semibold uppercase tracking-wider text-muted-foreground/60">
                    End user
                  </TableHead>
                  <TableHead className="h-10 text-right text-[11px] font-semibold uppercase tracking-wider text-muted-foreground/60">
                    Traces
                  </TableHead>
                  <TableHead className="h-10 text-[11px] font-semibold uppercase tracking-wider text-muted-foreground/60">
                    Security
                  </TableHead>
                  <TableHead className="h-10 text-right text-[11px] font-semibold uppercase tracking-wider text-muted-foreground/60">
                    Tokens
                  </TableHead>
                  <TableHead className="h-10 text-right text-[11px] font-semibold uppercase tracking-wider text-muted-foreground/60">
                    Cost
                  </TableHead>
                  <TableHead className="h-10 text-right text-[11px] font-semibold uppercase tracking-wider text-muted-foreground/60">
                    Wall clock
                  </TableHead>
                  <TableHead className="h-10 text-right text-[11px] font-semibold uppercase tracking-wider text-muted-foreground/60">
                    Last activity
                  </TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {sessions.map((s) => (
                  <TableRow
                    key={s.id}
                    className="cursor-pointer border-border/30 hover:bg-muted/30"
                    onClick={() => navigate({ to: "/sessions/$id", params: { id: s.id } })}
                  >
                    <TableCell className="font-mono text-xs">
                      {s.id.length > 24 ? `${s.id.slice(0, 20)}…` : s.id}
                    </TableCell>
                    <TableCell className="text-xs text-muted-foreground">
                      {s.end_user_id || "—"}
                    </TableCell>
                    <TableCell className="text-right font-mono tabular-nums text-xs text-muted-foreground">
                      {s.trace_count}
                    </TableCell>
                    <TableCell>
                      {s.threat_count ? (
                        <Badge variant="destructive" className="text-[10px] px-1.5 py-0">
                          <AlertTriangle className="h-3 w-3" /> {s.threat_count}
                        </Badge>
                      ) : (
                        <Badge variant="outline" className="text-[10px] px-1.5 py-0 text-muted-foreground">
                          clean
                        </Badge>
                      )}
                    </TableCell>
                    <TableCell className="text-right font-mono tabular-nums text-xs text-muted-foreground">
                      {formatNumber(s.total_tokens ?? 0)}
                    </TableCell>
                    <TableCell className="text-right font-mono tabular-nums text-xs text-muted-foreground">
                      {formatCost(s.total_cost_cents ?? 0)}
                    </TableCell>
                    <TableCell className="text-right font-mono tabular-nums text-xs text-muted-foreground">
                      {formatDuration(s.wall_clock_ms ?? 0)}
                    </TableCell>
                    <TableCell className="text-right text-xs text-muted-foreground">
                      {new Date(s.last_completed_at).toLocaleString()}
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

function computeKpis(sessions: Session[]) {
  let traces = 0;
  let tokens = 0;
  let cost = 0;
  let threats = 0;
  for (const s of sessions) {
    traces += s.trace_count ?? 0;
    tokens += s.total_tokens ?? 0;
    cost += s.total_cost_cents ?? 0;
    threats += s.threat_count ?? 0;
  }
  return { traces, tokens, cost, threats };
}
