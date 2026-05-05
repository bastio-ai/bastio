import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  ResponsiveContainer,
  LineChart,
  Line,
  XAxis,
  YAxis,
  Tooltip,
  CartesianGrid,
  BarChart,
  Bar,
} from "recharts";
import { ArrowDown, ArrowUp, ChevronRight, X } from "lucide-react";

import { Card, CardContent } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { SkeletonRows } from "@/components/skeleton";

import {
  workspaceApi,
  formatCents,
  formatTokens,
  type TopUserUsage,
  type ForecastResult,
  type CompareResult,
  type PeriodStats,
} from "./types";

export function AnalyticsTab() {
  const daily = useQuery({
    queryKey: ["workspace", "analytics", "daily"],
    queryFn: () => workspaceApi.analyticsDaily(14),
  });
  const byModel = useQuery({
    queryKey: ["workspace", "analytics", "by-model"],
    queryFn: workspaceApi.analyticsByModel,
  });
  const forecast = useQuery({
    queryKey: ["workspace", "analytics", "forecast"],
    queryFn: workspaceApi.analyticsForecast,
  });
  const compare = useQuery({
    queryKey: ["workspace", "analytics", "compare"],
    queryFn: workspaceApi.analyticsCompare,
  });
  const topUsers = useQuery({
    queryKey: ["workspace", "analytics", "top-users"],
    queryFn: () => workspaceApi.analyticsTopUsers(10),
  });

  const [drillUserID, setDrillUserID] = useState<string | null>(null);

  return (
    <div className="space-y-6">
      {/* Forecast — current spend, run-rate, projected month-end. */}
      <ForecastCard data={forecast.data ?? null} loading={forecast.isLoading} />

      {/* Week-over-week scoreboard. Five metrics each, deltas inline. */}
      <CompareCard data={compare.data ?? null} loading={compare.isLoading} />

      {/* Top users table. Row click → drill-down drawer. */}
      <Card>
        <CardContent className="p-6">
          <div className="mb-4 flex items-center justify-between">
            <h3 className="text-sm font-semibold">Top users this month</h3>
            <span className="text-xs text-muted-foreground">
              by cost · click a row for the 30-day breakdown
            </span>
          </div>
          {topUsers.isLoading ? (
            <SkeletonRows count={4} />
          ) : !topUsers.data?.users.length ? (
            <p className="text-sm text-muted-foreground">
              No assistant traffic yet this month.
            </p>
          ) : (
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-border text-xs uppercase tracking-wide text-muted-foreground">
                  <th className="px-2 py-2 text-left font-medium">User</th>
                  <th className="px-2 py-2 text-right font-medium">Messages</th>
                  <th className="px-2 py-2 text-right font-medium">Tokens</th>
                  <th className="px-2 py-2 text-right font-medium">Cost</th>
                  <th className="px-2 py-2"></th>
                </tr>
              </thead>
              <tbody className="divide-y divide-border">
                {topUsers.data.users.map((u) => (
                  <UserRow
                    key={u.user_id}
                    user={u}
                    onClick={() => setDrillUserID(u.user_id)}
                  />
                ))}
              </tbody>
            </table>
          )}
        </CardContent>
      </Card>

      {/* Existing charts. Kept exactly as before so nothing regresses. */}
      <Card>
        <CardContent className="p-6">
          <h3 className="mb-4 text-sm font-semibold">Usage over time (14 days)</h3>
          {daily.isLoading ? (
            <SkeletonRows count={4} />
          ) : (
            <ResponsiveContainer width="100%" height={250}>
              <LineChart
                data={daily.data?.days.map((d) => ({
                  ...d,
                  day: new Date(d.day).toLocaleDateString("en-US", {
                    month: "short",
                    day: "numeric",
                  }),
                }))}
              >
                <CartesianGrid strokeDasharray="3 3" className="stroke-border" />
                <XAxis dataKey="day" className="text-xs" />
                <YAxis className="text-xs" />
                <Tooltip />
                <Line
                  type="monotone"
                  dataKey="messages"
                  stroke="hsl(180 80% 50%)"
                  strokeWidth={2}
                  dot={false}
                />
              </LineChart>
            </ResponsiveContainer>
          )}
        </CardContent>
      </Card>

      <Card>
        <CardContent className="p-6">
          <h3 className="mb-4 text-sm font-semibold">Usage by model (this month)</h3>
          {byModel.isLoading ? (
            <SkeletonRows count={3} />
          ) : byModel.data?.by_model.length === 0 ? (
            <p className="text-sm text-muted-foreground">No usage yet.</p>
          ) : (
            <ResponsiveContainer width="100%" height={220}>
              <BarChart data={byModel.data?.by_model}>
                <CartesianGrid strokeDasharray="3 3" className="stroke-border" />
                <XAxis dataKey="model" className="text-xs" />
                <YAxis className="text-xs" />
                <Tooltip />
                <Bar dataKey="count" fill="hsl(180 80% 50%)" />
              </BarChart>
            </ResponsiveContainer>
          )}
        </CardContent>
      </Card>

      {drillUserID && (
        <UserDetailDrawer
          userID={drillUserID}
          onClose={() => setDrillUserID(null)}
        />
      )}
    </div>
  );
}

// =============================================================================
// Forecast card — current spend → daily avg → projected month-end.
// =============================================================================

function ForecastCard({
  data,
  loading,
}: {
  data: ForecastResult | null;
  loading: boolean;
}) {
  return (
    <Card>
      <CardContent className="p-6">
        <div className="mb-4 flex items-center justify-between">
          <h3 className="text-sm font-semibold">Spend forecast</h3>
          <span className="text-xs text-muted-foreground">
            this month vs last
          </span>
        </div>
        {loading || !data ? (
          <SkeletonRows count={2} />
        ) : (
          <div className="grid grid-cols-2 gap-4 sm:grid-cols-4">
            <Stat label="Current" value={formatCents(data.current_cents)} sub={`${data.days_elapsed} of ${data.days_in_month} days`} />
            <Stat label="Daily avg" value={formatCents(Math.round(data.daily_average_cents))} />
            <Stat label="Projected month-end" value={formatCents(data.projected_cents)} />
            <Stat
              label="vs last month"
              value={`${data.delta_pct_vs_last_month >= 0 ? "+" : ""}${data.delta_pct_vs_last_month.toFixed(0)}%`}
              sub={`last month ${formatCents(data.last_month_cents)}`}
              tone={data.delta_pct_vs_last_month > 10 ? "warn" : data.delta_pct_vs_last_month < -10 ? "good" : undefined}
            />
          </div>
        )}
      </CardContent>
    </Card>
  );
}

function Stat({
  label,
  value,
  sub,
  tone,
}: {
  label: string;
  value: string;
  sub?: string;
  tone?: "warn" | "good";
}) {
  const valueColor =
    tone === "warn"
      ? "text-amber-500"
      : tone === "good"
        ? "text-emerald-500"
        : "";
  return (
    <div>
      <p className="text-xs text-muted-foreground">{label}</p>
      <p className={`text-lg font-semibold ${valueColor}`}>{value}</p>
      {sub && <p className="text-[10px] text-muted-foreground">{sub}</p>}
    </div>
  );
}

// =============================================================================
// Week-over-week comparison.
// =============================================================================

function CompareCard({
  data,
  loading,
}: {
  data: CompareResult | null;
  loading: boolean;
}) {
  return (
    <Card>
      <CardContent className="p-6">
        <div className="mb-4 flex items-center justify-between">
          <h3 className="text-sm font-semibold">This week vs last week</h3>
          <span className="text-xs text-muted-foreground">rolling 7 days</span>
        </div>
        {loading || !data ? (
          <SkeletonRows count={2} />
        ) : (
          <div className="grid grid-cols-2 gap-4 sm:grid-cols-3 lg:grid-cols-5">
            <Delta label="Messages" cur={data.this_week.messages} prev={data.last_week.messages} fmt={(n) => n.toLocaleString()} />
            <Delta label="Tokens" cur={data.this_week.tokens} prev={data.last_week.tokens} fmt={formatTokens} />
            <Delta label="Cost" cur={data.this_week.cost_cents} prev={data.last_week.cost_cents} fmt={formatCents} />
            <Delta label="Active users" cur={data.this_week.active_users} prev={data.last_week.active_users} fmt={(n) => n.toLocaleString()} />
            <Delta label="Conversations" cur={data.this_week.conversations} prev={data.last_week.conversations} fmt={(n) => n.toLocaleString()} />
          </div>
        )}
      </CardContent>
    </Card>
  );
}

function Delta({
  label,
  cur,
  prev,
  fmt,
}: {
  label: string;
  cur: number;
  prev: number;
  fmt: (n: number) => string;
}) {
  const diff = cur - prev;
  const pct = prev > 0 ? (diff / prev) * 100 : cur > 0 ? 100 : 0;
  const arrow =
    diff > 0 ? <ArrowUp className="h-3 w-3" /> : diff < 0 ? <ArrowDown className="h-3 w-3" /> : null;
  // Heuristic colors: more usage = warning (cost direction), unless
  // the metric is "good more of" like active_users where rising is
  // healthy. Keep neutral for now — a precise good/bad classifier
  // would need per-metric semantics.
  return (
    <div>
      <p className="text-xs text-muted-foreground">{label}</p>
      <p className="text-lg font-semibold">{fmt(cur)}</p>
      <p className="flex items-center gap-1 text-[10px] text-muted-foreground">
        {arrow}
        <span>
          {diff >= 0 ? "+" : ""}
          {pct.toFixed(0)}% vs last week
        </span>
      </p>
    </div>
  );
}

// =============================================================================
// Per-user table row + drill-down drawer.
// =============================================================================

function UserRow({
  user,
  onClick,
}: {
  user: TopUserUsage;
  onClick: () => void;
}) {
  return (
    <tr
      onClick={onClick}
      className="cursor-pointer hover:bg-muted/50"
    >
      <td className="px-2 py-2">
        <p className="font-medium">{user.email || user.user_id}</p>
        {user.email && (
          <p className="font-mono text-[10px] text-muted-foreground">
            {user.user_id.slice(0, 8)}…
          </p>
        )}
      </td>
      <td className="px-2 py-2 text-right">{user.messages.toLocaleString()}</td>
      <td className="px-2 py-2 text-right">{formatTokens(user.tokens)}</td>
      <td className="px-2 py-2 text-right font-mono">{formatCents(user.cost_cents)}</td>
      <td className="px-2 py-2 text-right text-muted-foreground">
        <ChevronRight className="inline h-4 w-4" />
      </td>
    </tr>
  );
}

function UserDetailDrawer({
  userID,
  onClose,
}: {
  userID: string;
  onClose: () => void;
}) {
  const detail = useQuery({
    queryKey: ["workspace", "analytics", "user-detail", userID],
    queryFn: () => workspaceApi.analyticsUserDetail(userID),
  });

  return (
    <div
      className="fixed inset-0 z-50 flex justify-end bg-black/30"
      onClick={onClose}
    >
      <aside
        onClick={(e) => e.stopPropagation()}
        className="h-full w-full max-w-md overflow-y-auto border-l border-border bg-background p-6"
      >
        <div className="mb-4 flex items-center justify-between">
          <h3 className="text-sm font-semibold">User detail</h3>
          <Button size="sm" variant="ghost" onClick={onClose}>
            <X className="h-4 w-4" />
          </Button>
        </div>

        {detail.isLoading || !detail.data ? (
          <SkeletonRows count={5} />
        ) : (
          <>
            <div className="mb-6">
              <p className="text-sm font-medium">
                {detail.data.email || detail.data.user_id}
              </p>
              {detail.data.email && (
                <p className="font-mono text-xs text-muted-foreground">
                  {detail.data.user_id}
                </p>
              )}
              <p className="mt-1 text-xs text-muted-foreground">
                last 30 days
              </p>
            </div>

            <div className="mb-6 grid grid-cols-3 gap-4">
              <Stat
                label="Messages"
                value={detail.data.total_messages.toLocaleString()}
              />
              <Stat
                label="Tokens"
                value={formatTokens(detail.data.total_tokens)}
              />
              <Stat
                label="Cost"
                value={formatCents(detail.data.total_cost_cents)}
              />
            </div>

            <h4 className="mb-2 text-xs font-semibold uppercase tracking-wide text-muted-foreground">
              30-day token spend
            </h4>
            {detail.data.daily.length === 0 ? (
              <p className="mb-6 text-sm text-muted-foreground">No traffic.</p>
            ) : (
              <ResponsiveContainer width="100%" height={160}>
                <LineChart
                  data={detail.data.daily.map((d) => ({
                    ...d,
                    day: new Date(d.day).toLocaleDateString("en-US", {
                      month: "short",
                      day: "numeric",
                    }),
                  }))}
                >
                  <CartesianGrid strokeDasharray="3 3" className="stroke-border" />
                  <XAxis dataKey="day" className="text-xs" />
                  <YAxis className="text-xs" />
                  <Tooltip />
                  <Line
                    type="monotone"
                    dataKey="tokens"
                    stroke="hsl(180 80% 50%)"
                    strokeWidth={2}
                    dot={false}
                  />
                </LineChart>
              </ResponsiveContainer>
            )}

            <h4 className="mb-2 mt-6 text-xs font-semibold uppercase tracking-wide text-muted-foreground">
              Top assistants
            </h4>
            {detail.data.top_assistants.length === 0 ? (
              <p className="text-sm text-muted-foreground">
                No assistant-bound traffic.
              </p>
            ) : (
              <ul className="divide-y divide-border">
                {detail.data.top_assistants.map((a) => (
                  <li
                    key={a.assistant_id}
                    className="flex items-center justify-between py-2 text-sm"
                  >
                    <span>{a.assistant_name}</span>
                    <span className="font-mono text-xs text-muted-foreground">
                      {a.messages} msg · {formatTokens(a.tokens)} tok
                    </span>
                  </li>
                ))}
              </ul>
            )}
          </>
        )}
      </aside>
    </div>
  );
}

// PeriodStats is exported from types.ts; re-importing to keep the
// file self-documenting about the comparison shape.
export type { PeriodStats };
