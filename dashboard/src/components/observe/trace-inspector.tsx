import { useEffect, useState, type ReactNode } from "react";
import { Link } from "@tanstack/react-router";
import {
  Check,
  ChevronRight,
  Copy,
  ExternalLink,
  ShieldAlert,
  X,
} from "lucide-react";
import type { Trace } from "@/api/client";
import { securityDecision } from "@/components/observe/security-decision";
import { TraceStatus } from "@/components/observe/trace-status";
import { TRACE_TYPES, traceType } from "@/components/observe/trace-filters";
import { Button } from "@/components/ui/button";
import { cn, formatCost, formatDuration, formatNumber } from "@/lib/utils";

export function TraceInspector({
  trace,
  onClose,
}: {
  trace: Trace;
  onClose: () => void;
}) {
  const [tab, setTab] = useState<"request" | "security">("request");
  const [copied, setCopied] = useState(false);
  const [copyFailed, setCopyFailed] = useState(false);
  const decision = securityDecision(trace.security_action);
  useEffect(() => {
    setTab("request");
    setCopied(false);
    setCopyFailed(false);
  }, [trace.id]);
  const copyID = async () => {
    try {
      await navigator.clipboard.writeText(trace.id);
      setCopied(true);
      setCopyFailed(false);
    } catch {
      setCopyFailed(true);
    }
  };

  return (
    <aside
      aria-label="Trace inspector"
      className="hidden h-full w-[320px] flex-shrink-0 flex-col border-l border-border-subtle bg-background xl:flex 2xl:w-[350px]"
    >
      <header className="flex items-start justify-between gap-3 px-5 pb-4 pt-6">
        <div className="min-w-0">
          <p className="mb-2 text-[10px] text-muted-foreground">
            Trace details
          </p>
          <h2 className="break-words text-base font-medium tracking-tight">
            {trace.trace_name || trace.path || trace.model || "Unnamed trace"}
          </h2>
        </div>
        <Button
          variant="ghost"
          size="icon-xs"
          onClick={onClose}
          aria-label="Close trace details"
        >
          <X className="h-3.5 w-3.5" />
        </Button>
      </header>
      <div className="flex items-center justify-between gap-3 px-5 pb-5">
        <TraceStatus status={trace.status} />
        <time
          dateTime={trace.started_at}
          title={new Date(trace.started_at).toLocaleString()}
          className="font-mono text-[10px] text-muted-foreground"
        >
          {new Date(trace.started_at).toLocaleTimeString()}
        </time>
      </div>
      <div
        role="tablist"
        aria-label="Trace details"
        className="flex gap-5 border-y border-border-subtle px-5"
      >
        {(["request", "security"] as const).map((value) => (
          <button
            key={value}
            id={`trace-inspector-tab-${value}`}
            role="tab"
            aria-selected={tab === value}
            aria-controls="trace-inspector-panel"
            tabIndex={tab === value ? 0 : -1}
            onClick={() => setTab(value)}
            onKeyDown={(event) => {
              if (event.key === "ArrowLeft" || event.key === "ArrowRight") {
                event.preventDefault();
                const next = value === "request" ? "security" : "request";
                setTab(next);
                document.getElementById(`trace-inspector-tab-${next}`)?.focus();
              }
            }}
            className={cn(
              "border-b-2 border-transparent py-3 text-[11px] text-muted-foreground hover:text-foreground",
              tab === value && "border-foreground text-foreground",
            )}
          >
            {value === "request" ? "Request" : "Security"}
            {value === "security" && trace.threat_detected ? " · signals" : ""}
          </button>
        ))}
      </div>
      <div
        id="trace-inspector-panel"
        role="tabpanel"
        aria-labelledby={`trace-inspector-tab-${tab}`}
        className="min-h-0 flex-1 overflow-y-auto px-5"
      >
        {tab === "security" ? (
          <InspectorSection title="Security decision">
            <p
              className={cn(
                "mb-3 flex items-center gap-2 text-[13px]",
                decision.tone === "danger" || decision.tone === "warning"
                  ? "text-warn"
                  : "text-foreground",
              )}
            >
              <ShieldAlert className="h-4 w-4" />
              {trace.threat_detected ? decision.label : "No signals recorded"}
            </p>
            <p className="mb-4 text-xs leading-relaxed text-muted-foreground">
              {trace.threat_detected
                ? decision.explanation
                : "No detector signals were recorded for this request."}
            </p>
            <dl>
              <Metric
                label="Signals"
                value={
                  (trace.threat_types ?? []).join(", ").replaceAll("_", " ") ||
                  "None recorded"
                }
              />
              <Metric
                label="Policy action"
                value={trace.security_action || "Not recorded"}
                mono
              />
              <Metric
                label="Signal score"
                value={
                  trace.threat_detected
                    ? `${Math.round((trace.threat_score ?? 0) * 100)}%`
                    : "—"
                }
                mono
              />
            </dl>
            {trace.threat_detected ? (
              <p className="mt-4 text-[10px] leading-relaxed text-muted-foreground">
                Detector signals and policy enforcement are separate. A signal
                alone does not confirm that a request was blocked.
              </p>
            ) : null}
          </InspectorSection>
        ) : (
          <>
            {trace.threat_detected ? (
              <button
                type="button"
                onClick={() => setTab("security")}
                className="flex w-full items-center gap-2.5 border-b border-border-subtle py-4 text-left text-warn"
              >
                <ShieldAlert className="h-4 w-4" />
                <span className="min-w-0 flex-1 text-[11px]">
                  {(trace.threat_types ?? [])[0]?.replaceAll("_", " ") ||
                    "Security signal"}
                  <span className="mt-1 block text-[10px] text-muted-foreground">
                    {decision.label}
                  </span>
                </span>
                <ChevronRight className="h-3 w-3" />
              </button>
            ) : null}
            {trace.error_message ? (
              <p className="border-b border-border-subtle py-4 text-xs leading-relaxed text-danger">
                {trace.error_message}
              </p>
            ) : null}
            <InspectorSection title="Request">
              <dl>
                <Metric
                  label="Type"
                  value={
                    TRACE_TYPES.find((item) => item.value === traceType(trace))
                      ?.label || "Other"
                  }
                />
                <Metric
                  label="Provider"
                  value={trace.provider || "Not recorded"}
                />
                <Metric
                  label="Model / tool"
                  value={trace.model || "Not recorded"}
                  mono
                />
                <Metric
                  label="Route"
                  value={`${trace.method || ""} ${trace.path || "—"}`.trim()}
                  mono
                />
                <Metric
                  label="HTTP status"
                  value={
                    trace.http_status
                      ? String(trace.http_status)
                      : "Not recorded"
                  }
                  mono
                />
              </dl>
            </InspectorSection>
            <InspectorSection title="Performance">
              <dl>
                <Metric
                  label="Latency"
                  value={formatDuration(trace.duration_ms)}
                  mono
                />
                <Metric
                  label="Input tokens"
                  value={formatNumber(trace.input_tokens ?? 0)}
                  mono
                />
                <Metric
                  label="Output tokens"
                  value={formatNumber(trace.output_tokens ?? 0)}
                  mono
                />
                <Metric
                  label="Provider cost"
                  value={formatCost(trace.cost_cents ?? 0)}
                  mono
                />
              </dl>
            </InspectorSection>
            <InspectorSection title="Context">
              <dl>
                <Metric
                  label="Environment"
                  value={trace.environment || "Not recorded"}
                />
                <Metric
                  label="End user"
                  value={
                    trace.end_user_id ? (
                      <Link
                        to="/users/$id"
                        params={{ id: trace.end_user_id }}
                        className="hover:underline"
                      >
                        {trace.end_user_id}
                      </Link>
                    ) : (
                      "Not recorded"
                    )
                  }
                  mono
                />
                <Metric
                  label="Session"
                  value={
                    trace.session_id ? (
                      <Link
                        to="/sessions/$id"
                        params={{ id: trace.session_id }}
                        className="hover:underline"
                      >
                        {trace.session_id}
                      </Link>
                    ) : (
                      "Not recorded"
                    )
                  }
                  mono
                />
                <Metric
                  label="Release"
                  value={trace.release || "Not recorded"}
                  mono
                />
                {Object.entries(trace.tags ?? {}).map(([key, value]) => (
                  <Metric key={key} label={key} value={value} />
                ))}
              </dl>
            </InspectorSection>
          </>
        )}
        <section className="py-5">
          <p className="mb-2 text-[10px] text-muted-foreground">Trace ID</p>
          <p className="break-all font-mono text-[10px] text-text-secondary">
            {trace.id}
          </p>
          <button
            type="button"
            onClick={() => void copyID()}
            className="mt-3 inline-flex items-center gap-1.5 text-[11px] text-muted-foreground hover:text-foreground"
          >
            {copied ? (
              <Check className="h-3 w-3" />
            ) : (
              <Copy className="h-3 w-3" />
            )}
            {copied ? "Copied" : "Copy ID"}
          </button>
          {copyFailed ? (
            <p role="status" className="mt-2 text-[10px] text-muted-foreground">
              Copy unavailable. Select the trace ID above to copy it.
            </p>
          ) : null}
        </section>
      </div>
      <footer className="border-t border-border-subtle px-5 py-3">
        <Link
          to="/traces/$id"
          params={{ id: trace.id }}
          className="inline-flex items-center gap-2 text-xs text-text-secondary hover:text-foreground"
        >
          Open full trace
          <ExternalLink className="h-3 w-3" />
        </Link>
      </footer>
    </aside>
  );
}
function InspectorSection({
  title,
  children,
}: {
  title: string;
  children: ReactNode;
}) {
  return (
    <section className="border-b border-border-subtle py-5">
      <h3 className="mb-3 text-[11px] font-medium">{title}</h3>
      {children}
    </section>
  );
}
function Metric({
  label,
  value,
  mono = false,
}: {
  label: string;
  value: ReactNode;
  mono?: boolean;
}) {
  return (
    <div className="flex items-baseline justify-between gap-3 py-1.5 text-[10px] leading-relaxed">
      <dt className="shrink-0 text-muted-foreground">{label}</dt>
      <dd
        className={cn(
          "m-0 min-w-0 max-w-[70%] break-words text-right text-text-secondary",
          mono && "font-mono tabular-nums",
        )}
      >
        {value}
      </dd>
    </div>
  );
}
