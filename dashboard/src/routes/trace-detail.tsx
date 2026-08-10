import { useEffect, useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useParams } from "@tanstack/react-router";
import { ArrowLeft, Copy, Plus } from "lucide-react";

import { api } from "@/api/client";
import type {
  CreateTraceScoreRequest,
  Observation,
  Trace,
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
import { useTracesExtension } from "@/components/traces-extension";
import { formatCost, formatDuration } from "@/lib/utils";

export function TraceDetailPage() {
  const { id } = useParams({ from: "/traces/$id" });
  const ext = useTracesExtension();

  const { data, isLoading, error } = useQuery({
    queryKey: ["trace", id],
    queryFn: () => api.traces.get(id),
    retry: false,
  });

  // Fallback path: when api.traces.get(id) fails (typically 404 because the
  // row lives in an extension's data store, e.g. governance_events), ask the
  // injected TracesExtension whether it owns this id. Cloud-dashboard wires
  // up fetchExtraDetail; OSS standalone leaves it undefined and we fall
  // through to the original "Trace not found" empty state.
  const primaryFailed = Boolean(error) || (!isLoading && !data?.trace);
  const { data: extTrace, isLoading: extLoading } = useQuery({
    queryKey: ["trace-extension-detail", id],
    queryFn: () => ext.fetchExtraDetail!(id),
    enabled: primaryFailed && Boolean(ext.fetchExtraDetail),
    retry: false,
  });

  const { data: threats = [] } = useQuery({
    queryKey: ["trace-threats", id],
    queryFn: () => api.traces.threats(id),
    enabled: Boolean(id) && Boolean(data?.trace),
  });

  if (isLoading || (primaryFailed && extLoading)) {
    return <LoadingBlock label="Loading trace..." />;
  }
  if (primaryFailed) {
    if (extTrace && ext.renderDetail) {
      return <>{ext.renderDetail(extTrace)}</>;
    }
    if (extTrace) {
      return <ExtensionDefaultDetail trace={extTrace} />;
    }
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

  const trace = data!.trace!;
  const spans = (data!.spans ?? []) as Observation[];

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
        value={
          threatCount > 0
            ? `${threatCount} hit${threatCount > 1 ? "s" : ""}`
            : trace.status === "blocked" || trace.threat_detected
            ? "Blocked"
            : "Clean"
        }
        tone={
          threatCount > 0 || trace.status === "blocked" || trace.threat_detected
            ? "danger"
            : "success"
        }
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

// ExtensionDefaultDetail is the OSS fallback shown when a TracesExtension
// returned a row from fetchExtraDetail but did not supply renderDetail.
// Cloud-dashboard ships its own renderDetail with the rich governance UI;
// this default just makes sure /traces/:id never 404s for an id the
// extension claims as its own.
function ExtensionDefaultDetail({ trace }: { trace: Trace }) {
  // TraceHeader/KpiStrip type their `trace` prop as TraceDetail["trace"];
  // structurally Trace carries every field they actually read. Cast through
  // unknown to keep tsc honest without widening the shared helpers.
  const t = trace as unknown as NonNullable<TraceDetail["trace"]>;
  return (
    <div className="space-y-4">
      <TraceHeader trace={t} threatCount={0} />
      <KpiStrip trace={t} threatCount={0} />
      <Card className="border-border/50">
        <CardContent className="py-6 text-sm text-muted-foreground">
          This event came from an integration that doesn’t expose span-level detail.
          Provider: <span className="font-mono">{trace.provider ?? "—"}</span>.
        </CardContent>
      </Card>
    </div>
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
    <div className="space-y-3">
      <div>
        <div className="flex items-center gap-1.5">
          <span className="text-xs font-semibold text-foreground">Evaluation Scores</span>
          <Badge variant="outline" className="text-[10px] px-1.5 py-0 font-mono">
            {scores.length}
          </Badge>
        </div>
        <p className="text-[11px] text-muted-foreground mt-0.5 leading-relaxed">
          Record human feedback or LLM-as-a-judge quality metrics (e.g. correctness, toxicity) for dataset evaluation.
        </p>
      </div>

      {scores.length ? (
        <div className="space-y-1.5">
          {scores.map((s: TraceScore) => (
            <div
              key={s.id}
              className="flex items-center justify-between p-2 rounded-lg bg-muted/30 border border-border/40 text-xs"
            >
              <div className="flex items-center gap-2 min-w-0">
                <span className="font-mono font-medium text-foreground truncate">{s.name}</span>
                <Badge variant="secondary" className="text-[10px] px-1.5 py-0 font-mono">
                  {s.value_type === "numeric" ? s.numeric_value?.toFixed(3) : s.string_value}
                </Badge>
                {s.comment && (
                  <span className="text-[11px] text-muted-foreground truncate max-w-[120px]">
                    — {s.comment}
                  </span>
                )}
              </div>
              <span className="text-[10px] font-mono text-muted-foreground/70 shrink-0 ml-2">
                {s.evaluator || "human"}
              </span>
            </div>
          ))}
        </div>
      ) : (
        <p className="text-[11px] text-muted-foreground/70 italic">No evaluation scores recorded yet.</p>
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
  const [evaluator, setEvaluator] = useState("human");

  const submit = (e: React.FormEvent) => {
    e.preventDefault();
    if (!name) return;
    const req: CreateTraceScoreRequest = {
      name: name.trim(),
      value_type: valueType,
      evaluator: evaluator.trim() || "human",
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
  };

  return (
    <form onSubmit={submit} className="p-2.5 rounded-lg border border-border/40 bg-muted/10 space-y-2.5">
      <p className="text-[10px] font-medium uppercase tracking-wider text-muted-foreground">Add New Score</p>
      <div className="grid grid-cols-2 gap-2">
        <Input
          placeholder="Score Name (e.g. correctness)"
          value={name}
          onChange={(e) => setName(e.target.value)}
          className="h-7 text-xs"
          required
        />
        <Select value={valueType} onValueChange={(v) => setValueType(v as typeof valueType)}>
          <SelectTrigger className="h-7 text-xs">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="numeric">Numeric (0.0 - 1.0)</SelectItem>
            <SelectItem value="categorical">Categorical</SelectItem>
            <SelectItem value="boolean">Boolean (pass/fail)</SelectItem>
          </SelectContent>
        </Select>
      </div>

      <div className="grid grid-cols-2 gap-2">
        {valueType === "numeric" ? (
          <Input
            type="number"
            step="0.01"
            placeholder="Score Value (e.g. 0.95)"
            value={numeric}
            onChange={(e) => setNumeric(e.target.value)}
            className="h-7 text-xs"
            required
          />
        ) : (
          <Input
            placeholder={valueType === "boolean" ? "true / false" : "Value (e.g. pass)"}
            value={str}
            onChange={(e) => setStr(e.target.value)}
            className="h-7 text-xs"
            required
          />
        )}
        <Input
          placeholder="Evaluator (e.g. human, gpt-4o)"
          value={evaluator}
          onChange={(e) => setEvaluator(e.target.value)}
          className="h-7 text-xs"
        />
      </div>

      <div className="flex justify-end pt-1">
        <Button type="submit" disabled={!name || isPending} size="sm" className="h-7 text-xs gap-1">
          <Plus className="h-3 w-3" /> Add Score
        </Button>
      </div>
    </form>
  );
}
