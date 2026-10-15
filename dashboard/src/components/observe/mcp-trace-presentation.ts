// These workspace MCP routes always record operational metadata only.
// Do not apply that storage policy to imported MCP traces or other providers.
type TraceContext = {
  provider?: string;
  path?: string;
  trace_name?: string;
  status?: string;
  tags?: Record<string, string>;
};
type InspectionSpan = { name?: string; status?: string };
type Finding = { source?: string; action_taken?: string };

export function isWorkspaceMcpTrace(trace: TraceContext) {
  return trace.provider === "mcp" && Boolean(trace.path?.startsWith("/v1/workspace/mcp/connections/") || trace.trace_name?.startsWith("workspace.mcp."));
}

export function initialTraceTab(trace: TraceContext, findingCount: number) {
  return findingCount > 0 ? "security" : isWorkspaceMcpTrace(trace) ? "metadata" : "input";
}

export function mcpCallDetails(trace: TraceContext) {
  const source = trace.tags?.["mcp.source"];
  const remote = trace.tags?.remote_action;
  return {
    tool: trace.tags?.["mcp.tool"] || "Not recorded",
    source: source === "manual" ? "Manual tool test" : source === "assistant" ? "Assistant conversation" : source || "Not recorded",
    connection: trace.tags?.["mcp.connection_id"] || "Not recorded",
    remoteAction: remote === "completed" ? "Completed" : remote === "not_run" ? "Not executed" : remote === "unknown" ? "Unknown" : "Not recorded",
  };
}

export function mcpSecuritySummary(trace: TraceContext, spans: InspectionSpan[], findings: Finding[], findingsState: "ready" | "loading" | "error" = "ready") {
  const unattributed = findings.some(({ source }) => source !== "mcp.arguments" && source !== "mcp.result");
  const stages = [
    { name: "mcp.arguments", label: "Tool arguments", detail: "Inspection before forwarding to the tool" },
    { name: "mcp.result", label: "Tool response", detail: "Inspection before returning the result" },
  ].map((stage) => {
    const span = spans.find(({ name }) => name === stage.name);
    const signals = findings.filter(({ source }) => source === stage.name).length;
    const status = span?.status;
    let state = "Inspection details unavailable";
    if (status === "blocked") state = "Withheld";
    else if (status === "error") state = "Inspection failed";
    else if (status === "ok") {
      if (findingsState !== "ready") state = "Completed · finding details unavailable";
      else if (signals) state = "Completed · signals recorded";
      else if (unattributed) state = "Completed · signal attribution unavailable";
      else state = "Completed · no findings recorded";
    }
    return {
      ...stage, state,
      tone: status === "blocked" || status === "error" ? "danger" : status === "ok" && findingsState === "ready" ? "success" : "neutral",
    };
  });
  const complete = stages.every(({ tone }) => tone === "success");
  if (trace.status === "blocked") return {
    title: "Security policy withheld content", tone: "danger",
    description: "Review the recorded inspection decisions below. A remote action may already have completed before its result was withheld.", stages,
  };
  if (trace.status !== "ok") return {
    title: "Tool call did not complete", tone: "neutral",
    description: "Only recorded inspections are shown. Missing inspection details do not confirm that a check passed.", stages,
  };
  if (findingsState !== "ready") return {
    title: findingsState === "loading" ? "Loading security findings" : "Security findings unavailable", tone: "neutral",
    description: findingsState === "loading" ? "Inspection timings are available. Security findings are still loading." : "Security findings could not be loaded. An unavailable findings list does not confirm that the call had no findings.", stages,
  };
  if (findings.length) return {
    title: "Security signals recorded", tone: "neutral",
    description: "Review each signal's effective policy action below. A recorded signal alone does not confirm an attack.", stages,
  };
  return {
    title: complete ? "No security findings" : "No security findings recorded",
    tone: complete ? "success" : "neutral",
    description: complete ? "Argument and response inspections completed with no findings recorded." : "Some inspection details are unavailable. An empty findings list alone does not confirm that every check ran.", stages,
  };
}

export type McpTracePresentation = {
  details: ReturnType<typeof mcpCallDetails>;
  security: ReturnType<typeof mcpSecuritySummary>;
};
