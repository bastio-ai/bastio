import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { AlertTriangle, CircleDollarSign, Clock, Users as UsersIcon } from "lucide-react";

import { api } from "@/api/client";
import type { UserAnalytics } from "@/api/client";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent } from "@/components/ui/card";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
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
import { formatCost, formatDuration, formatNumber } from "@/lib/utils";

type OrderBy = "cost" | "requests" | "threats" | "latency";

export function UsersPage() {
  const navigate = useNavigate();
  const [orderBy, setOrderBy] = useState<OrderBy>("cost");

  const { data: users = [], isLoading } = useQuery({
    queryKey: ["users", orderBy],
    queryFn: () => api.users.list({ limit: 200, order_by: orderBy }),
  });

  const kpis = useMemo(() => computeKpis(users), [users]);

  return (
    <>
      <PageHeader
        title="Users"
        description="Every end-user behind X-End-User-Id — cost, latency, threats, usage."
      />

      <div className="mt-4 grid grid-cols-2 gap-3 sm:grid-cols-4">
        <KpiCard
          label="Users"
          value={formatNumber(users.length)}
          icon={<UsersIcon className="h-4 w-4" />}
        />
        <KpiCard
          label="Requests"
          value={formatNumber(kpis.requests)}
          sub={users.length ? `avg ${Math.round(kpis.requests / users.length)} / user` : ""}
        />
        <KpiCard
          label="Cost"
          value={formatCost(kpis.cost)}
          icon={<CircleDollarSign className="h-4 w-4" />}
        />
        <KpiCard
          label="Threats"
          value={formatNumber(kpis.threats)}
          tone={kpis.threats ? "danger" : "success"}
          icon={<AlertTriangle className="h-4 w-4" />}
        />
      </div>

      <Card className="border-border/50 mb-3 mt-4">
        <CardContent className="flex items-center gap-2 p-3">
          <span className="text-[10px] font-semibold uppercase tracking-wider text-muted-foreground/60">
            Sort by
          </span>
          <Select value={orderBy} onValueChange={(v) => setOrderBy(v as OrderBy)}>
            <SelectTrigger className="h-8 w-40 text-xs">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="cost">Cost (desc)</SelectItem>
              <SelectItem value="requests">Requests (desc)</SelectItem>
              <SelectItem value="threats">Threats (desc)</SelectItem>
              <SelectItem value="latency">Avg latency (desc)</SelectItem>
            </SelectContent>
          </Select>
        </CardContent>
      </Card>

      <Card className="border-border/50 overflow-hidden">
        <CardContent className="p-0">
          {isLoading ? (
            <SkeletonRows count={8} />
          ) : !users.length ? (
            <EmptyState
              icon={<UsersIcon className="h-6 w-6" />}
              title="No end-users yet"
              description="Pass X-End-User-Id on gateway requests to track activity per user."
            />
          ) : (
            <Table>
              <TableHeader>
                <TableRow className="hover:bg-transparent border-border/50">
                  <TableHead className="h-10 text-[11px] font-semibold uppercase tracking-wider text-muted-foreground/60">
                    End user
                  </TableHead>
                  <TableHead className="h-10 text-right text-[11px] font-semibold uppercase tracking-wider text-muted-foreground/60">
                    Requests
                  </TableHead>
                  <TableHead className="h-10 text-[11px] font-semibold uppercase tracking-wider text-muted-foreground/60">
                    Threats
                  </TableHead>
                  <TableHead className="h-10 text-right text-[11px] font-semibold uppercase tracking-wider text-muted-foreground/60">
                    Cost
                  </TableHead>
                  <TableHead className="h-10 text-right text-[11px] font-semibold uppercase tracking-wider text-muted-foreground/60">
                    Avg latency
                  </TableHead>
                  <TableHead className="h-10 text-right text-[11px] font-semibold uppercase tracking-wider text-muted-foreground/60">
                    Last seen
                  </TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {users.map((u: UserAnalytics) => (
                  <TableRow
                    key={u.end_user_id}
                    className="cursor-pointer border-border/30 hover:bg-muted/30"
                    onClick={() => navigate({ to: "/users/$id", params: { id: u.end_user_id } })}
                  >
                    <TableCell className="font-mono text-xs">{u.end_user_id}</TableCell>
                    <TableCell className="text-right font-mono tabular-nums text-xs text-muted-foreground">
                      {formatNumber(u.total_requests)}
                    </TableCell>
                    <TableCell>
                      {u.total_threats ? (
                        <Badge variant="destructive" className="text-[10px] px-1.5 py-0">
                          <AlertTriangle className="h-3 w-3" /> {u.total_threats}
                        </Badge>
                      ) : (
                        <Badge variant="outline" className="text-[10px] px-1.5 py-0 text-muted-foreground">
                          clean
                        </Badge>
                      )}
                    </TableCell>
                    <TableCell className="text-right font-mono tabular-nums text-xs text-muted-foreground">
                      {formatCost(u.total_cost_cents)}
                    </TableCell>
                    <TableCell className="text-right font-mono tabular-nums text-xs text-muted-foreground">
                      <span className="inline-flex items-center gap-1">
                        <Clock className="h-3 w-3" />
                        {formatDuration(Math.round(u.avg_duration_ms))}
                      </span>
                    </TableCell>
                    <TableCell className="text-right text-xs text-muted-foreground">
                      {new Date(u.last_request_at).toLocaleString()}
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

function computeKpis(users: UserAnalytics[]) {
  let requests = 0;
  let cost = 0;
  let threats = 0;
  for (const u of users) {
    requests += u.total_requests ?? 0;
    cost += u.total_cost_cents ?? 0;
    threats += u.total_threats ?? 0;
  }
  return { requests, cost, threats };
}
