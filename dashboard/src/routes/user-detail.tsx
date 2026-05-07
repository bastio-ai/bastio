import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Link, useNavigate, useParams } from "@tanstack/react-router";
import { AlertTriangle, ArrowLeft, MessagesSquare } from "lucide-react";

import { api } from "@/api/client";
import type { Session, Trace, UserThreatBreakdown } from "@/api/client";
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
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { KpiCard } from "@/components/observe/kpi-card";
import { formatCost, formatDuration, formatNumber } from "@/lib/utils";

export function UserDetailPage() {
  const { id } = useParams({ from: "/users/$id" });
  const navigate = useNavigate();
  const { data, isLoading, error } = useQuery({
    queryKey: ["user", id],
    queryFn: () => api.users.get(id),
  });

  const [tab, setTab] = useState("traces");

  if (isLoading) {
    return <LoadingBlock />;
  }
  if (error || !data?.user) {
    return (
      <div className="space-y-4">
        <BackLink />
        <Card className="border-border/50">
          <CardContent className="py-12 text-center">
            <p className="text-sm font-medium">User not found</p>
            <p className="mt-1 text-xs text-muted-foreground">
              This end-user id has no traces in your tenant.
            </p>
          </CardContent>
        </Card>
      </div>
    );
  }

  const user = data.user;
  const sessions = (data.sessions ?? []) as Session[];
  const traces = (data.traces ?? []) as Trace[];
  const threats = (data.threat_breakdown ?? []) as UserThreatBreakdown[];

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-3">
          <BackLink />
          <span className="font-mono text-sm font-semibold">{user.id}</span>
          {user.total_threats ? (
            <Badge variant="destructive" className="text-[10px] px-1.5 py-0">
              <AlertTriangle className="h-3 w-3" /> {user.total_threats}
            </Badge>
          ) : (
            <Badge variant="outline" className="text-[10px] px-1.5 py-0 text-muted-foreground">
              clean
            </Badge>
          )}
        </div>
        <span className="text-[11px] text-muted-foreground">
          first seen {new Date(user.first_seen_at).toLocaleString()} · last seen{" "}
          {new Date(user.last_seen_at).toLocaleString()}
        </span>
      </div>

      <div className="grid grid-cols-2 gap-3 sm:grid-cols-4 lg:grid-cols-6">
        <KpiCard label="Requests" value={formatNumber(user.total_traces)} />
        <KpiCard label="Sessions" value={String(user.session_count ?? sessions.length)} />
        <KpiCard
          label="Tokens"
          value={formatNumber(user.total_tokens ?? 0)}
          sub={`${formatNumber(user.input_tokens ?? 0)} → ${formatNumber(user.output_tokens ?? 0)}`}
        />
        <KpiCard label="Cost" value={formatCost(user.total_cost_cents ?? 0)} />
        <KpiCard
          label="Avg latency"
          value={formatDuration(Math.round(user.avg_duration_ms ?? 0))}
        />
        <KpiCard
          label="Blocked"
          value={String(user.total_blocked ?? 0)}
          tone={user.total_blocked ? "danger" : "default"}
        />
      </div>

      <Tabs value={tab} onValueChange={setTab}>
        <TabsList variant="line">
          <TabsTrigger value="traces">Traces ({traces.length})</TabsTrigger>
          <TabsTrigger value="sessions">Sessions ({sessions.length})</TabsTrigger>
          <TabsTrigger value="threats">Threats ({threats.length})</TabsTrigger>
        </TabsList>
        <TabsContent value="traces" className="pt-3">
          <TracesTable
            traces={traces}
            onOpen={(traceID) => navigate({ to: "/traces/$id", params: { id: traceID } })}
          />
        </TabsContent>
        <TabsContent value="sessions" className="pt-3">
          <SessionsTable
            sessions={sessions}
            onOpen={(sessionID) =>
              navigate({ to: "/sessions/$id", params: { id: sessionID } })
            }
          />
        </TabsContent>
        <TabsContent value="threats" className="pt-3">
          <ThreatsTable breakdown={threats} totalThreats={user.total_threats ?? 0} />
        </TabsContent>
      </Tabs>
    </div>
  );
}

function BackLink() {
  return (
    <Link
      to="/users"
      className="inline-flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground"
    >
      <ArrowLeft className="h-3.5 w-3.5" /> Back to users
    </Link>
  );
}

function LoadingBlock() {
  return (
    <div className="flex items-center justify-center py-16">
      <div className="flex items-center gap-2 text-sm text-muted-foreground">
        <div className="h-4 w-4 animate-spin rounded-full border-2 border-muted-foreground/30 border-t-muted-foreground" />
        Loading user…
      </div>
    </div>
  );
}

function TracesTable({
  traces,
  onOpen,
}: {
  traces: Trace[];
  onOpen: (id: string) => void;
}) {
  if (!traces.length) {
    return (
      <EmptyCard title="No traces" description="This user hasn't made any requests yet." />
    );
  }
  return (
    <Card className="border-border/50 overflow-hidden">
      <CardContent className="p-0">
        <Table>
          <TableHeader>
            <TableRow className="hover:bg-transparent border-border/50">
              <TableHead className="h-10 w-[7rem] text-[11px] font-semibold uppercase tracking-wider text-muted-foreground/60">
                Time
              </TableHead>
              <TableHead className="h-10 text-[11px] font-semibold uppercase tracking-wider text-muted-foreground/60">
                Name
              </TableHead>
              <TableHead className="h-10 text-[11px] font-semibold uppercase tracking-wider text-muted-foreground/60">
                Status
              </TableHead>
              <TableHead className="h-10 text-[11px] font-semibold uppercase tracking-wider text-muted-foreground/60">
                Security
              </TableHead>
              <TableHead className="h-10 text-right text-[11px] font-semibold uppercase tracking-wider text-muted-foreground/60">
                Latency
              </TableHead>
              <TableHead className="h-10 text-right text-[11px] font-semibold uppercase tracking-wider text-muted-foreground/60">
                Cost
              </TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {traces.map((t) => (
              <TableRow
                key={t.id}
                className="cursor-pointer border-border/30 hover:bg-muted/30"
                onClick={() => onOpen(t.id)}
              >
                <TableCell
                  className="font-mono tabular-nums text-xs text-muted-foreground"
                  title={new Date(t.started_at).toLocaleString()}
                >
                  {relativeTime(t.started_at)}
                </TableCell>
                <TableCell className="font-mono text-xs text-foreground/90">
                  {t.trace_name || t.path || t.model}
                </TableCell>
                <TableCell>
                  <Badge
                    variant={
                      t.status === "ok"
                        ? "success"
                        : t.status === "blocked"
                        ? "destructive"
                        : "warning"
                    }
                    className="text-[10px] px-1.5 py-0"
                  >
                    {t.status}
                  </Badge>
                </TableCell>
                <TableCell>
                  {t.threat_detected ? (
                    <Badge variant="destructive" className="text-[10px] px-1.5 py-0">
                      {(t.threat_types ?? []).join(", ") || "threat"}
                    </Badge>
                  ) : (
                    <Badge variant="outline" className="text-[10px] px-1.5 py-0 text-muted-foreground">
                      clean
                    </Badge>
                  )}
                </TableCell>
                <TableCell className="text-right font-mono tabular-nums text-xs text-muted-foreground">
                  {formatDuration(t.duration_ms)}
                </TableCell>
                <TableCell className="text-right font-mono tabular-nums text-xs text-muted-foreground">
                  {formatCost(t.cost_cents)}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </CardContent>
    </Card>
  );
}

function SessionsTable({
  sessions,
  onOpen,
}: {
  sessions: Session[];
  onOpen: (id: string) => void;
}) {
  if (!sessions.length) {
    return (
      <EmptyCard
        title="No sessions"
        description="Pass X-Session-Id on gateway requests to group traces into sessions."
      />
    );
  }
  return (
    <Card className="border-border/50 overflow-hidden">
      <CardContent className="p-0">
        <Table>
          <TableHeader>
            <TableRow className="hover:bg-transparent border-border/50">
              <TableHead className="h-10 text-[11px] font-semibold uppercase tracking-wider text-muted-foreground/60">
                Session
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
                Last activity
              </TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {sessions.map((s) => (
              <TableRow
                key={s.id}
                className="cursor-pointer border-border/30 hover:bg-muted/30"
                onClick={() => onOpen(s.id)}
              >
                <TableCell className="font-mono text-xs">
                  <MessagesSquare className="mr-1 inline h-3 w-3 text-muted-foreground" />
                  {s.id.length > 28 ? `${s.id.slice(0, 24)}…` : s.id}
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
                <TableCell className="text-right text-xs text-muted-foreground">
                  {new Date(s.last_completed_at).toLocaleString()}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </CardContent>
    </Card>
  );
}

function ThreatsTable({
  breakdown,
  totalThreats,
}: {
  breakdown: UserThreatBreakdown[];
  totalThreats: number;
}) {
  if (!breakdown.length) {
    return (
      <EmptyCard
        title="No threats detected"
        description="This user hasn't triggered any security detectors."
      />
    );
  }
  const max = useMemo(() => Math.max(1, ...breakdown.map((t) => t.count)), [breakdown]);
  return (
    <Card className="border-border/50">
      <CardContent className="p-3 space-y-2">
        <p className="text-[10px] font-semibold uppercase tracking-wider text-muted-foreground/60">
          Top detectors ({totalThreats} total hits)
        </p>
        {breakdown.map((t) => (
          <div key={t.threat_type} className="grid grid-cols-[8rem_1fr_5rem_5rem] items-center gap-2 text-xs">
            <span className="font-mono">{t.threat_type}</span>
            <div className="h-2 rounded bg-muted/50">
              <div
                className="h-2 rounded bg-destructive/70"
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
      </CardContent>
    </Card>
  );
}

function EmptyCard({ title, description }: { title: string; description: string }) {
  return (
    <Card className="border-border/50">
      <CardContent className="py-12 text-center">
        <p className="text-sm font-medium">{title}</p>
        <p className="mt-1 text-xs text-muted-foreground">{description}</p>
      </CardContent>
    </Card>
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
