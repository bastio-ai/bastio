import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { AlertTriangle, Clock, Search, Users as UsersIcon } from "lucide-react";

import { api } from "@/api/client";
import type { UserAnalytics } from "@/api/client";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
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
import { EmptyState } from "@/components/card";
import { AdminPageHeader, AdminSummaryStrip } from "@/components/admin/admin-primitives";
import { SkeletonRows } from "@/components/skeleton";
import { formatCost, formatDuration, formatNumber } from "@/lib/utils";

type OrderBy = "cost" | "requests" | "threats" | "latency";

export function UsersPage() {
  const navigate = useNavigate();
  const [orderBy, setOrderBy] = useState<OrderBy>("cost");
  const [search, setSearch] = useState("");

  const { data: users = [], isLoading } = useQuery({
    queryKey: ["users", orderBy],
    queryFn: () => api.users.list({ limit: 200, order_by: orderBy }),
  });

  const kpis = useMemo(() => computeKpis(users), [users]);
  const visibleUsers = useMemo(() => {
    const query = search.trim().toLowerCase();
    if (!query) return users;
    return users.filter((user) => user.end_user_id.toLowerCase().includes(query));
  }, [search, users]);

  return (
    <>
      <AdminPageHeader
        eyebrow="Identity telemetry"
        title="End users"
        description="Investigate request volume, cost, latency, and security outcomes for every identity supplied through X-End-User-Id."
        badge={<Badge variant="outline" className="font-mono text-[10px]">{formatNumber(users.length)} tracked</Badge>}
      />

      <AdminSummaryStrip items={[
        { label: "Tracked users", value: formatNumber(users.length), detail: "Distinct end-user identifiers" },
        { label: "Requests", value: formatNumber(kpis.requests), detail: users.length ? `Average ${Math.round(kpis.requests / users.length)} per user` : "No activity" },
        { label: "Attributed cost", value: formatCost(kpis.cost), detail: "Across tracked requests" },
        { label: "Threat events", value: formatNumber(kpis.threats), detail: kpis.threats ? "Requires review" : "No detections", tone: kpis.threats ? "danger" : "success" },
      ]} />

      <Card className="mb-3 border-border/60">
        <CardContent className="flex flex-col gap-2 p-3 sm:flex-row sm:items-center">
          <div className="relative min-w-0 flex-1">
            <Search className="pointer-events-none absolute left-2.5 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground" />
            <Input value={search} onChange={(event) => setSearch(event.target.value)} placeholder="Search end-user id…" className="h-8 pl-8 text-xs" />
          </div>
          <span className="text-[10px] font-medium uppercase tracking-[0.12em] text-muted-foreground">Sort</span>
          <Select value={orderBy} onValueChange={(v) => setOrderBy(v as OrderBy)}>
            <SelectTrigger className="h-8 w-full text-xs sm:w-44">
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
          ) : !visibleUsers.length ? (
            <EmptyState
              icon={<Search className="h-6 w-6" />}
              title="No matching end users"
              description="Try a shorter identifier or clear the search field."
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
                {visibleUsers.map((u: UserAnalytics) => (
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
