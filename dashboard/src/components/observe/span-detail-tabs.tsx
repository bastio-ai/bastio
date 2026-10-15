import { McpPayloadHistory } from "./mcp-payload-history";
import { useEffect, useMemo, useState } from "react";
import { Braces, Info, MessageSquareText, ShieldAlert, ShieldCheck } from "lucide-react";
import type { Observation, TraceThreatDetection } from "@/api/client";
import { Button } from "@/components/ui/button";
import type { McpTracePresentation } from "./mcp-trace-presentation";
import { Badge } from "@/components/ui/badge";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { cn, formatCost, formatDuration } from "@/lib/utils";
import { ChatMessages } from "./chat-messages";
import { JsonViewer } from "./json-viewer";
import { ThreatCascade } from "./threat-cascade";

type Props = {
  span: Observation;
  threats?: TraceThreatDetection[];
  activeTab: string;
  onTabChange: (tab: string) => void;
  selectedThreatId?: string | null;
  onThreatSelect?: (threat: TraceThreatDetection) => void;
  securityReviewSignal?: number;
  mcp?: McpTracePresentation;
};

// Right-pane detail for the currently selected span. Tabs adapt to span
// type: generation spans show chat bubbles + model parameters, tool spans
// show input/output JSON side-by-side, retrieval/event spans fall back to
// raw JSON. Security tab is always present and scoped to the whole trace.
export function SpanDetailTabs({
  span,
  threats = [],
  activeTab,
  onTabChange,
  selectedThreatId,
  onThreatSelect,
  securityReviewSignal = 0,
  mcp,
}: Props) {
  const tokens = (span.input_tokens ?? 0) + (span.output_tokens ?? 0);
  const isGeneration = span.type === "generation";
  const isTool = span.type === "tool";
  const input = isTool ? span.tool_input || span.input || "" : span.input || "";
  const output = isTool ? span.tool_output || span.output || "" : span.output || "";
  const [securitySpotlight, setSecuritySpotlight] = useState(0);

  useEffect(() => {
    if (!securityReviewSignal) return;
    setSecuritySpotlight(securityReviewSignal);
    const timeout = window.setTimeout(() => setSecuritySpotlight(0), 1800);
    return () => window.clearTimeout(timeout);
  }, [securityReviewSignal]);

  const parsedParameters = useMemo(() => {
    if (!span.model_parameters) return null;
    try {
      return JSON.parse(span.model_parameters) as Record<string, unknown>;
    } catch {
      return null;
    }
  }, [span.model_parameters]);

  return (
    <div className="flex h-full flex-col">
      <div className="border-b border-border/40 bg-muted/10 px-4 py-3">
        <div className="flex flex-wrap items-center gap-x-4 gap-y-1">
          <span className="font-mono text-sm font-semibold">{span.name || span.type}</span>
          <Badge variant="outline" className="text-[10px] px-1.5 py-0 font-mono">
            {span.type}
          </Badge>
          {span.status === "error" || span.status === "blocked" ? (
            <Badge variant="destructive" className="text-[10px] px-1.5 py-0">
              {span.status}
            </Badge>
          ) : null}
        </div>
        <div className="mt-2 flex flex-wrap gap-x-6 gap-y-1 text-[11px] text-muted-foreground">
          <Meta label="Duration" value={formatSpanDuration(span.duration_ms ?? 0)} />
          {span.model ? <Meta label="Model" value={span.model} /> : null}
          {tokens ? (
            <Meta
              label="Tokens"
              value={`${span.input_tokens ?? 0} → ${span.output_tokens ?? 0}`}
            />
          ) : null}
          {span.cost_cents ? <Meta label="Cost" value={formatCost(span.cost_cents)} /> : null}
          {span.prompt_name ? (
            <Meta
              label="Prompt"
              value={`${span.prompt_name}${span.prompt_version ? ` v${span.prompt_version}` : ""}`}
            />
          ) : null}
          {span.tool_name ? <Meta label="Tool" value={span.tool_name} /> : null}
        </div>
        {(span.status === "error" || span.status === "blocked") && span.status_message ? (
          <p className="mt-2 rounded border border-destructive/40 bg-destructive/5 px-2 py-1.5 font-mono text-[11px] text-destructive">
            {span.status_message}
          </p>
        ) : null}
      </div>

      <Tabs value={activeTab} onValueChange={onTabChange} className="flex-1 min-h-0">
        <TabsList variant="line" className="mx-4 mt-2 max-w-[calc(100%-2rem)] overflow-x-auto" aria-label="Span details">
          {mcp ? <TabsTrigger value="metadata">Overview</TabsTrigger> : null}
          <TabsTrigger value="input">Input</TabsTrigger>
          <TabsTrigger value="output">Output</TabsTrigger>
          {!mcp ? <TabsTrigger value="metadata">Metadata</TabsTrigger> : null}
          <TabsTrigger value="security">
            Security {threats.length ? `(${threats.length})` : ""}
          </TabsTrigger>
        </TabsList>
        <div className="flex-1 overflow-auto px-4 pb-4">
          <TabsContent value="input" keepMounted={false} className="pt-3">
            {mcp && !input.trim() ? <McpPayloadHistory span={span} direction="input" mcp={mcp} onTabChange={onTabChange} /> : <EvidenceViewer
              key={`${span.id}-input`}
              raw={input}
              label={isGeneration ? "Messages" : "Request"}
              direction="in"
              forceRaw={isTool}
            />}
          </TabsContent>
          <TabsContent value="output" keepMounted={false} className="pt-3">
            {mcp && !output.trim() ? <McpPayloadHistory span={span} direction="output" mcp={mcp} onTabChange={onTabChange} /> : <EvidenceViewer
              key={`${span.id}-output`}
              raw={output}
              label={isGeneration ? "Completion" : "Response"}
              direction="out"
              forceRaw={isTool}
            />}
          </TabsContent>
          <TabsContent value="metadata" className="pt-3 space-y-3">
            {mcp ? <section aria-label="MCP call overview" className="space-y-3">
              <div>
                <h3 className="text-sm font-semibold tracking-tight">MCP call overview</h3>
                <p className="mt-1 text-xs leading-relaxed text-muted-foreground">{mcp.retention?.description || "This call retained timing, outcomes and security decisions only."}</p>
              </div>
              <div className="grid gap-px overflow-hidden rounded-md border border-border-subtle bg-border-subtle sm:grid-cols-2">
                <MetadataItem label="Tool" value={mcp.details.tool} mono />
                <MetadataItem label="Started by" value={mcp.details.source} />
                <MetadataItem label="Remote action" value={mcp.details.remoteAction} />
                <MetadataItem label="Content retention" value={mcp.retention?.label || "Metadata only"} />
                {mcp.retention?.expiresAt ? <MetadataItem label="Content expires" value={new Date(mcp.retention.expiresAt).toLocaleString()} /> : null}
                <MetadataItem label="Connection ID" value={mcp.details.connection} mono />
              </div>
              <Button type="button" variant="outline" size="sm" className="cursor-pointer" onClick={() => onTabChange("security")}>
                <ShieldCheck className="size-3.5" /> View security checks
              </Button>
            </section> : null}
            {parsedParameters ? (
              <section>
                <p className="mb-1 text-[10px] font-semibold uppercase tracking-wider text-muted-foreground/60">
                  Model parameters
                </p>
                <JsonViewer value={parsedParameters} maxHeight="20rem" />
              </section>
            ) : null}
            <section aria-labelledby="span-metadata-heading">
              <h3 id="span-metadata-heading" className="mb-2 text-[10px] font-semibold uppercase tracking-wider text-muted-foreground/60">
                Span properties
              </h3>
              <div className="grid gap-px overflow-hidden rounded-md border border-border-subtle bg-border-subtle sm:grid-cols-2">
                <MetadataItem label="Span type" value={span.type} />
                <MetadataItem label="Status" value={span.status || "Not recorded"} />
                {span.status_message ? <MetadataItem label="Decision" value={span.status_message} /> : null}
                <MetadataItem label="Depth" value={String(span.depth ?? 0)} />
                <MetadataItem label="Started" value={new Date(span.started_at).toLocaleString()} />
                <MetadataItem label="Completed" value={new Date(span.completed_at).toLocaleString()} />
                <MetadataItem label="Span ID" value={span.id} mono />
                <MetadataItem label="Parent ID" value={span.parent_id || "Root"} mono />
                {span.prompt_name ? <MetadataItem label="Prompt" value={`${span.prompt_name}${span.prompt_version ? ` v${span.prompt_version}` : ""}`} mono /> : null}
                {span.model ? <MetadataItem label="Model" value={span.model} mono /> : null}
              </div>
              <details className="mt-3 rounded-md border border-border-subtle bg-surface-1">
                <summary className="cursor-pointer px-3 py-2 text-[10px] text-muted-foreground hover:text-foreground">
                  Raw span metadata
                </summary>
                <div className="border-t border-border-subtle p-2">
                  <JsonViewer
                    value={{
                      id: span.id,
                      trace_id: span.trace_id,
                      parent_id: span.parent_id,
                      type: span.type,
                      depth: span.depth,
                      started_at: span.started_at,
                      completed_at: span.completed_at,
                      prompt_id: span.prompt_id || undefined,
                      prompt_name: span.prompt_name || undefined,
                      prompt_version: span.prompt_version || undefined,
                    }}
                    maxHeight="20rem"
                  />
                </div>
              </details>
            </section>
          </TabsContent>
          <TabsContent
            value="security"
            className="relative pt-3 outline-none"
            id="trace-security-findings"
            tabIndex={-1}
          >
            {securitySpotlight ? (
              <span
                key={securitySpotlight}
                aria-hidden="true"
                data-review-spotlight="true"
                className="pointer-events-none absolute inset-1 z-10 rounded-lg ring-2 ring-warn/60 animate-pulse"
              />
            ) : null}
            {mcp ? <McpInspectionSummary summary={mcp.security} /> : null}
            {threats.length || !mcp ? <ThreatCascade
              threats={threats}
              selectedThreatId={selectedThreatId}
              onSelect={onThreatSelect}
            /> : null}
          </TabsContent>
        </div>
      </Tabs>
    </div>
  );
}

function McpInspectionSummary({ summary }: { summary: McpTracePresentation["security"] }) {
  const Icon = summary.tone === "success" ? ShieldCheck : summary.tone === "danger" ? ShieldAlert : Info;
  return (
    <section aria-label="MCP security checks" className="mb-4 space-y-4 rounded-lg border border-border-subtle bg-surface-1 p-4">
      <div className="flex items-start gap-3">
        <Icon className={cn("mt-0.5 size-4 shrink-0", summary.tone === "success" ? "text-success" : summary.tone === "danger" ? "text-danger" : "text-muted-foreground")} />
        <div>
          <h3 className="text-sm font-semibold tracking-tight">{summary.title}</h3>
          <p className="mt-1 text-xs leading-relaxed text-muted-foreground">{summary.description}</p>
        </div>
      </div>
      <div className="divide-y divide-border-subtle">
        {summary.stages.map((stage) => <div key={stage.name} className="flex flex-wrap items-center justify-between gap-2 py-3">
          <div>
            <p className="text-xs font-medium">{stage.label}</p>
            <p className="mt-1 text-[11px] text-muted-foreground">{stage.detail}</p>
          </div>
          <Badge variant={stage.tone === "danger" ? "destructive" : stage.tone === "success" ? "success" : "outline"} className="text-[10px]">{stage.state}</Badge>
        </div>)}
      </div>
      <p className="text-[11px] leading-relaxed text-muted-foreground">Checks cover the whole MCP call. Any captured payload history is sanitized and has separate access controls.</p>
    </section>
  );
}

function EvidenceViewer({
  raw,
  label,
  direction,
  forceRaw = false,
}: {
  raw: string;
  label: string;
  direction: "in" | "out";
  forceRaw?: boolean;
}) {
  const [mode, setMode] = useState<"readable" | "raw">(forceRaw ? "raw" : "readable");

  return (
    <section aria-label={`${label} evidence`}>
      {!forceRaw ? (
        <div className="mb-2 flex items-center justify-between">
          <h3 className="text-[10px] font-semibold uppercase tracking-wider text-muted-foreground/60">
            {label}
          </h3>
          <div className="flex rounded-md border border-border-subtle bg-surface-1 p-0.5" aria-label="Evidence display mode">
            <button
              type="button"
              aria-pressed={mode === "readable"}
              onClick={() => setMode("readable")}
              className={cn(
                "flex h-6 items-center gap-1 rounded px-2 text-[9px] text-muted-foreground",
                mode === "readable" && "bg-background text-foreground shadow-sm",
              )}
            >
              <MessageSquareText className="size-3" /> Readable
            </button>
            <button
              type="button"
              aria-pressed={mode === "raw"}
              onClick={() => setMode("raw")}
              className={cn(
                "flex h-6 items-center gap-1 rounded px-2 text-[9px] text-muted-foreground",
                mode === "raw" && "bg-background text-foreground shadow-sm",
              )}
            >
              <Braces className="size-3" /> Raw
            </button>
          </div>
        </div>
      ) : null}

      {mode === "readable" && !forceRaw ? (
        <ChatMessages raw={raw} direction={direction} />
      ) : (
        <JsonViewer rawString={raw} />
      )}
    </section>
  );
}

function MetadataItem({ label, value, mono = false }: { label: string; value: string; mono?: boolean }) {
  return (
    <div className="min-w-0 bg-background px-3 py-2.5">
      <p className="text-[9px] uppercase tracking-wider text-muted-foreground/60">{label}</p>
      <p className={cn("mt-1 truncate text-[10px] text-foreground/90", mono && "font-mono") } title={value}>
        {value}
      </p>
    </div>
  );
}

function formatSpanDuration(durationMs: number) {
  return durationMs < 1 ? "<1ms" : formatDuration(durationMs);
}

function Meta({ label, value, className }: { label: string; value: string; className?: string }) {
  return (
    <span className={cn("inline-flex items-baseline gap-1 font-mono tabular-nums", className)}>
      <span className="text-[10px] uppercase tracking-wider text-muted-foreground/50">{label}</span>
      <span className="text-foreground/90">{value}</span>
    </span>
  );
}
