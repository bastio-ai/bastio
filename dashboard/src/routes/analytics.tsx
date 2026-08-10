import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Activity, ShieldAlert, TrendingUp, Users } from "lucide-react";
import { Area, AreaChart, ResponsiveContainer, Tooltip, XAxis, YAxis } from "recharts";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { PageHeader, EmptyState } from "@/components/card";
import { Skeleton } from "@/components/skeleton";
import { KpiCard } from "@/components/observe/kpi-card";
import { DataPanel } from "@/components/data/data-panel";
import { api } from "@/api/client";
import type { UserAnalytics } from "@/api/client";
import { formatNumber, formatDuration, formatCost } from "@/lib/utils";

type UserSort = "cost" | "requests" | "threats" | "latency";
type RangePreset = "24h" | "7d" | "30d" | "all";

function rangeToFrom(range: RangePreset): string | undefined {
  const hours = { "24h": 24, "7d": 24 * 7, "30d": 24 * 30 }[range as "24h" | "7d" | "30d"];
  if (!hours) return undefined;
  return new Date(Date.now() - hours * 3600 * 1000).toISOString();
}

export function AnalyticsPage() {
  const [range, setRange] = useState<RangePreset>("7d");
  const queryParams = useMemo(() => ({ from: rangeToFrom(range) }), [range]);

  const { data, isLoading } = useQuery({
    queryKey: ["analytics", queryParams],
    queryFn: () => api.analytics.overview(queryParams),
  });

  const rangeSelect = (
    <Select value={range} onValueChange={(v) => setRange(v as RangePreset)}>
      <SelectTrigger className="h-8 w-32 text-xs">
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        <SelectItem value="24h">Last 24h</SelectItem>
        <SelectItem value="7d">Last 7 days</SelectItem>
        <SelectItem value="30d">Last 30 days</SelectItem>
        <SelectItem value="all">All time</SelectItem>
      </SelectContent>
    </Select>
  );

  if (isLoading) {
    return (
      <>
        <PageHeader title="Analytics" description="Request volume, cost, latency, and error rates" action={rangeSelect} />
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-2 mb-5">
          {Array.from({ length: 4 }).map((_, i) => (
            <Skeleton key={i} className="h-28" />
          ))}
        </div>
        <Skeleton className="h-56 mb-4" />
        <div className="grid grid-cols-1 lg:grid-cols-2 gap-2">
          <Skeleton className="h-64" />
          <Skeleton className="h-64" />
        </div>
      </>
    );
  }

  const d = data ?? {
    total_requests: 0, total_threats: 0, total_blocked: 0,
    total_cost_cents: 0, avg_duration_ms: 0,
    requests_by_hour: [], threats_by_type: [], top_models: [],
  };

  return (
    <>
      <PageHeader title="Analytics" description="Request volume, cost, latency, and error rates" action={rangeSelect} />

      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-2 mb-5">
        <KpiCard
          label="Total Requests"
          value={formatNumber(d.total_requests)}
        />
        <KpiCard
          label="Threats Detected"
          value={formatNumber(d.total_threats)}
          sparklineTone={d.total_threats > 0 ? "danger" : "neutral"}
        />
        <KpiCard
          label="Total Cost"
          value={formatCost(d.total_cost_cents)}
        />
        <KpiCard
          label="Avg Latency"
          value={formatDuration(d.avg_duration_ms)}
        />
      </div>

      <DataPanel title="Requests over time" className="mb-2 min-h-[260px]">
        {d.requests_by_hour.length === 0 ? (
          <EmptyState
            icon={<Activity className="h-5 w-5" />}
            title="No request activity"
            description="Send a request through the gateway to see the hourly chart populate."
          />
        ) : (
          <div className="h-56 p-4">
            <ResponsiveContainer width="100%" height="100%">
              <AreaChart data={d.requests_by_hour}>
                <defs>
                  <linearGradient id="reqGrad" x1="0" y1="0" x2="0" y2="1">
                    <stop offset="0%" stopColor="var(--text-secondary)" stopOpacity={0.3} />
                    <stop offset="100%" stopColor="var(--text-secondary)" stopOpacity={0} />
                  </linearGradient>
                </defs>
                <XAxis
                  dataKey="hour"
                  tickFormatter={(v) => new Date(v).toLocaleString([], { month: "short", day: "numeric", hour: "numeric" })}
                  stroke="var(--text-muted)"
                  strokeOpacity={0.3}
                  tick={{ fill: "var(--text-muted)", fontSize: 10, fontFamily: "var(--font-mono)" }}
                />
                <YAxis
                  stroke="var(--text-muted)"
                  strokeOpacity={0.3}
                  tick={{ fill: "var(--text-muted)", fontSize: 10, fontFamily: "var(--font-mono)" }}
                  allowDecimals={false}
                />
                <Tooltip
                  contentStyle={{
                    background: "var(--surface-3)",
                    border: "1px solid var(--border-default)",
                    borderRadius: "6px",
                    fontSize: "11px",
                    fontFamily: "var(--font-mono)",
                  }}
                  labelFormatter={(v) =>
                    typeof v === "string" || typeof v === "number"
                      ? new Date(v).toLocaleString()
                      : ""
                  }
                />
                <Area type="monotone" dataKey="count" stroke="var(--text-secondary)" strokeWidth={1} fill="url(#reqGrad)" />
              </AreaChart>
            </ResponsiveContainer>
          </div>
        )}
      </DataPanel>

      <div className="grid grid-cols-1 lg:grid-cols-2 gap-2">
        <DataPanel title="Threats by type">
          {d.threats_by_type.length === 0 ? (
            <EmptyState
              icon={<ShieldAlert className="h-5 w-5" />}
              title="No threat data"
              description="Threat distribution will appear once threats are detected."
            />
          ) : (
            <div className="p-4 space-y-3">
              {d.threats_by_type.map((t) => {
                const maxCount = Math.max(...d.threats_by_type.map((x) => x.count));
                const pct = maxCount > 0 ? (t.count / maxCount) * 100 : 0;
                return (
                  <div key={t.type}>
                    <div className="flex justify-between text-[13px] mb-1.5">
                      <span className="text-text-primary">{t.type}</span>
                      <span className="font-mono text-[11px] text-text-secondary tabular-nums">{t.count}</span>
                    </div>
                    <div className="h-1 rounded-full bg-surface-2 overflow-hidden">
                      <div
                        className="h-full rounded-full bg-danger transition-all duration-500"
                        style={{ width: `${pct}%` }}
                      />
                    </div>
                  </div>
                );
              })}
            </div>
          )}
        </DataPanel>

        <DataPanel title="Top models">
          {d.top_models.length === 0 ? (
            <EmptyState
              icon={<TrendingUp className="h-5 w-5" />}
              title="No model data"
              description="Model usage will appear once requests are processed."
            />
          ) : (
            <div className="p-2 space-y-0.5">
              {d.top_models.map((m, i) => (
                <div
                  key={m.model}
                  className="flex items-center justify-between px-3 py-2 rounded-sm hover:bg-surface-2 transition-colors"
                >
                  <div className="flex items-center gap-3">
                    <span className="font-mono text-[11px] text-text-muted tabular-nums w-4">{i + 1}</span>
                    <span className="font-mono text-[12px] text-text-primary">{m.model}</span>
                  </div>
                  <div className="flex items-center gap-4 font-mono text-[11px] text-text-secondary tabular-nums">
                    <span>{m.count} reqs</span>
                    <span>{formatCost(m.cost_cents)}</span>
                  </div>
                </div>
              ))}
            </div>
          )}
        </DataPanel>
      </div>

      <TopUsersSection />
    </>
  );
}

function TopUsersSection() {
  const [sort, setSort] = useState<UserSort>("cost");
  const { data: users = [], isLoading } = useQuery({
    queryKey: ["analytics", "users", { sort }],
    queryFn: () => api.analytics.users({ limit: 20, order_by: sort }),
  });

  const sortSelect = (
    <Select value={sort} onValueChange={(v) => setSort(v as UserSort)}>
      <SelectTrigger className="h-7 w-36 text-xs">
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        <SelectItem value="cost">by cost</SelectItem>
        <SelectItem value="requests">by requests</SelectItem>
        <SelectItem value="threats">by threats</SelectItem>
        <SelectItem value="latency">by latency</SelectItem>
      </SelectContent>
    </Select>
  );

  return (
    <DataPanel title="Top end-users" action={sortSelect} className="mt-2">
      {isLoading ? (
        <div className="flex items-center justify-center py-10">
          <div className="h-4 w-4 animate-spin rounded-full border-2 border-border-default border-t-text-secondary" />
        </div>
      ) : users.length === 0 ? (
        <EmptyState
          icon={<Users className="h-5 w-5" />}
          title="No end-user activity yet"
          description="Pass X-End-User-Id on gateway requests to see per-user cost, latency, and threat rollups here."
        />
      ) : (
        <div className="p-2 space-y-0.5">
          {users.map((u: UserAnalytics, i) => (
            <div
              key={u.end_user_id}
              className="grid grid-cols-[1.5rem_1fr_6rem_6rem_5rem_5rem] items-center gap-3 px-3 py-2 rounded-sm hover:bg-surface-2 transition-colors"
            >
              <span className="font-mono text-[11px] text-text-muted tabular-nums">{i + 1}</span>
              <span className="truncate font-mono text-[12px] text-text-primary">{u.end_user_id}</span>
              <span className="text-right font-mono text-[11px] text-text-secondary tabular-nums">
                {formatNumber(u.total_requests)} reqs
              </span>
              <span className="text-right font-mono text-[11px] text-text-secondary tabular-nums">
                {formatCost(u.total_cost_cents)}
              </span>
              <span className="text-right font-mono text-[11px] text-text-secondary tabular-nums">
                {formatDuration(u.avg_duration_ms)}
              </span>
              <span className="text-right font-mono text-[11px] tabular-nums">
                {u.total_threats > 0 ? (
                  <span className="text-danger">{u.total_threats} threats</span>
                ) : (
                  <span className="text-text-muted">—</span>
                )}
              </span>
            </div>
          ))}
        </div>
      )}
    </DataPanel>
  );
}
