import type { McpTracePresentation } from "@/components/observe/mcp-trace-presentation";
import {
  initialTraceTab,
  isWorkspaceMcpTrace,
  mcpCallDetails,
  mcpRetentionSummary,
  mcpSecuritySummary,
} from "@/components/observe/mcp-trace-presentation";
import { onlyPassiveFindings } from "@/components/observe/security-decision";
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
  Info,
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
  TraceThreatDetection,
} from "@/api/client";
import { Button, buttonVariants } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { AdminPageHeader, AdminSummaryStrip } from "@/components/admin/admin-primitives";
import { FindingExplanationCard } from "@/components/observe/finding-explanation-card";
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
    // The recorder batches writes; a link opened immediately can precede ingestion.
    retry: (failures, err) => failures < 4 && err.message.includes("trace not found"),
    retryDelay: 1000,
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

  const {
    data: threats = [],
    isPending: findingsLoading,
    isError: findingsError,
    refetch: retryFindings,
  } = useQuery({
    queryKey: ["trace-threats", id],
    queryFn: () => api.traces.threats(id),
    enabled: Boolean(id) && Boolean(data?.trace),
  });

  if (isLoading || (primaryFailed && extLoading)) {
    return <LoadingBlock label="Loading trace…" />;
  }
  if (primaryFailed) {
    if (extTrace && ext.renderDetail) {
      return <>{ext.renderDetail(extTrace)}</>;
    }
    if (extTrace) {
      return <ExtensionDefaultDetail trace={extTrace} />;
    }
    return (
      <div className="mx-auto max-w-[1180px] space-y-4 pb-10 pt-2 md:pt-4">
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
  const mcp: McpTracePresentation | undefined = isWorkspaceMcpTrace(trace)
    ? {
        details: mcpCallDetails(trace),
        retention: mcpRetentionSummary(trace),
        security: mcpSecuritySummary(
          trace,
          spans,
          threats,
          findingsError ? "error" : findingsLoading ? "loading" : "ready",
        ),
      }
    : undefined;

  return (
    <div className="flex h-[calc(100vh-6.5rem)] flex-col gap-3">
      <TraceHeader trace={trace} threatCount={threats.length} />
      <TraceOutcomeBanner
        trace={trace}
        mcp={mcp}
        threats={threats}
        onReviewFindings={() => setReviewFindingsRequest((request) => request + 1)}
      />
      <KpiStrip trace={trace} mcp={mcp} threats={threats} />
      {findingsError ? (
        <div
          role="alert"
          className="flex shrink-0 flex-wrap items-center justify-between gap-2 border-y border-border-subtle border-l-2 border-l-warn bg-surface-1/40 px-4 py-2.5"
        >
          <p className="text-xs text-muted-foreground">
            Security findings could not be loaded. The tool outcome and recorded timings are still available.
          </p>
          <Button
            type="button"
            variant="outline"
            size="sm"
            className="h-7 cursor-pointer text-xs"
            onClick={() => void retryFindings()}
          >
            Retry security details
          </Button>
        </div>
      ) : null}
      <div className="min-h-0 flex-1 overflow-hidden border border-border-subtle bg-background">
        <TraceSplit
          mcp={mcp}
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
          className="pointer-events-none fixed bottom-5 left-1/2 z-50 flex -translate-x-1/2 items-center gap-2 border border-border-default bg-popover px-3 py-2 text-[11px] font-medium text-popover-foreground shadow-sm animate-in fade-in-0 slide-in-from-bottom-2 duration-150"
        >
          <CheckCircle2 aria-hidden="true" className="size-3.5 text-success" />
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
      description={
        trace.started_at
          ? `Started ${new Date(trace.started_at).toLocaleString()} · ${trace.provider ?? "unknown provider"} · ${trace.model ?? "unknown model"}`
          : undefined
      }
      bordered={false}
      className="mb-0"
      badge={
        <div className="flex flex-wrap items-center gap-2.5 text-[11px] text-muted-foreground">
          <span className="inline-flex items-center gap-1.5 font-mono">
            <span
              aria-hidden="true"
              className={cn(
                "size-1.5 rounded-full",
                trace.status === "ok"
                  ? "bg-success"
                  : trace.status === "blocked"
                    ? "bg-destructive"
                    : "bg-warn",
              )}
            />
            {trace.status}
          </span>
          {threatCount > 0 ? (
            <span className="inline-flex items-center gap-1.5">
              <span
                aria-hidden="true"
                className={cn(
                  "size-1.5 rounded-full",
                  trace.status === "blocked" ? "bg-destructive" : "bg-warn",
                )}
              />
              <span>
                {threatCount} security signal{threatCount === 1 ? "" : "s"}
              </span>
            </span>
          ) : null}
          {trace.environment ? (
            <span className="border border-border-subtle bg-surface-1/50 px-1.5 py-0.5 font-mono text-[11px] text-muted-foreground">
              env:{trace.environment}
            </span>
          ) : null}
          {trace.release ? (
            <span className="border border-border-subtle bg-surface-1/50 px-1.5 py-0.5 font-mono text-[11px] text-muted-foreground">
              {trace.release}
            </span>
          ) : null}
          {trace.tags
            ? Object.entries(trace.tags).map(([k, v]) => (
                <span
                  key={k}
                  className="border border-border-subtle bg-surface-1/50 px-1.5 py-0.5 font-mono text-[11px] text-muted-foreground"
                >
                  {k}:{v}
                </span>
              ))
            : null}
        </div>
      }
      actions={
        <div className="flex items-center gap-2">
          <BackLink session={trace.session_id} />
          <span className="font-mono text-[11px] text-muted-foreground">{trace.id}</span>
          <Button
            type="button"
            variant="ghost"
            size="icon"
            className="h-6 w-6"
            onClick={() => navigator.clipboard?.writeText(trace.id ?? "")}
            aria-label="Copy trace id"
          >
            <Copy aria-hidden="true" className="h-3 w-3" />
          </Button>
        </div>
      }
    />
  );
}

function BackLink({ session }: { session?: string }) {
  if (session) {
    return (
      <Link
        to="/sessions/$id"
        params={{ id: session }}
        className="inline-flex items-center gap-1.5 text-xs text-muted-foreground hover:text-foreground"
      >
        <ArrowLeft aria-hidden="true" className="h-3.5 w-3.5" /> Back to session
      </Link>
    );
  }
  return (
    <Link
      to="/traces"
      className="inline-flex items-center gap-1.5 text-xs text-muted-foreground hover:text-foreground"
    >
      <ArrowLeft aria-hidden="true" className="h-3.5 w-3.5" /> Back to traces
    </Link>
  );
}

function KpiStrip({
  trace,
  mcp,
  threats,
}: {
  trace: NonNullable<TraceDetail["trace"]>;
  mcp?: McpTracePresentation;
  threats: TraceThreatDetection[];
}) {
  const tokens = (trace.input_tokens ?? 0) + (trace.output_tokens ?? 0);
  const outcome = getTraceOutcome(trace, threats, mcp);
  return (
    <div className="shrink-0 [&>section]:mb-0">
      <AdminSummaryStrip
        items={[
          {
            label: "Duration",
            value: formatDuration(trace.duration_ms ?? 0),
            detail: "End-to-end latency",
          },
          isWorkspaceMcpTrace(trace)
            ? {
                label: "Tool",
                value: mcpCallDetails(trace).tool,
                detail: "MCP tool call",
              }
            : {
                label: "Tokens",
                value: tokens.toLocaleString(),
                detail: `${trace.input_tokens ?? 0} in · ${trace.output_tokens ?? 0} out`,
              },
          isWorkspaceMcpTrace(trace)
            ? {
                label: "Content retention",
                value: mcp?.retention?.label || "Metadata only",
                detail: "Policy applied to this call",
              }
            : {
                label: "Cost",
                value: formatCost(trace.cost_cents ?? 0),
                detail: trace.provider ?? "Unknown provider",
              },
          {
            label: "Gateway outcome",
            value: outcome.shortLabel,
            detail: trace.http_status
              ? `HTTP ${trace.http_status} · ${trace.security_action || "pass"}`
              : trace.security_action || "pass",
            tone:
              outcome.tone === "danger"
                ? "danger"
                : outcome.tone === "warning"
                  ? "warning"
                  : "success",
          },
        ]}
      />
    </div>
  );
}

type OutcomeTone = "success" | "warning" | "danger";

function getTraceOutcome(
  trace: NonNullable<TraceDetail["trace"]>,
  threats: TraceThreatDetection[],
  mcp?: McpTracePresentation,
) {
  const actionable = threats.length > 0 && !onlyPassiveFindings(threats);
  if (trace.provider === "mcp") {
    const remote = trace.tags?.remote_action;
    const detail =
      remote === "completed"
        ? "Remote action completed"
        : remote === "unknown"
          ? "Remote outcome unknown"
          : "Tool did not run";
    if (trace.status === "blocked") {
      return {
        label: remote === "completed" ? "Tool ran · result withheld" : "Tool request withheld",
        shortLabel: "Withheld",
        description: trace.error_message || "Workspace security policy withheld this content.",
        detail,
        tone: "danger" as OutcomeTone,
        icon: Ban,
      };
    }
    if (trace.status !== "ok") {
      return {
        label: "Tool request failed",
        shortLabel: "Failed",
        description: trace.error_message || "The MCP tool request did not complete successfully.",
        detail,
        tone: "danger" as OutcomeTone,
        icon: AlertTriangle,
      };
    }
    const inspections = mcp?.security;
    const inspectionDetailsMissing = Boolean(
      inspections && inspections.tone !== "success" && !threats.length,
    );
    return {
      label: actionable ? "Tool completed with security records" : "Tool completed",
      shortLabel: "Completed",
      description: mcp
        ? mcp.retention?.description ||
          "This trace records tool timing, outcomes and security decisions. Payload history follows the policy applied to this call."
        : "This trace records the tool call outcome and available security decisions.",
      detail: actionable
        ? `Effective strategy: ${trace.security_action}`
        : threats.length
          ? "Signals observed · no enforcement"
          : inspections?.tone === "success"
            ? "Security checks completed"
            : inspections?.title || "No security findings recorded",
      tone: (actionable || inspectionDetailsMissing ? "warning" : "success") as OutcomeTone,
      icon: actionable ? ShieldAlert : inspectionDetailsMissing ? Info : CheckCircle2,
    };
  }

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

  if (threats.length && !actionable) {
    return {
      label: "Request allowed · signals observed",
      shortLabel: "Allowed",
      description:
        "Detector signals were recorded without a policy action. These signals alone do not confirm an attack.",
      detail: "No enforcement",
      tone: "success" as OutcomeTone,
      icon: CheckCircle2,
    };
  }

  if (threats.length) {
    const mismatch =
      blockedFindings > 0
        ? `${blockedFindings} finding${blockedFindings === 1 ? "" : "s"} requested a block, but the gateway outcome remained successful.`
        : "The request completed while the security event was recorded for review.";
    return {
      label: "Allowed with security findings",
      shortLabel: "Allowed · review",
      description: `${threats.length} ${categories.join(" / ") || "security"} finding${threats.length === 1 ? " was" : "s were"} recorded. ${mismatch}`,
      detail: trace.security_action
        ? `Effective strategy: ${trace.security_action}`
        : "Review detector strategy",
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
  mcp,
  threats,
  onReviewFindings,
}: {
  trace: NonNullable<TraceDetail["trace"]>;
  threats: TraceThreatDetection[];
  mcp?: McpTracePresentation;
  onReviewFindings: () => void;
}) {
  const outcome = getTraceOutcome(trace, threats, mcp);
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
        "flex shrink-0 flex-wrap items-center gap-3 border-y border-border-subtle border-l-2 bg-surface-1/40 px-4 py-2.5",
        outcome.tone === "danger" && "border-l-destructive",
        outcome.tone === "warning" && "border-l-warn",
        outcome.tone === "success" && "border-l-success",
      )}
    >
      <Icon
        aria-hidden="true"
        className={cn(
          "size-4 shrink-0",
          outcome.tone === "danger" && "text-destructive",
          outcome.tone === "warning" && "text-warn",
          outcome.tone === "success" && "text-success",
        )}
      />
      <div className="min-w-[18rem] flex-1">
        <div className="flex flex-wrap items-center gap-2">
          <h2 id="trace-outcome-title" className="text-xs font-medium tracking-tight text-foreground">
            {outcome.label}
          </h2>
          <span className="border border-border-subtle bg-background px-1.5 py-0.5 font-mono text-[10px] text-muted-foreground">
            {outcome.detail}
          </span>
        </div>
        <p className="mt-0.5 max-w-4xl text-xs leading-relaxed text-muted-foreground">
          {outcome.description}
        </p>
      </div>
      {threats.length ? (
        <Button
          type="button"
          variant="outline"
          size="sm"
          className="h-7 text-xs"
          onClick={reviewFindings}
        >
          {reviewFeedback ? (
            <>
              <Check aria-hidden="true" className="size-3" /> Findings in view
            </>
          ) : (
            <>
              Review {threats.length} finding{threats.length === 1 ? "" : "s"}
              <ArrowRight aria-hidden="true" className="size-3" />
            </>
          )}
        </Button>
      ) : null}
    </section>
  );
}

function TraceSplit({
  mcp,
  trace,
  spans,
  threats,
  traceId,
  reviewFindingsRequest,
}: {
  trace: NonNullable<TraceDetail["trace"]>;
  spans: Observation[];
  threats: TraceThreatDetection[];
  traceId: string;
  reviewFindingsRequest: number;
  mcp?: McpTracePresentation;
}) {
  const syntheticRoot = useMemo<Observation>(
    () => ({
      id: traceId,
      trace_id: traceId,
      parent_id: "",
      type: "span",
      name: isWorkspaceMcpTrace(trace)
        ? `MCP tool call · ${mcpCallDetails(trace).tool}`
        : trace.path || trace.method || "request",
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
      status_message: trace.error_message,
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
  const [tab, setTab] = useState<string>(() => initialTraceTab(trace, threats.length));
  const [tabWasChosen, setTabWasChosen] = useState(false);
  const [selectedThreatId, setSelectedThreatId] = useState<string | null>(threats[0]?.id ?? null);
  const firstThreatId = threats[0]?.id;
  const selected = allSpans.find((s) => s.id === selectedId) ?? syntheticRoot;
  const selectedThreat =
    threats.find((threat) => threat.id === selectedThreatId) ?? threats[0] ?? null;

  useEffect(() => {
    // Reset selection when navigating between traces.
    setSelectedId(traceId);
    setTabWasChosen(false);
  }, [traceId]);

  useEffect(() => {
    if (!tabWasChosen) setTab(initialTraceTab(trace, threats.length));
  }, [trace, threats.length, tabWasChosen]);

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

  const selectThreat = (threat: TraceThreatDetection) => {
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
        <div className="flex h-full min-h-0 flex-col bg-background">
          <div className="flex items-center justify-between border-b border-border-subtle px-3.5 py-2.5">
            <div>
              <h2 className="text-xs font-medium text-foreground">Spans</h2>
              <p className="mt-0.5 text-[11px] text-muted-foreground">
                {allSpans.length} recorded · {formatDuration(trace.duration_ms ?? 0)} total
              </p>
            </div>
            <span className="font-mono text-[11px] tabular-nums text-muted-foreground">
              {allSpans.length}
            </span>
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
          <div className="shrink-0 border-t border-border-subtle">
            <details className="group">
              <summary className="flex cursor-pointer list-none items-center justify-between px-3.5 py-2.5 text-xs text-muted-foreground transition-colors hover:bg-surface-1/60 hover:text-foreground">
                <span className="flex items-center gap-2">
                  <Check aria-hidden="true" className="size-3.5" /> Evaluation scores
                </span>
                <ChevronDown
                  aria-hidden="true"
                  className="size-3.5 transition-transform group-open:rotate-180"
                />
              </summary>
              <div className="max-h-[21rem] overflow-auto border-t border-border-subtle bg-background p-3.5">
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
              mcp={mcp}
              threats={threats}
              activeTab={tab}
              onTabChange={(nextTab) => {
                setTab(nextTab);
                setTabWasChosen(true);
              }}
              selectedThreatId={selectedThreatId}
              traceContext={trace}
              onThreatSelect={selectThreat}
              securityReviewSignal={reviewFindingsRequest}
            />
          </main>
          <TraceSecurityInspector
            trace={trace}
            mcp={mcp}
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
  mcp,
  threats,
  selectedThreat,
  onSelectThreat,
}: {
  trace: NonNullable<TraceDetail["trace"]>;
  threats: TraceThreatDetection[];
  mcp?: McpTracePresentation;
  selectedThreat: TraceThreatDetection | null;
  onSelectThreat: (threat: TraceThreatDetection) => void;
}) {
  const noFindingTone = mcp?.security.tone || "success";
  const selectedIndex = selectedThreat
    ? threats.findIndex((threat) => threat.id === selectedThreat.id)
    : -1;
  const weighted = selectedThreat
    ? weightedThreatScore(
        selectedThreat.score ?? 0,
        selectedThreat.confidence ?? 0,
        selectedThreat.weighted_score,
      )
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
      className="hidden h-full w-[316px] shrink-0 flex-col border-l border-border-subtle bg-background 2xl:flex"
    >
      <div className="flex items-start gap-2.5 border-b border-border-subtle px-3.5 py-3">
        {selectedThreat || noFindingTone === "danger" ? (
          <ShieldAlert aria-hidden="true" className="mt-0.5 size-4 shrink-0 text-destructive" />
        ) : noFindingTone === "success" ? (
          <ShieldCheck aria-hidden="true" className="mt-0.5 size-4 shrink-0 text-success" />
        ) : (
          <Info aria-hidden="true" className="mt-0.5 size-4 shrink-0 text-muted-foreground" />
        )}
        <div className="min-w-0 flex-1">
          <h2 className="text-xs font-medium tracking-tight text-foreground">
            {selectedThreat
              ? selectedThreat.threat_type
              : mcp?.security.title || "No security findings"}
          </h2>
          <p className="mt-0.5 text-[11px] leading-relaxed text-muted-foreground">
            {selectedThreat
              ? `Detected by ${selectedThreat.detector_name}`
              : mcp?.security.description || "This trace passed all configured checks"}
          </p>
        </div>
        {selectedThreat ? (
          <div className="flex items-center gap-0.5">
            <Button
              type="button"
              variant="ghost"
              size="icon-xs"
              aria-label="Previous finding"
              disabled={selectedIndex <= 0}
              onClick={() => moveSelection(-1)}
            >
              <ChevronLeft aria-hidden="true" className="size-3.5" />
            </Button>
            <span className="min-w-8 text-center font-mono text-[10px] tabular-nums text-muted-foreground">
              {selectedIndex + 1}/{threats.length}
            </span>
            <Button
              type="button"
              variant="ghost"
              size="icon-xs"
              aria-label="Next finding"
              disabled={selectedIndex >= threats.length - 1}
              onClick={() => moveSelection(1)}
            >
              <ChevronRight aria-hidden="true" className="size-3.5" />
            </Button>
          </div>
        ) : null}
      </div>

      <div className="min-h-0 flex-1 divide-y divide-border-subtle overflow-y-auto px-3.5">
        {selectedThreat ? (
          <>
            <div className="py-3.5">
              <FindingExplanationCard finding={selectedThreat} trace={trace} />
            </div>

            <InvestigationSection title="Decision">
              <InvestigationRow label="Action">
                <span className="inline-flex items-center gap-1.5 font-mono text-foreground">
                  <span
                    aria-hidden="true"
                    className={cn(
                      "size-1.5 rounded-full",
                      selectedThreat.action_taken === "block"
                        ? "bg-destructive"
                        : "bg-warn",
                    )}
                  />
                  {selectedThreat.action_taken || "record"}
                </span>
              </InvestigationRow>
              <InvestigationRow label="Weighted score" mono>
                {(weighted * 100).toFixed(0)}%
              </InvestigationRow>
              <InvestigationRow label="Severity" mono>
                {selectedThreat.severity}
              </InvestigationRow>
              <InvestigationRow label="Confidence" mono>
                {((selectedThreat.confidence ?? 0) * 100).toFixed(0)}%
              </InvestigationRow>
            </InvestigationSection>

            <InvestigationSection title="Matched evidence">
              <CopyValue value={selectedThreat.matched_pattern || "No pattern recorded"} />
              {selectedThreat.matched_content ? (
                <pre className="mt-2 max-h-28 overflow-auto whitespace-pre-wrap break-words border border-border-subtle bg-surface-1/50 p-2 font-mono text-[11px] leading-relaxed text-foreground">
                  {selectedThreat.matched_content}
                </pre>
              ) : null}
            </InvestigationSection>

            <InvestigationSection title="Finding context">
              <InvestigationRow label="Detector" mono>
                {selectedThreat.detector_name}
              </InvestigationRow>
              <InvestigationRow label="Subtype" mono>
                {selectedThreat.threat_subtype || "—"}
              </InvestigationRow>
              <InvestigationRow label="Source" mono>
                {selectedThreat.source || "—"}
              </InvestigationRow>
              <InvestigationRow label="End user" mono>
                {selectedThreat.end_user_id || trace.end_user_id || "—"}
              </InvestigationRow>
              <InvestigationRow label="IP address" mono>
                {selectedThreat.ip_address || "—"}
              </InvestigationRow>
            </InvestigationSection>
          </>
        ) : null}

        <InvestigationSection title="Trace context">
          <InvestigationRow label="Environment" mono>
            {trace.environment || "—"}
          </InvestigationRow>
          <InvestigationRow label="Release" mono>
            {trace.release || "—"}
          </InvestigationRow>
          <InvestigationRow label="Session" mono>
            {trace.session_id || "—"}
          </InvestigationRow>
          <InvestigationRow label="End user" mono>
            {trace.end_user_id || "—"}
          </InvestigationRow>
          <InvestigationRow label="Route" mono>
            {trace.path || trace.method}
          </InvestigationRow>
          <InvestigationRow label="Provider" mono>
            {trace.provider} · {trace.model}
          </InvestigationRow>
          <CopyValue value={trace.id} label="Trace ID" />
        </InvestigationSection>
      </div>

      {selectedThreat ? (
        <div className="space-y-2 border-t border-border-subtle p-3.5">
          <Link
            to="/threats/$id"
            params={{ id: selectedThreat.id }}
            className={cn(
              buttonVariants({ variant: "outline", size: "sm" }),
              "w-full justify-between text-xs",
            )}
          >
            <span className="flex items-center gap-1.5">
              <FileJson aria-hidden="true" className="size-3.5" /> Open full threat
            </span>
            <ExternalLink aria-hidden="true" className="size-3.5" />
          </Link>
          <Link
            to="/overlays/new"
            search={{ from_threat: selectedThreat.id, template: undefined }}
            className={cn(buttonVariants({ size: "sm" }), "w-full text-xs")}
          >
            <ShieldPlus aria-hidden="true" className="size-3.5" /> Create policy from finding
          </Link>
          <Link
            to="/security-settings"
            className={cn(buttonVariants({ variant: "outline", size: "sm" }), "w-full text-xs")}
          >
            Review detector strategy
          </Link>
        </div>
      ) : null}
    </aside>
  );
}

function InvestigationSection({
  title,
  children,
}: {
  title: string;
  children: React.ReactNode;
}) {
  return (
    <section className="py-3.5">
      <h3 className="mb-2 text-[11px] font-medium text-muted-foreground">{title}</h3>
      <div className="space-y-1.5">{children}</div>
    </section>
  );
}

function InvestigationRow({
  label,
  children,
  mono = false,
}: {
  label: string;
  children: React.ReactNode;
  mono?: boolean;
}) {
  return (
    <div className="flex min-w-0 items-start gap-3 text-[11px] leading-4">
      <span className="w-[82px] shrink-0 text-muted-foreground">{label}</span>
      <span
        className={cn(
          "min-w-0 flex-1 break-words text-right text-foreground",
          mono && "font-mono tabular-nums",
        )}
      >
        {children}
      </span>
    </div>
  );
}

function CopyValue({ value, label }: { value: string; label?: string }) {
  const [copied, setCopied] = useState(false);
  return (
    <div className="flex items-center gap-2">
      {label ? (
        <span className="w-[82px] shrink-0 text-[11px] text-muted-foreground">{label}</span>
      ) : null}
      <code className="min-w-0 flex-1 truncate font-mono text-[11px] text-foreground">
        {value}
      </code>
      <button
        type="button"
        className="flex size-5 shrink-0 cursor-pointer items-center justify-center text-muted-foreground hover:bg-surface-2 hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
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
        {copied ? (
          <Check aria-hidden="true" className="size-3" />
        ) : (
          <Copy aria-hidden="true" className="size-3" />
        )}
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
    <div className="mx-auto max-w-[1180px] space-y-4 pb-10 pt-2 md:pt-4">
      <TraceHeader trace={t} threatCount={0} />
      <KpiStrip trace={t} threats={[]} />
      <div className="border-y border-border-subtle border-l-2 border-l-border-default bg-surface-1/40 px-4 py-3 text-xs text-muted-foreground">
        This event came from an integration that doesn’t expose span-level detail. Provider:{" "}
        <span className="font-mono text-foreground">{trace.provider ?? "—"}</span>.
      </div>
    </div>
  );
}

function LoadingBlock({ label }: { label: string }) {
  return (
    <div className="mx-auto max-w-[1180px] pb-10 pt-2 md:pt-4">
      <div className="flex items-center justify-center border-y border-border-subtle py-16">
        <div className="flex items-center gap-2 text-xs text-muted-foreground">
          <div className="h-3.5 w-3.5 animate-spin rounded-full border-2 border-muted-foreground/30 border-t-muted-foreground" />
          {label}
        </div>
      </div>
    </div>
  );
}

function EmptyStateCard({ title, description }: { title: string; description: string }) {
  return (
    <div className="border-y border-border-subtle py-12 text-center">
      <p className="text-[13px] font-medium text-foreground">{title}</p>
      <p className="mt-1 text-xs text-muted-foreground">{description}</p>
    </div>
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
        <div className="flex items-center justify-between gap-2">
          <span className="text-xs font-medium text-foreground">Evaluation scores</span>
          <span className="font-mono text-[11px] tabular-nums text-muted-foreground">
            {scores.length}
          </span>
        </div>
        <p className="mt-0.5 text-[11px] leading-relaxed text-muted-foreground">
          Record human feedback or LLM-as-a-judge quality metrics (e.g. correctness, toxicity) for dataset evaluation.
        </p>
      </div>

      {scores.length ? (
        <div className="divide-y divide-border-subtle border-y border-border-subtle">
          {scores.map((s: TraceScore) => (
            <div
              key={s.id}
              className="flex items-center justify-between gap-2 py-2 text-xs"
            >
              <div className="flex min-w-0 items-center gap-2">
                <span className="truncate font-mono text-foreground">{s.name}</span>
                <span className="border border-border-subtle bg-surface-1/50 px-1.5 py-0.5 font-mono text-[10px] tabular-nums text-foreground">
                  {s.value_type === "numeric" ? s.numeric_value?.toFixed(3) : s.string_value}
                </span>
                {s.comment ? (
                  <span className="max-w-[120px] truncate text-[11px] text-muted-foreground">
                    — {s.comment}
                  </span>
                ) : null}
              </div>
              <span className="shrink-0 font-mono text-[10px] text-muted-foreground">
                {s.evaluator || "human"}
              </span>
            </div>
          ))}
        </div>
      ) : (
        <p className="border-y border-border-subtle py-2.5 text-[11px] text-muted-foreground">
          No evaluation scores recorded yet.
        </p>
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
    <form onSubmit={submit} className="space-y-2.5 border-t border-border-subtle pt-3">
      <p className="text-[11px] font-medium text-foreground">Add score</p>
      <div className="grid grid-cols-2 gap-2">
        <div>
          <label htmlFor={`${formId}-name`} className="mb-1 block text-[11px] text-muted-foreground">
            Name
          </label>
          <Input
            id={`${formId}-name`}
            placeholder="e.g. correctness"
            value={name}
            onChange={(e) => setName(e.target.value)}
            autoCorrect="off"
            autoCapitalize="none"
            spellCheck={false}
            className="h-7 text-xs"
            required
          />
        </div>
        <div>
          <label htmlFor={`${formId}-type`} className="mb-1 block text-[11px] text-muted-foreground">
            Type
          </label>
          <Select value={valueType} onValueChange={(v) => setValueType(v as typeof valueType)}>
            <SelectTrigger id={`${formId}-type`} className="h-7 text-xs" aria-label="Score value type">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="numeric">Numeric (0.0 - 1.0)</SelectItem>
              <SelectItem value="categorical">Categorical</SelectItem>
              <SelectItem value="boolean">Boolean (pass/fail)</SelectItem>
            </SelectContent>
          </Select>
        </div>
      </div>

      <div className="grid grid-cols-2 gap-2">
        {valueType === "numeric" ? (
          <div>
            <label htmlFor={`${formId}-numeric`} className="mb-1 block text-[11px] text-muted-foreground">
              Value
            </label>
            <Input
              id={`${formId}-numeric`}
              type="number"
              step="0.01"
              placeholder="e.g. 0.95"
              value={numeric}
              onChange={(e) => setNumeric(e.target.value)}
              className="h-7 text-xs"
              required
            />
          </div>
        ) : (
          <div>
            <label htmlFor={`${formId}-categorical`} className="mb-1 block text-[11px] text-muted-foreground">
              Value
            </label>
            <Input
              id={`${formId}-categorical`}
              placeholder={valueType === "boolean" ? "true / false" : "e.g. pass"}
              value={str}
              onChange={(e) => setStr(e.target.value)}
              autoCorrect="off"
              autoCapitalize="none"
              spellCheck={false}
              className="h-7 text-xs"
              required
            />
          </div>
        )}
        <div>
          <label htmlFor={`${formId}-evaluator`} className="mb-1 block text-[11px] text-muted-foreground">
            Evaluator
          </label>
          <Input
            id={`${formId}-evaluator`}
            placeholder="e.g. human, gpt-4o"
            value={evaluator}
            onChange={(e) => setEvaluator(e.target.value)}
            autoCorrect="off"
            autoCapitalize="none"
            spellCheck={false}
            className="h-7 text-xs"
          />
        </div>
      </div>

      <div className="flex justify-end pt-0.5">
        <Button type="submit" disabled={!name || isPending} size="sm" className="h-7 gap-1 text-xs">
          <Plus aria-hidden="true" className="h-3 w-3" /> Add score
        </Button>
      </div>
    </form>
  );
}
