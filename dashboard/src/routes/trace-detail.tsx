import { useEffect, useId, useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useParams } from "@tanstack/react-router";
import {
  AlertTriangle,
  ArrowLeft,
  ArrowRight,
  Ban,
  Check,
  CheckCircle2,
  ChevronDown,
  ChevronLeft,
  ChevronRight,
  Copy,
  ExternalLink,
  FileJson,
  Plus,
  ShieldAlert,
  ShieldCheck,
  ShieldPlus,
} from "lucide-react";

import { api } from "@/api/client";
import type {
  CreateTraceScoreRequest,
  Observation,
  Trace,
  TraceDetail,
  TraceScore,
} from "@/api/client";
import { Badge } from "@/components/ui/badge";
import { Button, buttonVariants } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { AdminPageHeader, AdminSummaryStrip } from "@/components/admin/admin-primitives";
import { ResizablePanels } from "@/components/observe/resizable-panels";
import { SpanDetailTabs } from "@/components/observe/span-detail-tabs";
import { SpanTree } from "@/components/observe/span-tree";
import { useTracesExtension } from "@/components/traces-extension";
import { cn, formatCost, formatDuration, weightedThreatScore } from "@/lib/utils";

export function TraceDetailPage() {
  const { id } = useParams({ from: "/traces/$id" });
  const ext = useTracesExtension();
  const [reviewFindingsRequest, setReviewFindingsRequest] = useState(0);

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
    <div className="flex h-[calc(100vh-6.5rem)] flex-col gap-3">
      <TraceHeader trace={trace} threatCount={threats.length} />
      <TraceOutcomeBanner
        trace={trace}
        threats={threats}
        onReviewFindings={() => setReviewFindingsRequest((request) => request + 1)}
      />
      <KpiStrip trace={trace} threats={threats} />
      <div className="min-h-0 flex-1 overflow-hidden rounded-xl border border-border/70 bg-card">
        <TraceSplit
          trace={trace}
          spans={spans}
          threats={threats}
          traceId={id}
          reviewFindingsRequest={reviewFindingsRequest}
        />
      </div>
      {reviewFindingsRequest ? (
        <div
          key={reviewFindingsRequest}
          role="status"
          aria-live="polite"
          className="pointer-events-none fixed bottom-5 left-1/2 z-50 flex -translate-x-1/2 items-center gap-2 rounded-md border border-border bg-popover px-3 py-2 text-[11px] font-medium text-popover-foreground shadow-sm animate-in fade-in-0 slide-in-from-bottom-2 duration-150"
        >
          <CheckCircle2 className="size-3.5 text-success" />
          Showing the first of {threats.length} findings
        </div>
      ) : null}
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
    <AdminPageHeader
      eyebrow="Trace investigation"
      title={trace.trace_name || trace.path || trace.method}
      description={trace.started_at ? `Started ${new Date(trace.started_at).toLocaleString()} · ${trace.provider ?? "unknown provider"} · ${trace.model ?? "unknown model"}` : undefined}
      badge={
        <div className="flex flex-wrap items-center gap-2">
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
            {threatCount} finding{threatCount === 1 ? "" : "s"}
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
      }
      actions={
        <div className="flex items-center gap-2">
          <BackLink session={trace.session_id} />
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
      }
      className="mb-0"
    />
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
  threats,
}: {
  trace: NonNullable<TraceDetail["trace"]>;
  threats: import("@/api/client").TraceThreatDetection[];
}) {
  const tokens = (trace.input_tokens ?? 0) + (trace.output_tokens ?? 0);
  const outcome = getTraceOutcome(trace, threats);
  return (
    <AdminSummaryStrip items={[
      { label: "Duration", value: formatDuration(trace.duration_ms ?? 0), detail: "End-to-end latency" },
      { label: "Tokens", value: tokens.toLocaleString(), detail: `${trace.input_tokens ?? 0} in · ${trace.output_tokens ?? 0} out` },
      { label: "Cost", value: formatCost(trace.cost_cents ?? 0), detail: trace.provider ?? "Unknown provider" },
      {
        label: "Gateway outcome",
        value: outcome.shortLabel,
        detail: trace.http_status ? `HTTP ${trace.http_status} · ${trace.security_action || "pass"}` : trace.security_action || "pass",
        tone: outcome.tone === "danger" ? "danger" : outcome.tone === "warning" ? "warning" : "success",
      },
    ]} />
  );
}

type OutcomeTone = "success" | "warning" | "danger";

function getTraceOutcome(
  trace: NonNullable<TraceDetail["trace"]>,
  threats: import("@/api/client").TraceThreatDetection[],
) {
  const blockedFindings = threats.filter((threat) => threat.action_taken === "block").length;
  const categories = [...new Set(threats.map((threat) => threat.threat_type).filter(Boolean))];

  if (trace.status === "blocked" || trace.http_status === 403) {
    return {
      label: "Request blocked",
      shortLabel: "Blocked",
      description: `${threats.length || 1} security finding${threats.length === 1 ? "" : "s"} prevented this request before the provider completed it.`,
      detail: categories.length ? categories.join(" · ") : "Security policy enforced",
      tone: "danger" as OutcomeTone,
      icon: Ban,
    };
  }

  if (trace.status !== "ok") {
    return {
      label: "Request failed",
      shortLabel: "Failed",
      description: trace.error_message || "The gateway did not complete this request successfully.",
      detail: trace.http_status ? `HTTP ${trace.http_status}` : trace.status,
      tone: "danger" as OutcomeTone,
      icon: AlertTriangle,
    };
  }

  if (threats.length) {
    const mismatch = blockedFindings > 0
      ? `${blockedFindings} finding${blockedFindings === 1 ? "" : "s"} requested a block, but the gateway outcome remained successful.`
      : "The request completed while the security event was recorded for review.";
    return {
      label: "Allowed with security findings",
      shortLabel: "Allowed · review",
      description: `${threats.length} ${categories.join(" / ") || "security"} finding${threats.length === 1 ? " was" : "s were"} recorded. ${mismatch}`,
      detail: trace.security_action ? `Effective strategy: ${trace.security_action}` : "Review detector strategy",
      tone: "warning" as OutcomeTone,
      icon: ShieldAlert,
    };
  }

  return {
    label: "Request allowed",
    shortLabel: "Allowed",
    description: "The gateway completed the request and no security detectors fired.",
    detail: "Security checks passed",
    tone: "success" as OutcomeTone,
    icon: CheckCircle2,
  };
}

function TraceOutcomeBanner({
  trace,
  threats,
  onReviewFindings,
}: {
  trace: NonNullable<TraceDetail["trace"]>;
  threats: import("@/api/client").TraceThreatDetection[];
  onReviewFindings: () => void;
}) {
  const outcome = getTraceOutcome(trace, threats);
  const Icon = outcome.icon;
  const [reviewFeedback, setReviewFeedback] = useState(0);

  useEffect(() => {
    if (!reviewFeedback) return;
    const timeout = window.setTimeout(() => setReviewFeedback(0), 2600);
    return () => window.clearTimeout(timeout);
  }, [reviewFeedback]);

  const reviewFindings = () => {
    setReviewFeedback((feedback) => feedback + 1);
    onReviewFindings();
  };

  return (
    <section
      aria-labelledby="trace-outcome-title"
      className={cn(
        "flex flex-wrap items-center gap-3 rounded-lg border px-3.5 py-3",
        outcome.tone === "danger" && "border-danger/25 bg-danger-bg/60",
        outcome.tone === "warning" && "border-warn-border bg-warn-bg/50",
        outcome.tone === "success" && "border-success-border bg-success-bg/60",
      )}
    >
      <span
        className={cn(
          "flex size-8 flex-shrink-0 items-center justify-center rounded-md border bg-background/70",
          outcome.tone === "danger" && "border-danger/25 text-danger",
          outcome.tone === "warning" && "border-warn-border text-warn",
          outcome.tone === "success" && "border-success-border text-success",
        )}
      >
        <Icon className="size-4" />
      </span>
      <div className="min-w-[18rem] flex-1">
        <div className="flex flex-wrap items-center gap-2">
          <h2 id="trace-outcome-title" className="text-[12px] font-semibold tracking-tight">
            {outcome.label}
          </h2>
          <Badge variant="outline" className="h-5 px-1.5 font-mono text-[9px]">
            {outcome.detail}
          </Badge>
        </div>
        <p className="mt-1 max-w-4xl text-[10px] leading-4 text-muted-foreground">
          {outcome.description}
        </p>
      </div>
      {threats.length ? (
        <Button
          type="button"
          variant="outline"
          size="sm"
          className="h-8 text-[10px]"
          onClick={reviewFindings}
        >
          {reviewFeedback ? (
            <>
              <Check className="size-3" /> Findings in view
            </>
          ) : (
            <>
              Review {threats.length} finding{threats.length === 1 ? "" : "s"}
              <ArrowRight className="size-3" />
            </>
          )}
        </Button>
      ) : null}
    </section>
  );
}

function TraceSplit({
  trace,
  spans,
  threats,
  traceId,
  reviewFindingsRequest,
}: {
  trace: NonNullable<TraceDetail["trace"]>;
  spans: Observation[];
  threats: import("@/api/client").TraceThreatDetection[];
  traceId: string;
  reviewFindingsRequest: number;
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
  const [tab, setTab] = useState<string>(threats.length ? "security" : "input");
  const [tabWasChosen, setTabWasChosen] = useState(false);
  const [selectedThreatId, setSelectedThreatId] = useState<string | null>(threats[0]?.id ?? null);
  const firstThreatId = threats[0]?.id;
  const selected = allSpans.find((s) => s.id === selectedId) ?? syntheticRoot;
  const selectedThreat = threats.find((threat) => threat.id === selectedThreatId) ?? threats[0] ?? null;

  useEffect(() => {
    // Reset selection when navigating between traces.
    setSelectedId(traceId);
    setTabWasChosen(false);
  }, [traceId]);

  useEffect(() => {
    if (!threats.length) {
      setSelectedThreatId(null);
      return;
    }
    setSelectedThreatId((current) =>
      current && threats.some((threat) => threat.id === current) ? current : threats[0]!.id,
    );
    if (!tabWasChosen) setTab("security");
  }, [tabWasChosen, threats]);

  useEffect(() => {
    if (!reviewFindingsRequest || !firstThreatId) return;

    setSelectedId(traceId);
    setSelectedThreatId(firstThreatId);
    setTab("security");
    setTabWasChosen(true);

    window.history.replaceState(window.history.state, "", "#trace-security");
    window.requestAnimationFrame(() => {
      const findings = document.getElementById("trace-security-findings");
      findings?.scrollIntoView({ behavior: "smooth", block: "nearest" });
      findings?.focus({ preventScroll: true });
    });
  }, [firstThreatId, reviewFindingsRequest, traceId]);

  const selectThreat = (threat: import("@/api/client").TraceThreatDetection) => {
    setSelectedThreatId(threat.id);
    setTab("security");
    setTabWasChosen(true);
  };

  return (
    <ResizablePanels
      defaultLeftPct={24}
      minLeftPx={250}
      minRightPx={680}
      left={
        <div className="flex h-full min-h-0 flex-col bg-surface-1/35">
          <div className="flex items-center justify-between border-b border-border-subtle px-3 py-2.5">
            <div>
              <h2 className="text-[10px] font-semibold uppercase tracking-wider text-muted-foreground">
                Spans
              </h2>
              <p className="mt-0.5 text-[9px] text-muted-foreground/70">
                {allSpans.length} recorded · {formatDuration(trace.duration_ms ?? 0)} total
              </p>
            </div>
            <Badge variant="outline" className="h-5 px-1.5 font-mono text-[9px]">
              {allSpans.length}
            </Badge>
          </div>
          <div className="min-h-0 flex-1 overflow-auto">
            <SpanTree
              spans={allSpans}
              traceStartMs={traceStartMs}
              traceDurationMs={trace.duration_ms ?? 1}
              selectedSpanId={selectedId}
              onSelect={(span) => setSelectedId(span.id)}
              threats={threats}
            />
          </div>
          <div className="flex-shrink-0 border-t border-border-subtle">
            <details className="group">
              <summary className="flex cursor-pointer list-none items-center justify-between px-3 py-2.5 text-[10px] text-muted-foreground hover:bg-surface-2 hover:text-foreground">
                <span className="flex items-center gap-2">
                  <Check className="size-3.5" /> Evaluation scores
                </span>
                <ChevronDown className="size-3.5 transition-transform group-open:rotate-180" />
              </summary>
              <div className="max-h-[21rem] overflow-auto border-t border-border-subtle bg-background p-3">
                <ScoresPanel traceId={traceId} />
              </div>
            </details>
          </div>
        </div>
      }
      right={
        <div className="flex h-full min-h-0 min-w-0">
          <main className="min-w-0 flex-1">
            <SpanDetailTabs
              span={selected}
              threats={threats}
              activeTab={tab}
              onTabChange={(nextTab) => {
                setTab(nextTab);
                setTabWasChosen(true);
              }}
              selectedThreatId={selectedThreatId}
              onThreatSelect={selectThreat}
              securityReviewSignal={reviewFindingsRequest}
            />
          </main>
          <TraceSecurityInspector
            trace={trace}
            threats={threats}
            selectedThreat={selectedThreat}
            onSelectThreat={selectThreat}
          />
        </div>
      }
    />
  );
}

function TraceSecurityInspector({
  trace,
  threats,
  selectedThreat,
  onSelectThreat,
}: {
  trace: NonNullable<TraceDetail["trace"]>;
  threats: import("@/api/client").TraceThreatDetection[];
  selectedThreat: import("@/api/client").TraceThreatDetection | null;
  onSelectThreat: (threat: import("@/api/client").TraceThreatDetection) => void;
}) {
  const selectedIndex = selectedThreat ? threats.findIndex((threat) => threat.id === selectedThreat.id) : -1;
  const weighted = selectedThreat
    ? weightedThreatScore(selectedThreat.score ?? 0, selectedThreat.confidence ?? 0, selectedThreat.weighted_score)
    : 0;

  const moveSelection = (offset: number) => {
    if (!threats.length) return;
    const nextIndex = Math.min(threats.length - 1, Math.max(0, selectedIndex + offset));
    const next = threats[nextIndex];
    if (next) onSelectThreat(next);
  };

  return (
    <aside
      id="trace-security"
      aria-label="Trace investigation details"
      className="hidden h-full w-[316px] flex-shrink-0 flex-col border-l border-border-subtle bg-background 2xl:flex"
    >
      <div className="flex items-start gap-2 border-b border-border-subtle px-3 py-3">
        <span className={cn("mt-1 flex size-6 items-center justify-center rounded-md border", selectedThreat ? "border-danger/20 bg-danger-bg text-danger" : "border-success-border bg-success-bg text-success")}>
          {selectedThreat ? <ShieldAlert className="size-3.5" /> : <ShieldCheck className="size-3.5" />}
        </span>
        <div className="min-w-0 flex-1">
          <h2 className="text-[11px] font-semibold tracking-tight">
            {selectedThreat ? selectedThreat.threat_type : "No security findings"}
          </h2>
          <p className="mt-0.5 truncate text-[9px] text-muted-foreground">
            {selectedThreat ? `Detected by ${selectedThreat.detector_name}` : "This trace passed all configured checks"}
          </p>
        </div>
        {selectedThreat ? (
          <div className="flex items-center gap-0.5">
            <Button variant="ghost" size="icon-xs" aria-label="Previous finding" disabled={selectedIndex <= 0} onClick={() => moveSelection(-1)}>
              <ChevronLeft className="size-3.5" />
            </Button>
            <span className="min-w-8 text-center font-mono text-[9px] text-muted-foreground">
              {selectedIndex + 1}/{threats.length}
            </span>
            <Button variant="ghost" size="icon-xs" aria-label="Next finding" disabled={selectedIndex >= threats.length - 1} onClick={() => moveSelection(1)}>
              <ChevronRight className="size-3.5" />
            </Button>
          </div>
        ) : null}
      </div>

      <div className="min-h-0 flex-1 space-y-3 overflow-y-auto p-3">
        {selectedThreat ? (
          <>
            <InvestigationCard title="Decision">
              <InvestigationRow label="Action">
                <Badge variant={selectedThreat.action_taken === "block" ? "destructive" : "warning"} className="h-4 px-1.5 text-[8px]">
                  {selectedThreat.action_taken || "record"}
                </Badge>
              </InvestigationRow>
              <InvestigationRow label="Weighted score" mono>{(weighted * 100).toFixed(0)}%</InvestigationRow>
              <InvestigationRow label="Severity" mono>{selectedThreat.severity}</InvestigationRow>
              <InvestigationRow label="Confidence" mono>{((selectedThreat.confidence ?? 0) * 100).toFixed(0)}%</InvestigationRow>
            </InvestigationCard>

            <InvestigationCard title="Matched evidence">
              <CopyValue value={selectedThreat.matched_pattern || "No pattern recorded"} />
              {selectedThreat.matched_content ? (
                <pre className="mt-2 max-h-28 overflow-auto whitespace-pre-wrap break-words rounded-md border border-border-subtle bg-background p-2 font-mono text-[9px] leading-4 text-foreground/85">
                  {selectedThreat.matched_content}
                </pre>
              ) : null}
            </InvestigationCard>

            <InvestigationCard title="Finding context">
              <InvestigationRow label="Detector" mono>{selectedThreat.detector_name}</InvestigationRow>
              <InvestigationRow label="Subtype" mono>{selectedThreat.threat_subtype || "—"}</InvestigationRow>
              <InvestigationRow label="Source" mono>{selectedThreat.source || "—"}</InvestigationRow>
              <InvestigationRow label="End user" mono>{selectedThreat.end_user_id || trace.end_user_id || "—"}</InvestigationRow>
              <InvestigationRow label="IP address" mono>{selectedThreat.ip_address || "—"}</InvestigationRow>
            </InvestigationCard>
          </>
        ) : null}

        <InvestigationCard title="Trace context">
          <InvestigationRow label="Environment" mono>{trace.environment || "—"}</InvestigationRow>
          <InvestigationRow label="Release" mono>{trace.release || "—"}</InvestigationRow>
          <InvestigationRow label="Session" mono>{trace.session_id || "—"}</InvestigationRow>
          <InvestigationRow label="End user" mono>{trace.end_user_id || "—"}</InvestigationRow>
          <InvestigationRow label="Route" mono>{trace.path || trace.method}</InvestigationRow>
          <InvestigationRow label="Provider" mono>{trace.provider} · {trace.model}</InvestigationRow>
          <CopyValue value={trace.id} label="Trace ID" />
        </InvestigationCard>
      </div>

      {selectedThreat ? (
        <div className="space-y-2 border-t border-border-subtle p-3">
          <Link
            to="/threats/$id"
            params={{ id: selectedThreat.id }}
            className={buttonVariants({ variant: "outline", size: "sm" }) + " w-full justify-between text-[10px]"}
          >
            <span className="flex items-center gap-1.5"><FileJson className="size-3" /> Open full threat</span>
            <ExternalLink className="size-3" />
          </Link>
          <Link
            to="/overlays/new"
            search={{ from_threat: selectedThreat.id, template: undefined }}
            className={buttonVariants({ size: "sm" }) + " w-full text-[10px]"}
          >
            <ShieldPlus className="size-3.5" /> Create policy from finding
          </Link>
          <Link
            to="/security-settings"
            className={buttonVariants({ variant: "outline", size: "sm" }) + " w-full text-[10px]"}
          >
            Review detector strategy
          </Link>
        </div>
      ) : null}
    </aside>
  );
}

function InvestigationCard({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <section className="rounded-md border border-border-subtle bg-surface-1 p-3">
      <h3 className="mb-2 text-[9px] font-medium uppercase tracking-wider text-muted-foreground">{title}</h3>
      <div className="space-y-1.5">{children}</div>
    </section>
  );
}

function InvestigationRow({ label, children, mono = false }: { label: string; children: React.ReactNode; mono?: boolean }) {
  return (
    <div className="flex min-w-0 items-start gap-3 text-[9px] leading-4">
      <span className="w-[76px] flex-shrink-0 text-muted-foreground">{label}</span>
      <span className={cn("min-w-0 flex-1 break-words text-right text-foreground/90", mono && "font-mono tabular-nums")}>{children}</span>
    </div>
  );
}

function CopyValue({ value, label }: { value: string; label?: string }) {
  const [copied, setCopied] = useState(false);
  return (
    <div className="flex items-center gap-2">
      {label ? <span className="w-[76px] flex-shrink-0 text-[9px] text-muted-foreground">{label}</span> : null}
      <code className="min-w-0 flex-1 truncate text-[9px] text-foreground/90">{value}</code>
      <button
        type="button"
        className="flex size-5 flex-shrink-0 items-center justify-center rounded text-muted-foreground hover:bg-surface-2 hover:text-foreground"
        aria-label={`Copy ${label || "matched value"}`}
        onClick={async () => {
          try {
            await navigator.clipboard.writeText(value);
            setCopied(true);
            window.setTimeout(() => setCopied(false), 1200);
          } catch {
            setCopied(false);
          }
        }}
      >
        {copied ? <Check className="size-3" /> : <Copy className="size-3" />}
      </button>
    </div>
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
      <KpiStrip trace={t} threats={[]} />
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
  const formId = useId();

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
        <label htmlFor={`${formId}-name`} className="sr-only">Score name</label>
        <Input
          id={`${formId}-name`}
          placeholder="Score Name (e.g. correctness)"
          value={name}
          onChange={(e) => setName(e.target.value)}
          className="h-7 text-xs"
          required
        />
        <Select value={valueType} onValueChange={(v) => setValueType(v as typeof valueType)}>
          <SelectTrigger className="h-7 text-xs" aria-label="Score value type">
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
          <>
            <label htmlFor={`${formId}-numeric`} className="sr-only">Numeric score value</label>
            <Input
              id={`${formId}-numeric`}
              type="number"
              step="0.01"
              placeholder="Score Value (e.g. 0.95)"
              value={numeric}
              onChange={(e) => setNumeric(e.target.value)}
              className="h-7 text-xs"
              required
            />
          </>
        ) : (
          <>
            <label htmlFor={`${formId}-categorical`} className="sr-only">Score value</label>
            <Input
              id={`${formId}-categorical`}
              placeholder={valueType === "boolean" ? "true / false" : "Value (e.g. pass)"}
              value={str}
              onChange={(e) => setStr(e.target.value)}
              className="h-7 text-xs"
              required
            />
          </>
        )}
        <label htmlFor={`${formId}-evaluator`} className="sr-only">Evaluator</label>
        <Input
          id={`${formId}-evaluator`}
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
