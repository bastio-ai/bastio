import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { Search, Users as UsersIcon } from "lucide-react";

import { api } from "@/api/client";
import type { UserAnalytics } from "@/api/client";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { EmptyState } from "@/components/card";
import {
  AdminPageHeader,
  AdminSummaryStrip,
} from "@/components/admin/admin-primitives";
import { SkeletonRows } from "@/components/skeleton";
import { cn, formatCost, formatDuration, formatNumber } from "@/lib/utils";

type OrderBy = "cost" | "requests" | "threats" | "latency";

export function UsersPage() {
  const [orderBy, setOrderBy] = useState<OrderBy>("cost");
  const [search, setSearch] = useState("");

  const { data: users = [], isLoading } = useQuery({
    queryKey: ["users", orderBy],
    meta: { dashboardLive: true },
    refetchOnWindowFocus: false,
    queryFn: () => api.users.list({ limit: 200, order_by: orderBy }),
  });

  const kpis = useMemo(() => computeKpis(users), [users]);
  const visibleUsers = useMemo(() => {
    const query = search.trim().toLowerCase();
    if (!query) return users;
    return users.filter((user) =>
      user.end_user_id.toLowerCase().includes(query),
    );
  }, [search, users]);

  return (
    <div className="mx-auto max-w-[1180px] pb-10 pt-2 md:pt-4">
      <AdminPageHeader
        eyebrow="Observe"
        title="End users"
        description="Investigate request volume, cost, latency, and security outcomes for every identity supplied through X-End-User-Id."
        bordered={false}
        className="mb-4"
        badge={
          <span className="font-mono text-[11px] tabular-nums text-muted-foreground">
            {formatNumber(users.length)} tracked
          </span>
        }
      />

      <AdminSummaryStrip
        items={[
          {
            label: "Tracked users",
            value: formatNumber(users.length),
            detail: "Distinct end-user identifiers",
          },
          {
            label: "Requests",
            value: formatNumber(kpis.requests),
            detail: users.length
              ? `Average ${Math.round(kpis.requests / users.length)} per user`
              : "No activity",
          },
          {
            label: "Attributed cost",
            value: formatCost(kpis.cost),
            detail: "Across tracked requests",
          },
          {
            label: "Threat events",
            value: formatNumber(kpis.threats),
            detail: kpis.threats ? "Requires review" : "No detections",
            tone: kpis.threats ? "danger" : "success",
          },
        ]}
      />

      <search className="mb-4 flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <div className="relative w-full sm:w-72">
          <label htmlFor="end-users-search" className="sr-only">
            Search end-user identifier
          </label>
          <Search
            aria-hidden="true"
            className="pointer-events-none absolute left-2.5 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground"
          />
          <Input
            id="end-users-search"
            type="search"
            autoCorrect="off"
            autoCapitalize="none"
            spellCheck={false}
            value={search}
            onChange={(event) => setSearch(event.target.value)}
            placeholder="Search end-user id…"
            className="h-8 pl-8 text-xs"
          />
        </div>
        <div className="flex items-center gap-2.5">
          <span className="text-[11px] text-muted-foreground">Sort by</span>
          <Select
            value={orderBy}
            onValueChange={(v) => setOrderBy(v as OrderBy)}
          >
            <SelectTrigger
              aria-label="Sort end users"
              className="h-8 w-full text-xs sm:w-44"
            >
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="cost">Cost (desc)</SelectItem>
              <SelectItem value="requests">Requests (desc)</SelectItem>
              <SelectItem value="threats">Threats (desc)</SelectItem>
              <SelectItem value="latency">Avg latency (desc)</SelectItem>
            </SelectContent>
          </Select>
        </div>
      </search>

      <div className="border-y border-border-subtle">
        {isLoading ? (
          <div className="py-4">
            <SkeletonRows count={6} />
          </div>
        ) : !users.length ? (
          <EmptyState
            icon={<UsersIcon className="size-5" />}
            title="No end-users yet"
            description="Pass X-End-User-Id on gateway requests to track activity per user."
          />
        ) : !visibleUsers.length ? (
          <EmptyState
            icon={<Search className="size-5" />}
            title="No matching end users"
            description="Try a shorter identifier or clear the search field."
          />
        ) : (
          <div
            className="overflow-x-auto"
            tabIndex={0}
            role="region"
            aria-label="Tracked end users"
          >
            <table className="w-full min-w-[720px] text-left text-xs">
              <thead className="border-b border-border-subtle text-[11px] text-muted-foreground">
                <tr>
                  <th
                    scope="col"
                    className="sticky top-0 bg-background py-2.5 pr-4 font-normal"
                  >
                    End user
                  </th>
                  <th
                    scope="col"
                    className="sticky top-0 bg-background px-4 py-2.5 text-right font-normal"
                  >
                    Requests
                  </th>
                  <th
                    scope="col"
                    className="sticky top-0 bg-background px-4 py-2.5 font-normal"
                  >
                    Security
                  </th>
                  <th
                    scope="col"
                    className="sticky top-0 bg-background px-4 py-2.5 text-right font-normal"
                  >
                    Cost
                  </th>
                  <th
                    scope="col"
                    className="sticky top-0 bg-background px-4 py-2.5 text-right font-normal"
                  >
                    Avg latency
                  </th>
                  <th
                    scope="col"
                    className="sticky top-0 bg-background py-2.5 pl-4 text-right font-normal"
                  >
                    Last seen
                  </th>
                </tr>
              </thead>
              <tbody className="divide-y divide-border-subtle">
                {visibleUsers.map((u: UserAnalytics) => (
                  <tr
                    key={u.end_user_id}
                    className="transition-colors hover:bg-surface-1/60"
                  >
                    <td className="py-3 pr-4 font-mono text-xs">
                      <Link
                        to="/users/$id"
                        params={{ id: u.end_user_id }}
                        className="text-foreground underline decoration-border-default underline-offset-4 hover:decoration-foreground"
                      >
                        {u.end_user_id}
                      </Link>
                    </td>
                    <td className="px-4 py-3 text-right font-mono tabular-nums text-muted-foreground">
                      {formatNumber(u.total_requests)}
                    </td>
                    <td className="px-4 py-3">
                      <span className="inline-flex items-center gap-1.5 text-xs">
                        <span
                          aria-hidden="true"
                          className={cn(
                            "size-1.5 rounded-full",
                            u.total_threats ? "bg-destructive" : "bg-success",
                          )}
                        />
                        <span
                          className={
                            u.total_threats
                              ? "font-mono tabular-nums text-destructive"
                              : "text-muted-foreground"
                          }
                        >
                          {u.total_threats
                            ? `${u.total_threats} ${u.total_threats === 1 ? "threat" : "threats"}`
                            : "Clean"}
                        </span>
                      </span>
                    </td>
                    <td className="px-4 py-3 text-right font-mono tabular-nums text-muted-foreground">
                      {formatCost(u.total_cost_cents)}
                    </td>
                    <td className="px-4 py-3 text-right font-mono tabular-nums text-muted-foreground">
                      {formatDuration(Math.round(u.avg_duration_ms))}
                    </td>
                    <td className="py-3 pl-4 text-right tabular-nums text-muted-foreground">
                      {new Date(u.last_request_at).toLocaleString()}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </div>
    </div>
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
