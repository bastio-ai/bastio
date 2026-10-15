// Workspace MCP traces record operational metadata. Optional payload history
// is independently authorized; imported MCP traces retain their own policy.
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
  retention?: ReturnType<typeof mcpRetentionSummary>;
};

// Policy tags describe capture at call time, never the workspace's current
// policy. Payloads are fetched separately only when a payload tab is opened.
export function mcpRetentionSummary(trace: TraceContext, now = Date.now()) {
  const mode = trace.tags?.["mcp.history.mode"] === "sanitized" ? "sanitized" : "metadata_only";
  const state = trace.tags?.["mcp.history.state"] || "not_retained";
  const expiresAt = trace.tags?.["mcp.history.expires_at"];
  const expired = Boolean(expiresAt && Date.parse(expiresAt) <= now);
  const reason = trace.tags?.["mcp.history.reason"];
  const label = mode === "metadata_only" ? "Metadata only" : expired ? "Expired" : state === "retained" ? "Sanitized history" : "Not captured";
  const description = mode === "sanitized" && state === "retained"
    ? expired ? "The captured content has expired and is no longer available." : "Sanitized snapshots were retained under the workspace policy. Viewing them requires separate authorization."
    : reason === "temporary_chat" ? "Temporary chat privacy disabled payload capture for this call."
    : reason === "workspace_no_store" || reason === "storage_policy_changed" ? "The workspace privacy policy prevented payload capture for this call."
    : reason === "policy_changed" ? "The history policy changed during this call, so its payloads were not retained."
    : mode === "sanitized" ? "Sanitized history was enabled, but this call's payload was not retained."
    : "This call retained timing, outcomes and security decisions only. Payload content was not captured.";
  return {mode, state, expiresAt, expired, label, description};
}

export function mcpPayloadAppliesToSpan(span: { id: string; trace_id: string; name?: string; type: string }, direction: "input" | "output") {
  return span.id === span.trace_id || span.type === "tool" || span.name === (direction === "input" ? "mcp.arguments" : "mcp.result");
}
