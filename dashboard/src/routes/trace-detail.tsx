import { useEffect, useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useParams } from "@tanstack/react-router";
import { ArrowLeft, Copy, Plus } from "lucide-react";

import { api } from "@/api/client";
import type {
  CreateTraceScoreRequest,
  Observation,
  TraceDetail,
  TraceScore,
} from "@/api/client";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { KpiCard } from "@/components/observe/kpi-card";
import { ResizablePanels } from "@/components/observe/resizable-panels";
import { SpanDetailTabs } from "@/components/observe/span-detail-tabs";
import { SpanTree } from "@/components/observe/span-tree";
import { formatCost, formatDuration } from "@/lib/utils";

export function TraceDetailPage() {
  const { id } = useParams({ from: "/traces/$id" });

  const { data, isLoading, error } = useQuery({
    queryKey: ["trace", id],
    queryFn: () => api.traces.get(id),
  });

  const { data: threats = [] } = useQuery({
    queryKey: ["trace-threats", id],
    queryFn: () => api.traces.threats(id),
    enabled: Boolean(id),
  });

  if (isLoading) {
    return <LoadingBlock label="Loading trace..." />;
  }
  if (error || !data?.trace) {
    return (
      <div className="space-y-4">
        <BackLink />
        <EmptyStateCard
          title="Trace not found"
          description="This id no longer exists, or belongs to another tenant."
        />
      </div>
    );
  }

  const trace = data.trace;
  const spans = (data.spans ?? []) as Observation[];

  return (
    <div className="flex h-[calc(100vh-4rem)] flex-col space-y-4">
      <TraceHeader trace={trace} threatCount={threats.length} />
      <KpiStrip trace={trace} threatCount={threats.length} />
      <div className="flex-1 min-h-0 overflow-hidden rounded border border-border/50">
        <TraceSplit trace={trace} spans={spans} threats={threats} traceId={id} />
      </div>
    </div>
  );
}

function TraceHeader({
  trace,
  threatCount,
}: {
  trace: NonNullable<TraceDetail["trace"]>;
  threatCount: number;
}) {
  return (
    <div className="flex items-center justify-between">
      <div className="flex flex-wrap items-center gap-3">
        <BackLink session={trace.session_id} />
        <span className="text-sm font-semibold">
          {trace.trace_name || trace.path || trace.method}
        </span>
        <Badge
          variant={
            trace.status === "ok"
              ? "success"
              : trace.status === "blocked"
              ? "destructive"
              : "warning"
          }
          className="text-[10px] px-1.5 py-0"
        >
          {trace.status}
        </Badge>
        {threatCount > 0 ? (
          <Badge variant="destructive" className="text-[10px] px-1.5 py-0">
            {threatCount} detector{threatCount === 1 ? "" : "s"}
          </Badge>
        ) : null}
        {trace.environment ? (
          <Badge variant="outline" className="text-[10px] px-1.5 py-0 text-muted-foreground">
            env: {trace.environment}
          </Badge>
        ) : null}
        {trace.release ? (
          <Badge variant="outline" className="text-[10px] px-1.5 py-0 text-muted-foreground">
            {trace.release}
          </Badge>
        ) : null}
        {trace.tags
          ? Object.entries(trace.tags).map(([k, v]) => (
              <Badge
                key={k}
                variant="outline"
                className="text-[10px] px-1.5 py-0 font-mono text-muted-foreground"
              >
                {k}: {v}
              </Badge>
            ))
          : null}
      </div>
      <div className="flex items-center gap-2">
        <span className="font-mono text-[11px] text-muted-foreground">{trace.id}</span>
        <Button
          variant="ghost"
          size="icon"
          className="h-6 w-6"
          onClick={() => navigator.clipboard?.writeText(trace.id ?? "")}
          aria-label="Copy trace id"
        >
          <Copy className="h-3 w-3" />
        </Button>
      </div>
    </div>
  );
}

function BackLink({ session }: { session?: string }) {
  if (session) {
    return (
      <Link
        to="/sessions/$id"
        params={{ id: session }}
        className="inline-flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground"
      >
        <ArrowLeft className="h-3.5 w-3.5" /> Back to session
      </Link>
    );
  }
  return (
    <Link
      to="/traces"
      className="inline-flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground"
    >
      <ArrowLeft className="h-3.5 w-3.5" /> Back to traces
    </Link>
  );
}

function KpiStrip({
  trace,
  threatCount,
}: {
  trace: NonNullable<TraceDetail["trace"]>;
  threatCount: number;
}) {
  const tokens = (trace.input_tokens ?? 0) + (trace.output_tokens ?? 0);
  return (
    <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-6">
      <KpiCard label="Duration" value={formatDuration(trace.duration_ms ?? 0)} />
      <KpiCard
        label="Tokens"
        value={tokens.toLocaleString()}
        sub={`${trace.input_tokens ?? 0} → ${trace.output_tokens ?? 0}`}
      />
      <KpiCard label="Cost" value={formatCost(trace.cost_cents ?? 0)} />
      <KpiCard
        label="Security"
        value={threatCount === 0 ? "Clean" : `${threatCount} hit`}
        tone={threatCount === 0 ? "success" : "danger"}
        sub={trace.security_action ?? "pass"}
      />
      <KpiCard
        label="Provider"
        value={trace.provider ?? "—"}
        sub={trace.model ?? ""}
      />
      <KpiCard
        label="End user"
        value={trace.end_user_id || "—"}
        sub={
          trace.started_at
            ? new Date(trace.started_at).toLocaleString()
            : ""
        }
      />
    </div>
  );
}

function TraceSplit({
  trace,
  spans,
  threats,
  traceId,
}: {
  trace: NonNullable<TraceDetail["trace"]>;
  spans: Observation[];
  threats: import("@/api/client").TraceThreatDetection[];
  traceId: string;
}) {
  const syntheticRoot = useMemo<Observation>(
    () => ({
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
    }),
    [trace, traceId],
  );
  const traceStartMs = useMemo(
    () => new Date(trace.started_at ?? "").getTime() || 0,
    [trace.started_at],
  );
  const allSpans = useMemo<Observation[]>(() => {
    if (!spans.length) return [syntheticRoot];
    return [syntheticRoot, ...spans.map((s) => ({ ...s, parent_id: s.parent_id || traceId }))];
  }, [spans, syntheticRoot, traceId]);

  const [selectedId, setSelectedId] = useState<string>(traceId);
  const [tab, setTab] = useState<string>("input");
  const selected = allSpans.find((s) => s.id === selectedId) ?? syntheticRoot;

  useEffect(() => {
    // Reset selection when navigating between traces.
    setSelectedId(traceId);
  }, [traceId]);

  return (
    <ResizablePanels
      storageKey="trace-detail-split"
      defaultLeftPct={42}
      left={
        <div className="h-full overflow-auto">
          <div className="border-b border-border/40 bg-muted/10 px-3 py-2 text-[10px] font-semibold uppercase tracking-wider text-muted-foreground/60">
            Spans ({allSpans.length})
          </div>
          <SpanTree
            spans={allSpans}
            traceStartMs={traceStartMs}
            traceDurationMs={trace.duration_ms ?? 1}
            selectedSpanId={selectedId}
            onSelect={(s) => setSelectedId(s.id)}
            threats={threats}
          />
          <div className="border-t border-border/40 px-3 py-3">
            <ScoresPanel traceId={traceId} />
          </div>
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

function LoadingBlock({ label }: { label: string }) {
  return (
    <div className="flex items-center justify-center py-16">
      <div className="flex items-center gap-2 text-sm text-muted-foreground">
        <div className="h-4 w-4 animate-spin rounded-full border-2 border-muted-foreground/30 border-t-muted-foreground" />
        {label}
      </div>
    </div>
  );
}

function EmptyStateCard({ title, description }: { title: string; description: string }) {
  return (
    <Card className="border-border/50">
      <CardContent className="py-12 text-center">
        <p className="text-sm font-medium">{title}</p>
        <p className="mt-1 text-xs text-muted-foreground">{description}</p>
      </CardContent>
    </Card>
  );
}

function ScoresPanel({ traceId }: { traceId: string }) {
  const qc = useQueryClient();
  const { data: scores = [] } = useQuery({
    queryKey: ["trace-scores", traceId],
    queryFn: () => api.traces.scores(traceId),
  });

  const create = useMutation({
    mutationFn: (req: CreateTraceScoreRequest) => api.traces.createScore(traceId, req),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["trace-scores", traceId] });
    },
  });

  return (
    <div className="space-y-2">
      <p className="text-[10px] font-semibold uppercase tracking-wider text-muted-foreground/60">
        Scores ({scores.length})
      </p>
      {scores.length ? (
        <div className="divide-y divide-border/30">
          {scores.map((s: TraceScore) => (
            <div
              key={s.id}
              className="grid grid-cols-[9rem_1fr_7rem] gap-2 py-1 text-xs"
            >
              <span className="truncate font-mono">{s.name}</span>
              <span className="truncate text-muted-foreground">
                {s.value_type === "numeric"
                  ? s.numeric_value?.toFixed(3)
                  : s.string_value}
                {s.comment ? ` — ${s.comment}` : ""}
              </span>
              <span className="truncate text-right text-[11px] text-muted-foreground">
                {s.evaluator || "—"}
              </span>
            </div>
          ))}
        </div>
      ) : (
        <p className="text-xs text-muted-foreground">No scores yet.</p>
      )}
      <AddScoreForm onSubmit={(req) => create.mutate(req)} isPending={create.isPending} />
    </div>
  );
}

function AddScoreForm({
  onSubmit,
  isPending,
}: {
  onSubmit: (req: CreateTraceScoreRequest) => void;
  isPending: boolean;
}) {
  const [name, setName] = useState("");
  const [valueType, setValueType] = useState<"numeric" | "categorical" | "boolean">("numeric");
  const [numeric, setNumeric] = useState("");
  const [str, setStr] = useState("");
  const [comment, setComment] = useState("");
  const [evaluator, setEvaluator] = useState("human");

  const submit = () => {
    if (!name) return;
    const req: CreateTraceScoreRequest = {
      name,
      value_type: valueType,
      comment: comment || undefined,
      evaluator: evaluator || undefined,
    };
    if (valueType === "numeric") {
      const n = parseFloat(numeric);
      if (Number.isNaN(n)) return;
      req.numeric_value = n;
    } else {
      if (!str) return;
      req.string_value = str;
    }
    onSubmit(req);
    setName("");
    setNumeric("");
    setStr("");
    setComment("");
  };

  return (
    <div className="grid grid-cols-[1fr_8rem_1fr_1fr_auto] gap-2 pt-2">
      <Input
        placeholder="name"
        value={name}
        onChange={(e) => setName(e.target.value)}
        className="h-7 text-xs"
      />
      <Select value={valueType} onValueChange={(v) => setValueType(v as typeof valueType)}>
        <SelectTrigger className="h-7 text-xs">
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value="numeric">numeric</SelectItem>
          <SelectItem value="categorical">categorical</SelectItem>
          <SelectItem value="boolean">boolean</SelectItem>
        </SelectContent>
      </Select>
      {valueType === "numeric" ? (
        <Input
          placeholder="value"
          value={numeric}
          onChange={(e) => setNumeric(e.target.value)}
          className="h-7 text-xs"
        />
      ) : (
        <Input
          placeholder={valueType === "boolean" ? "true/false" : "value"}
          value={str}
          onChange={(e) => setStr(e.target.value)}
          className="h-7 text-xs"
        />
      )}
      <Input
        placeholder="evaluator"
        value={evaluator}
        onChange={(e) => setEvaluator(e.target.value)}
        className="h-7 text-xs"
      />
      <Button onClick={submit} disabled={!name || isPending} size="sm" className="h-7 text-xs">
        <Plus className="h-3 w-3" /> Add
      </Button>
    </div>
  );
}
