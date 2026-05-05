import { useMemo } from "react";
import { Link } from "@tanstack/react-router";
import type { Observation, TraceThreatDetection } from "@/api/client";
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
};

// Right-pane detail for the currently selected span. Tabs adapt to span
// type: generation spans show chat bubbles + model parameters, tool spans
// show input/output JSON side-by-side, retrieval/event spans fall back to
// raw JSON. Security tab is always present and scoped to the whole trace.
export function SpanDetailTabs({ span, threats = [], activeTab, onTabChange }: Props) {
  const tokens = (span.input_tokens ?? 0) + (span.output_tokens ?? 0);
  const isGeneration = span.type === "generation";
  const isTool = span.type === "tool";

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
          {span.status === "error" ? (
            <Badge variant="destructive" className="text-[10px] px-1.5 py-0">
              error
            </Badge>
          ) : null}
        </div>
        <div className="mt-2 flex flex-wrap gap-x-6 gap-y-1 text-[11px] text-muted-foreground">
          <Meta label="Duration" value={formatDuration(span.duration_ms ?? 0)} />
          {span.model ? <Meta label="Model" value={span.model} /> : null}
          {tokens ? (
            <Meta
              label="Tokens"
              value={`${span.input_tokens ?? 0} → ${span.output_tokens ?? 0}`}
            />
          ) : null}
          {span.cost_cents ? <Meta label="Cost" value={formatCost(span.cost_cents)} /> : null}
          {span.prompt_name ? (
            <Link
              to="/prompts/$name"
              params={{ name: span.prompt_name }}
              className="inline-flex items-baseline gap-1 font-mono tabular-nums hover:underline"
            >
              <span className="text-[10px] uppercase tracking-wider text-muted-foreground/50">
                Prompt
              </span>
              <span className="text-foreground/90">
                {span.prompt_name}
                {span.prompt_version ? ` v${span.prompt_version}` : ""}
              </span>
            </Link>
          ) : null}
          {span.tool_name ? <Meta label="Tool" value={span.tool_name} /> : null}
        </div>
        {span.status === "error" && span.status_message ? (
          <p className="mt-2 rounded border border-destructive/40 bg-destructive/5 px-2 py-1.5 font-mono text-[11px] text-destructive">
            {span.status_message}
          </p>
        ) : null}
      </div>

      <Tabs value={activeTab} onValueChange={onTabChange} className="flex-1 min-h-0">
        <TabsList variant="line" className="mx-4 mt-2">
          <TabsTrigger value="input">Input</TabsTrigger>
          <TabsTrigger value="output">Output</TabsTrigger>
          <TabsTrigger value="metadata">Metadata</TabsTrigger>
          <TabsTrigger value="security">
            Security {threats.length ? `(${threats.length})` : ""}
          </TabsTrigger>
        </TabsList>
        <div className="flex-1 overflow-auto px-4 pb-4">
          <TabsContent value="input" className="pt-3">
            {isTool ? (
              <JsonViewer rawString={span.tool_input ?? span.input ?? ""} />
            ) : isGeneration ? (
              <ChatMessages raw={span.input ?? ""} label="Messages" direction="in" />
            ) : (
              <JsonViewer rawString={span.input ?? ""} />
            )}
          </TabsContent>
          <TabsContent value="output" className="pt-3">
            {isTool ? (
              <JsonViewer rawString={span.tool_output ?? span.output ?? ""} />
            ) : isGeneration ? (
              <ChatMessages raw={span.output ?? ""} label="Completion" direction="out" />
            ) : (
              <JsonViewer rawString={span.output ?? ""} />
            )}
          </TabsContent>
          <TabsContent value="metadata" className="pt-3 space-y-3">
            {parsedParameters ? (
              <section>
                <p className="mb-1 text-[10px] font-semibold uppercase tracking-wider text-muted-foreground/60">
                  Model parameters
                </p>
                <JsonViewer value={parsedParameters} maxHeight="20rem" />
              </section>
            ) : null}
            <section>
              <p className="mb-1 text-[10px] font-semibold uppercase tracking-wider text-muted-foreground/60">
                Span
              </p>
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
              />
            </section>
          </TabsContent>
          <TabsContent value="security" className="pt-3">
            <ThreatCascade threats={threats} />
          </TabsContent>
        </div>
      </Tabs>
    </div>
  );
}

function Meta({ label, value, className }: { label: string; value: string; className?: string }) {
  return (
    <span className={cn("inline-flex items-baseline gap-1 font-mono tabular-nums", className)}>
      <span className="text-[10px] uppercase tracking-wider text-muted-foreground/50">{label}</span>
      <span className="text-foreground/90">{value}</span>
    </span>
  );
}
