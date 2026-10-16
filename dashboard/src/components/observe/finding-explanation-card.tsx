import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { api } from "@/api/client";
import { useSecurityExtension } from "@/components/security-extension";
import { findingExplanation, type ExplainableFinding, type FindingTraceContext } from "./finding-explanation";

export function FindingExplanationCard({ finding, trace, loading = false, unavailable = false }: {
  finding: ExplainableFinding;
  trace?: FindingTraceContext;
  loading?: boolean;
  unavailable?: boolean;
}) {
  const explanation = findingExplanation(finding, trace);
  return (
    <section aria-label="Finding explained" className="space-y-3 rounded-md border border-border-subtle bg-surface-1 p-3 text-xs leading-relaxed">
      <div>
        <h3 className="font-semibold tracking-tight">{explanation.title}</h3>
        <p className="mt-1 text-muted-foreground">{explanation.description}</p>
      </div>
      <dl className="space-y-2">
        {trace?.trace_name ? <div><dt className="text-muted-foreground">Application operation</dt><dd className="break-words font-mono text-[11px]">{trace.trace_name}</dd></div> : null}
        {trace?.provider ? <div><dt className="text-muted-foreground">Provider / model</dt><dd className="break-words font-mono text-[11px]">{[trace.provider, trace.model].filter(Boolean).join(" · ")}</dd></div> : null}
        <div><dt className="text-muted-foreground">Detected in</dt><dd>{explanation.direction || "Request or response — stage not recorded"}</dd></div>
        <div><dt className="text-muted-foreground">Outcome</dt><dd>{explanation.outcome}</dd></div>
      </dl>
      {loading ? <p role="status" className="text-muted-foreground">Loading operation context…</p> : null}
      {unavailable ? <p className="text-muted-foreground">Related trace context could not be loaded.</p> : null}
      {explanation.evidence ? <div className="space-y-2 border-t border-border-subtle pt-3"><p className="font-medium">Evidence unavailable</p><p className="text-muted-foreground">{explanation.evidence}</p><RecordingSettingsLink /></div> : null}
      <details className="border-t border-border-subtle pt-2 text-[11px] text-muted-foreground"><summary className="cursor-pointer hover:text-foreground">How to interpret severity and score</summary><p className="mt-2">Severity describes the detector's risk category. The weighted score is a policy score, not the probability of a data leak. Application task labels do not establish whether the matched value is personal data.</p></details>
    </section>
  );
}

export function RecordingSettingsLink() {
  const { extraTabs = [] } = useSecurityExtension();
  if (!extraTabs.some((tab) => tab.id === "privacy-filter")) return null;
  return <Link to="/security-settings" search={{ tab: "privacy-filter" }} className="cursor-pointer underline underline-offset-4 hover:text-foreground">Review AI content recording</Link>;
}

// Fetch context only for an opened finding, never once per table row. Uses
// the trace page's query key so opening the related trace reuses the result.
export function ConnectedFindingExplanation({ finding }: { finding: ExplainableFinding & { trace_id: string } }) {
  const context = useQuery({
    queryKey: ["trace", finding.trace_id],
    queryFn: () => api.traces.get(finding.trace_id),
    enabled: Boolean(finding.trace_id),
    staleTime: 30_000,
    retry: false,
  });
  return <FindingExplanationCard finding={finding} trace={context.data?.trace} loading={context.isPending && Boolean(finding.trace_id)} unavailable={context.isError} />;
}
