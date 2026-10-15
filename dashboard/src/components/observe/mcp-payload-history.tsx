import { useEffect, useId, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { LockKeyhole } from "lucide-react";
import { http, unwrap } from "@/api/typed";
import type { Observation } from "@/api/client";
import { Button } from "@/components/ui/button";
import { JsonViewer } from "./json-viewer";
import {
  mcpPayloadAppliesToSpan,
  type McpTracePresentation,
} from "./mcp-trace-presentation";

export function McpPayloadHistory({
  span,
  direction,
  mcp,
  onTabChange,
}: {
  span: Observation;
  direction: "input" | "output";
  mcp: McpTracePresentation;
  onTabChange: (tab: string) => void;
}) {
  const retention = mcp.retention;
  const viewId = useId();
  const applies = mcpPayloadAppliesToSpan(span, direction);
  const [clock, setClock] = useState(() => Date.now());
  const taggedExpiry = retention?.expiresAt
    ? Date.parse(retention.expiresAt)
    : Infinity;
  const history = useQuery({
    queryKey: ["mcp-history-content", span.trace_id, viewId],
    queryFn: ({ signal }) =>
      unwrap(
        http.GET("/v1/workspace/mcp/traces/{id}/content", {
          params: { path: { id: span.trace_id } },
          cache: "no-store",
          signal,
        }),
      ),
    enabled:
      applies &&
      retention?.mode === "sanitized" &&
      retention.state === "retained" &&
      clock < taggedExpiry,
    gcTime: 0,
    staleTime: 0,
    retry: false,
    refetchInterval: 15_000,
  });
  const expiresAt = history.data?.expires_at || retention?.expiresAt;
  const expiry = expiresAt ? Date.parse(expiresAt) : Infinity;
  const expired = clock >= expiry;
  useEffect(() => {
    if (!Number.isFinite(expiry) || expired) return;
    const timer = window.setTimeout(
      () => setClock(Date.now()),
      Math.min(2_147_483_647, Math.max(0, expiry - Date.now())),
    );
    return () => window.clearTimeout(timer);
  }, [expiry, expired, clock]);
  const payloadState =
    direction === "input"
      ? history.data?.content?.input_state
      : history.data?.content?.output_state;
  const payload =
    direction === "input"
      ? history.data?.content?.input
      : history.data?.content?.output;
  const captureExpected =
    applies &&
    retention?.mode === "sanitized" &&
    retention.state === "retained";
  const mayShow =
    captureExpected &&
    applies &&
    !expired &&
    !history.isError &&
    history.data?.state === "sanitized" &&
    payloadState === "sanitized" &&
    payload !== undefined;
  if (mayShow)
    return (
      <section className="space-y-3" aria-label={`Sanitized tool ${direction}`}>
        <div className="rounded-md border border-border-subtle bg-surface-1 p-3 text-xs">
          <p className="font-medium">
            Sanitized{" "}
            {direction === "input" ? "tool arguments" : "tool response"}
          </p>
          <p className="mt-1 text-[11px] leading-relaxed text-muted-foreground">
            This is a redacted snapshot, which can differ from what was sent or
            returned.{" "}
            {expiresAt
              ? `Expires ${new Date(expiresAt).toLocaleString()}.`
              : ""}
          </p>
        </div>
        <JsonViewer
          rawString={
            typeof payload === "string"
              ? payload
              : JSON.stringify(payload, null, 2)
          }
        />
      </section>
    );
  let title = `${direction === "input" ? "Input" : "Output"} content not retained`;
  let description =
    retention?.description ||
    "To protect sensitive data, this MCP call retained operational metadata only. An admin can enable sanitized history for future calls under Workspace settings → Privacy & data.";
  if (!applies) {
    title = "No payload captured for this step";
    description =
      "Call history is available on the root call, the tool step, and its corresponding inspection step.";
  } else if (expired || history.data?.state === "expired") {
    title = "History expired";
    description =
      "The captured payload has expired and can no longer be viewed. Timing and security metadata remain available.";
  } else if (captureExpected && history.isPending) {
    title = "Loading sanitized history";
    description = "Checking your access to this call’s retained snapshots…";
  } else if (captureExpected && history.isError) {
    title = "History could not be loaded";
    description =
      "The retained content is unavailable right now. This does not change the tool’s outcome.";
  } else if (history.data?.state === "restricted") {
    title = "History access restricted";
    description =
      "Captured content is available to owners, admins and selected reviewers. Reviewers also need access to the integration.";
  } else if (history.data?.state === "not_retained") {
    title = "History no longer available";
    description =
      "The captured payload is unavailable under the current storage policy, or it has been deleted.";
  } else if (payloadState === "withheld") {
    title = "Payload history withheld";
    description =
      "Blocked content and content that could not be safely sanitized are not retained.";
  } else if (payloadState === "size_limit") {
    title = "Payload exceeds history limits";
    description =
      "This payload was omitted from history because of the capture size limit. The tool’s outcome is shown separately.";
  } else if (
    direction === "output" &&
    (payloadState === "not_run" || mcp.details.remoteAction === "Not executed")
  ) {
    title = "No tool response";
    description =
      "The recorded remote action is Not executed, so there is no tool response to inspect.";
  } else if (payloadState === "not_recorded") {
    title = "No snapshot recorded";
    description =
      "This direction did not produce an eligible snapshot. The recorded tool outcome and security decisions are available in Overview.";
  }
  return (
    <section
      aria-label={`${direction} history availability`}
      className="rounded-lg border border-border-subtle bg-surface-1 p-5"
    >
      <LockKeyhole className="mb-3 size-5 text-muted-foreground" />
      <h3 className="text-sm font-semibold tracking-tight">{title}</h3>
      <p className="mt-2 max-w-xl text-xs leading-relaxed text-muted-foreground">
        {description}
      </p>
      <div className="mt-4 flex flex-wrap gap-2">
        {history.isError && captureExpected ? (
          <Button
            type="button"
            size="sm"
            variant="outline"
            onClick={() => void history.refetch()}
          >
            Retry history
          </Button>
        ) : null}
        <Button
          type="button"
          variant="outline"
          size="sm"
          onClick={() => onTabChange("metadata")}
        >
          View call overview
        </Button>
        <Button
          type="button"
          variant="ghost"
          size="sm"
          onClick={() => onTabChange("security")}
        >
          View security checks
        </Button>
      </div>
    </section>
  );
}
