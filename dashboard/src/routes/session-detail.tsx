import { useEffect, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Link, useParams } from "@tanstack/react-router";
import { ArrowLeft, AlertTriangle } from "lucide-react";

import { api } from "@/api/client";
import type { Observation, Trace, TraceDetail, TraceThreatDetection } from "@/api/client";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent } from "@/components/ui/card";
import { AdminPageHeader, AdminSummaryStrip } from "@/components/admin/admin-primitives";
import { ResizablePanels } from "@/components/observe/resizable-panels";
import { SpanDetailTabs } from "@/components/observe/span-detail-tabs";
import { SpanTree } from "@/components/observe/span-tree";
import { cn, formatCost, formatDuration, formatNumber } from "@/lib/utils";

export function SessionDetailPage() {
  const { id } = useParams({ from: "/sessions/$id" });
  const { data, isLoading, error } = useQuery({
    queryKey: ["session", id],
    queryFn: () => api.sessions.get(id),
  });

  if (isLoading) {
    return <LoadingBlock />;
  }
  if (error || !data?.session) {
    return (
      <div className="space-y-4">
        <BackLink />
        <Card className="border-border/50">
          <CardContent className="py-12 text-center">
            <p className="text-sm font-medium">Session not found</p>
            <p className="mt-1 text-xs text-muted-foreground">
              This session id has no traces, or is outside your tenant.
            </p>
          </CardContent>
        </Card>
      </div>
    );
  }

  const session = data.session;
  const traces = (data.traces ?? []) as Trace[];
  const firstTrace = traces[0];

  return (
    <div className="flex h-[calc(100vh-6.5rem)] flex-col gap-3">
      <AdminPageHeader
        eyebrow="Session investigation"
        title={<span className="font-mono">{session.id}</span>}
        description={`${session.trace_count} related traces · ${session.end_user_id || "unknown end-user"}`}
        badge={session.threat_count ? (
            <Badge variant="destructive" className="text-[10px] px-1.5 py-0">
              <AlertTriangle className="h-3 w-3" /> {session.threat_count} threats
            </Badge>
          ) : (
            <Badge variant="outline" className="text-[10px] px-1.5 py-0 text-muted-foreground">
              clean
            </Badge>
          )}
        actions={<BackLink />}
        className="mb-0"
      />

      <AdminSummaryStrip items={[
        { label: "Traces", value: String(session.trace_count), detail: `${session.error_count ?? 0} errors` },
        { label: "Tokens", value: formatNumber(session.total_tokens ?? 0), detail: `${formatNumber(session.input_tokens ?? 0)} in · ${formatNumber(session.output_tokens ?? 0)} out` },
        { label: "Cost", value: formatCost(session.total_cost_cents ?? 0), detail: "Session total" },
        { label: "Wall clock", value: formatDuration(session.wall_clock_ms ?? 0), detail: `${session.blocked_count ?? 0} blocked`, tone: session.blocked_count ? "danger" : "default" },
      ]} />

      <div className="min-h-0 flex-1 overflow-hidden rounded-xl border border-border/70 bg-card">
        <SessionSplit traces={traces} initialTraceId={firstTrace?.id ?? null} />
      </div>
    </div>
  );
}

function SessionSplit({
  traces,
  initialTraceId,
}: {
  traces: Trace[];
  initialTraceId: string | null;
}) {
  const [activeTraceId, setActiveTraceId] = useState<string | null>(initialTraceId);
  useEffect(() => {
    if (!activeTraceId && initialTraceId) setActiveTraceId(initialTraceId);
  }, [activeTraceId, initialTraceId]);

  return (
    <ResizablePanels
      storageKey="session-detail-split"
      defaultLeftPct={28}
      minLeftPx={240}
      left={
        <div className="h-full">
          <div className="border-b border-border/40 bg-muted/10 px-3 py-2 text-[10px] font-semibold uppercase tracking-wider text-muted-foreground/60">
            Traces in session ({traces.length})
          </div>
          <div className="divide-y divide-border/20">
            {traces.map((t, idx) => (
              <button
                key={t.id}
                type="button"
                onClick={() => setActiveTraceId(t.id)}
                className={cn(
                  "flex w-full flex-col gap-1 px-3 py-2 text-left text-xs hover:bg-muted/30",
                  t.id === activeTraceId ? "bg-muted/40" : "",
                )}
              >
                <div className="flex items-center gap-2">
                  <span className="font-mono text-muted-foreground tabular-nums">
                    #{idx + 1}
                  </span>
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
                  {t.threat_detected ? (
                    <AlertTriangle className="h-3 w-3 text-destructive" />
                  ) : null}
                  <span className="ml-auto font-mono text-muted-foreground tabular-nums">
                    {formatDuration(t.duration_ms)}
                  </span>
                </div>
                <div className="truncate font-mono text-[11px] text-foreground/80">
                  {t.path}
                </div>
                <div className="flex justify-between text-[10px] text-muted-foreground">
                  <span>{t.model}</span>
                  <span>{new Date(t.started_at).toLocaleTimeString()}</span>
                </div>
              </button>
            ))}
          </div>
        </div>
      }
      right={
        activeTraceId ? (
          <EmbeddedTrace traceId={activeTraceId} />
        ) : (
          <div className="flex h-full items-center justify-center text-xs text-muted-foreground">
            Select a trace to inspect.
          </div>
        )
      }
    />
  );
}

function EmbeddedTrace({ traceId }: { traceId: string }) {
  const { data, isLoading } = useQuery({
    queryKey: ["trace", traceId],
    queryFn: () => api.traces.get(traceId),
  });
  const { data: threats = [] } = useQuery<TraceThreatDetection[]>({
    queryKey: ["trace-threats", traceId],
    queryFn: () => api.traces.threats(traceId),
  });

  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [tab, setTab] = useState<string>("input");

  useEffect(() => {
    setSelectedId(null);
    setTab("input");
  }, [traceId]);

  if (isLoading || !data?.trace) {
    return (
      <div className="flex h-full items-center justify-center text-xs text-muted-foreground">
        Loading trace…
      </div>
    );
  }

  const trace = data.trace as NonNullable<TraceDetail["trace"]>;
  const spans = (data.spans ?? []) as Observation[];
  const syntheticRoot: Observation = {
    id: traceId,
    trace_id: traceId,
    parent_id: "",
    type: "span",
    name: trace.path || trace.method || "request",
    depth: 0,
    started_at: trace.started_at ?? new Date().toISOString(),
    completed_at: trace.completed_at ?? new Date().toISOString(),
    duration_ms: trace.duration_ms ?? 0,
    input_tokens: trace.input_tokens,
    output_tokens: trace.output_tokens,
    model: trace.model,
    input: trace.request_body ?? "",
    output: trace.response_body ?? "",
    status: trace.status ?? "ok",
    error_message: trace.error_message,
  };
  const allSpans: Observation[] = spans.length
    ? [syntheticRoot, ...spans.map((s) => ({ ...s, parent_id: s.parent_id || traceId }))]
    : [syntheticRoot];
  const currentId = selectedId ?? traceId;
  const selected = allSpans.find((s) => s.id === currentId) ?? syntheticRoot;

  return (
    <ResizablePanels
      storageKey="session-trace-split"
      defaultLeftPct={42}
      left={
        <div className="h-full overflow-auto">
          <div className="border-b border-border/40 bg-muted/10 px-3 py-2 text-[10px] font-semibold uppercase tracking-wider text-muted-foreground/60">
            Spans ({allSpans.length})
          </div>
          <SpanTree
            spans={allSpans}
            traceStartMs={new Date(trace.started_at ?? "").getTime() || 0}
            traceDurationMs={trace.duration_ms ?? 1}
            selectedSpanId={currentId}
            onSelect={(s) => setSelectedId(s.id)}
            threats={threats}
          />
        </div>
      }
      right={
        <div className="h-full min-h-0">
          <SpanDetailTabs
            span={selected}
            threats={threats}
            activeTab={tab}
            onTabChange={setTab}
          />
        </div>
      }
    />
  );
}

function BackLink() {
  return (
    <Link
      to="/sessions"
      className="inline-flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground"
    >
      <ArrowLeft className="h-3.5 w-3.5" /> Back to sessions
    </Link>
  );
}

function LoadingBlock() {
  return (
    <div className="flex items-center justify-center py-16">
      <div className="flex items-center gap-2 text-sm text-muted-foreground">
        <div className="h-4 w-4 animate-spin rounded-full border-2 border-muted-foreground/30 border-t-muted-foreground" />
        Loading session…
      </div>
    </div>
  );
}
