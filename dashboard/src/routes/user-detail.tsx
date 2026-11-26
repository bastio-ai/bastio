import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Link, useParams } from "@tanstack/react-router";
import { ArrowLeft } from "lucide-react";

import { api } from "@/api/client";
import type { Session, Trace, UserThreatBreakdown } from "@/api/client";
import { Badge } from "@/components/ui/badge";
import {
  AdminPageHeader,
  AdminSummaryStrip,
  AdminUnderlineNav,
} from "@/components/admin/admin-primitives";
import { cn, formatCost, formatDuration, formatNumber } from "@/lib/utils";

type UserTab = "traces" | "sessions" | "threats";

export function UserDetailPage() {
  const { id } = useParams({ from: "/users/$id" });
  const { data, isLoading, error } = useQuery({
    queryKey: ["user", id],
    queryFn: () => api.users.get(id),
  });

  const [tab, setTab] = useState<UserTab>("traces");

  if (isLoading) {
    return <LoadingBlock />;
  }
  if (error || !data?.user) {
    return (
      <div className="mx-auto max-w-[1180px] space-y-4 pb-10 pt-2 md:pt-4">
        <BackLink />
        <div className="border-y border-border-subtle py-12 text-center">
          <p className="text-sm font-medium text-foreground">User not found</p>
          <p className="mt-1 text-xs text-muted-foreground">
            This end-user id has no traces in your tenant.
          </p>
        </div>
      </div>
    );
  }

  const user = data.user;
  const sessions = (data.sessions ?? []) as Session[];
  const traces = (data.traces ?? []) as Trace[];
  const threats = (data.threat_breakdown ?? []) as UserThreatBreakdown[];

  return (
    <div className="mx-auto max-w-[1180px] pb-10 pt-2 md:pt-4">
      <AdminPageHeader
        eyebrow="End-user investigation"
        title={<span className="font-mono">{user.id}</span>}
        description={`First seen ${new Date(user.first_seen_at).toLocaleString()} · Last seen ${new Date(user.last_seen_at).toLocaleString()}`}
        bordered={false}
        className="mb-4"
        badge={
          <span className="inline-flex items-center gap-1.5 text-[11px] text-muted-foreground">
            <span
              aria-hidden="true"
              className={cn(
                "size-1.5 rounded-full",
                user.total_threats ? "bg-destructive" : "bg-success",
              )}
            />
            {user.total_threats ? `${user.total_threats} threats` : "Clean"}
          </span>
        }
        actions={<BackLink />}
      />

      <AdminSummaryStrip
        items={[
          {
            label: "Requests",
            value: formatNumber(user.total_traces),
            detail: `${user.session_count ?? sessions.length} sessions`,
          },
          {
            label: "Tokens",
            value: formatNumber(user.total_tokens ?? 0),
            detail: `${formatNumber(user.input_tokens ?? 0)} in · ${formatNumber(user.output_tokens ?? 0)} out`,
          },
          {
            label: "Attributed cost",
            value: formatCost(user.total_cost_cents ?? 0),
            detail: `${formatDuration(Math.round(user.avg_duration_ms ?? 0))} average latency`,
          },
          {
            label: "Blocked",
            value: String(user.total_blocked ?? 0),
            detail: `${user.total_threats ?? 0} threat events`,
            tone: user.total_blocked ? "danger" : "success",
          },
        ]}
      />

      <AdminUnderlineNav
        ariaLabel="End-user views"
        value={tab}
        onChange={setTab}
        items={[
          { id: "traces", label: "Traces", count: traces.length },
          { id: "sessions", label: "Sessions", count: sessions.length },
          { id: "threats", label: "Threats", count: threats.length },
        ]}
      />

      {tab === "traces" ? (
        <TracesTable traces={traces} />
      ) : tab === "sessions" ? (
        <SessionsTable sessions={sessions} />
      ) : (
        <ThreatsTable
          breakdown={threats}
          totalThreats={user.total_threats ?? 0}
        />
      )}
    </div>
  );
}

function BackLink() {
  return (
    <Link
      to="/users"
      className="inline-flex items-center gap-1.5 text-xs text-muted-foreground hover:text-foreground"
    >
      <ArrowLeft aria-hidden="true" className="h-3.5 w-3.5" /> Back to users
    </Link>
  );
}

function LoadingBlock() {
  return (
    <div className="mx-auto flex max-w-[1180px] items-center justify-center py-16">
      <div className="flex items-center gap-2 text-xs text-muted-foreground">
        <div className="h-3.5 w-3.5 animate-spin rounded-full border-2 border-muted-foreground/30 border-t-muted-foreground" />
        Loading user…
      </div>
    </div>
  );
}

function TracesTable({ traces }: { traces: Trace[] }) {
  if (!traces.length) {
    return (
      <EmptySection
        title="No traces"
        description="This user hasn't made any requests yet."
      />
    );
  }
  return (
    <div
      className="overflow-x-auto border-y border-border-subtle"
      tabIndex={0}
      role="region"
      aria-label="End-user traces"
    >
      <table className="w-full min-w-[720px] text-left text-xs">
        <thead className="border-b border-border-subtle text-[11px] text-muted-foreground">
          <tr>
            <th
              scope="col"
              className="sticky top-0 w-28 bg-background py-2.5 pr-4 font-normal"
            >
              Time
            </th>
            <th
              scope="col"
              className="sticky top-0 bg-background px-4 py-2.5 font-normal"
            >
              Name
            </th>
            <th
              scope="col"
              className="sticky top-0 bg-background px-4 py-2.5 font-normal"
            >
              Status
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
              Latency
            </th>
            <th
              scope="col"
              className="sticky top-0 bg-background py-2.5 pl-4 text-right font-normal"
            >
              Cost
            </th>
          </tr>
        </thead>
        <tbody className="divide-y divide-border-subtle">
          {traces.map((t) => (
            <tr key={t.id} className="transition-colors hover:bg-surface-1/60">
              <td
                className="py-3 pr-4 font-mono tabular-nums text-muted-foreground"
                title={new Date(t.started_at).toLocaleString()}
              >
                {relativeTime(t.started_at)}
              </td>
              <td className="px-4 py-3 font-mono text-foreground">
                <Link
                  to="/traces/$id"
                  params={{ id: t.id }}
                  className="underline decoration-border-default underline-offset-4 hover:decoration-foreground"
                >
                  {t.trace_name || t.path || t.model}
                </Link>
              </td>
              <td className="px-4 py-3">
                <Badge
                  variant={
                    t.status === "ok"
                      ? "success"
                      : t.status === "blocked"
                        ? "destructive"
                        : "warning"
                  }
                  className="px-1.5 py-0 text-[10px]"
                >
                  {t.status}
                </Badge>
              </td>
              <td className="px-4 py-3">
                {t.threat_detected ? (
                  <Badge
                    variant="destructive"
                    className="px-1.5 py-0 text-[10px]"
                  >
                    {(t.threat_types ?? []).join(", ") || "threat"}
                  </Badge>
                ) : (
                  <span className="text-muted-foreground">Clean</span>
                )}
              </td>
              <td className="px-4 py-3 text-right font-mono tabular-nums text-muted-foreground">
                {formatDuration(t.duration_ms)}
              </td>
              <td className="py-3 pl-4 text-right font-mono tabular-nums text-muted-foreground">
                {formatCost(t.cost_cents)}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function SessionsTable({ sessions }: { sessions: Session[] }) {
  if (!sessions.length) {
    return (
      <EmptySection
        title="No sessions"
        description="Pass X-Session-Id on gateway requests to group traces into sessions."
      />
    );
  }
  return (
    <div
      className="overflow-x-auto border-y border-border-subtle"
      tabIndex={0}
      role="region"
      aria-label="End-user sessions"
    >
      <table className="w-full min-w-[720px] text-left text-xs">
        <thead className="border-b border-border-subtle text-[11px] text-muted-foreground">
          <tr>
            <th
              scope="col"
              className="sticky top-0 bg-background py-2.5 pr-4 font-normal"
            >
              Session
            </th>
            <th
              scope="col"
              className="sticky top-0 bg-background px-4 py-2.5 text-right font-normal"
            >
              Traces
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
              Tokens
            </th>
            <th
              scope="col"
              className="sticky top-0 bg-background px-4 py-2.5 text-right font-normal"
            >
              Cost
            </th>
            <th
              scope="col"
              className="sticky top-0 bg-background py-2.5 pl-4 text-right font-normal"
            >
              Last activity
            </th>
          </tr>
        </thead>
        <tbody className="divide-y divide-border-subtle">
          {sessions.map((s) => (
            <tr key={s.id} className="transition-colors hover:bg-surface-1/60">
              <td className="py-3 pr-4 font-mono text-foreground">
                <Link
                  to="/sessions/$id"
                  params={{ id: s.id }}
                  className="underline decoration-border-default underline-offset-4 hover:decoration-foreground"
                >
                  {s.id.length > 28 ? `${s.id.slice(0, 24)}…` : s.id}
                </Link>
              </td>
              <td className="px-4 py-3 text-right font-mono tabular-nums text-muted-foreground">
                {s.trace_count}
              </td>
              <td className="px-4 py-3">
                {s.threat_count ? (
                  <span className="font-mono tabular-nums text-destructive">
                    {s.threat_count}{" "}
                    {s.threat_count === 1 ? "threat" : "threats"}
                  </span>
                ) : (
                  <span className="text-muted-foreground">Clean</span>
                )}
              </td>
              <td className="px-4 py-3 text-right font-mono tabular-nums text-muted-foreground">
                {formatNumber(s.total_tokens ?? 0)}
              </td>
              <td className="px-4 py-3 text-right font-mono tabular-nums text-muted-foreground">
                {formatCost(s.total_cost_cents ?? 0)}
              </td>
              <td className="py-3 pl-4 text-right tabular-nums text-muted-foreground">
                {new Date(s.last_completed_at).toLocaleString()}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function ThreatsTable({
  breakdown,
  totalThreats,
}: {
  breakdown: UserThreatBreakdown[];
  totalThreats: number;
}) {
  const max = useMemo(
    () => Math.max(1, ...breakdown.map((t) => t.count)),
    [breakdown],
  );
  if (!breakdown.length) {
    return (
      <EmptySection
        title="No threats detected"
        description="This user hasn't triggered any security detectors."
      />
    );
  }
  return (
    <div className="space-y-3 border-y border-border-subtle py-4">
      <p className="text-[11px] font-medium text-muted-foreground">
        Top detectors ({totalThreats} total hits)
      </p>
      <div className="divide-y divide-border-subtle">
        {breakdown.map((t) => (
          <div
            key={t.threat_type}
            className="grid grid-cols-[8rem_1fr_4rem_4rem] items-center gap-3 py-2.5 text-xs"
          >
            <span className="font-mono text-foreground">{t.threat_type}</span>
            <div className="h-1.5 rounded-full bg-surface-2">
              <div
                className="h-1.5 rounded-full bg-destructive/75"
                style={{ width: `${(t.count / max) * 100}%` }}
              />
            </div>
            <span className="text-right font-mono tabular-nums text-muted-foreground">
              {t.count}
            </span>
            <span className="text-right font-mono tabular-nums text-muted-foreground">
              {((t.avg_score ?? 0) * 100).toFixed(0)}%
            </span>
          </div>
        ))}
      </div>
    </div>
  );
}

function EmptySection({
  title,
  description,
}: {
  title: string;
  description: string;
}) {
  return (
    <div className="border-y border-border-subtle py-12 text-center">
      <p className="text-[13px] font-medium text-foreground">{title}</p>
      <p className="mt-1 text-xs text-muted-foreground">{description}</p>
    </div>
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
