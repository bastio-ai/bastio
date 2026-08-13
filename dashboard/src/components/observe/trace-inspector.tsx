import { Link } from "@tanstack/react-router";
import { ExternalLink, MessagesSquare, X } from "lucide-react";

import type { Trace } from "@/api/client";
import { Badge } from "@/components/ui/badge";
import { Button, buttonVariants } from "@/components/ui/button";
import { formatCost, formatDuration, formatNumber } from "@/lib/utils";

export function TraceInspector({ trace, onClose }: { trace: Trace; onClose: () => void }) {
  return (
    <aside className="hidden h-full w-[326px] flex-shrink-0 flex-col border-l border-border-subtle bg-background xl:flex">
      <div className="flex items-start gap-2 border-b border-border-subtle px-4 py-4">
        <span className={trace.status === "ok" ? "mt-1 h-2 w-2 rounded-full bg-success" : "mt-1 h-2 w-2 rounded-full bg-danger"} />
        <div className="min-w-0 flex-1">
          <div className="flex items-center gap-2">
            <h2 className="truncate text-sm font-semibold">{trace.trace_name || trace.path || trace.model}</h2>
            <Badge variant={trace.status === "ok" ? "success" : trace.status === "blocked" ? "destructive" : "warning"} className="px-1.5 py-0 text-[9px]">{trace.status}</Badge>
          </div>
          <p className="mt-1 font-mono text-[10px] leading-4 text-muted-foreground">{trace.id}</p>
        </div>
        <Button variant="ghost" size="icon-xs" onClick={onClose} aria-label="Close trace details"><X className="h-3.5 w-3.5" /></Button>
      </div>

      <div className="min-h-0 flex-1 space-y-3 overflow-y-auto p-3">
        <InspectorCard title="Request">
          <Metric label="Started" value={new Date(trace.started_at).toLocaleString()} mono />
          <Metric label="Duration" value={formatDuration(trace.duration_ms)} mono />
          <Metric label="Provider" value={trace.provider || "—"} />
          <Metric label="Model" value={trace.model || "—"} mono />
          <Metric label="Environment" value={trace.environment || "—"} />
          <Metric label="Release" value={trace.release || "—"} mono />
        </InspectorCard>

        <InspectorCard title="Usage">
          <Metric label="Input tokens" value={formatNumber(trace.input_tokens ?? 0)} mono />
          <Metric label="Output tokens" value={formatNumber(trace.output_tokens ?? 0)} mono />
          <Metric label="Total tokens" value={formatNumber((trace.input_tokens ?? 0) + (trace.output_tokens ?? 0))} mono />
          <Metric label="Cost" value={formatCost(trace.cost_cents ?? 0)} mono />
        </InspectorCard>

        <InspectorCard title="Security">
          <Metric label="Verdict" value={trace.threat_detected ? "Threat detected" : "Clean"} />
          <Metric label="Threat score" value={trace.threat_detected ? `${Math.round((trace.threat_score ?? 0) * 100)}%` : "—"} mono />
          <Metric label="Threat types" value={(trace.threat_types ?? []).join(", ") || "—"} mono />
        </InspectorCard>

        <InspectorCard title="Context">
          <Metric label="End user" value={trace.end_user_id || "—"} mono />
          <Metric label="Session" value={trace.session_id || "—"} mono />
          <Metric label="Path" value={trace.path || "—"} mono />
        </InspectorCard>
      </div>

      <div className="space-y-2 border-t border-border-subtle p-3">
        <Link to="/traces/$id" params={{ id: trace.id }} className={buttonVariants({ size: "sm" }) + " w-full text-[11px]"}>Open full trace <ExternalLink className="h-3.5 w-3.5" /></Link>
        {trace.session_id ? <Link to="/sessions/$id" params={{ id: trace.session_id }} className={buttonVariants({ variant: "outline", size: "sm" }) + " w-full text-[11px]"}><MessagesSquare className="h-3.5 w-3.5" /> Open session</Link> : null}
      </div>
    </aside>
  );
}

function InspectorCard({ title, children }: { title: string; children: React.ReactNode }) {
  return <section className="rounded-md border border-border-subtle bg-surface-1 p-3"><h3 className="mb-2 text-[10px] font-medium text-muted-foreground">{title}</h3><div className="space-y-1.5">{children}</div></section>;
}

function Metric({ label, value, mono = false }: { label: string; value: string; mono?: boolean }) {
  return <div className="flex items-start gap-3 text-[10px] leading-4"><span className="w-[88px] flex-shrink-0 text-muted-foreground">{label}</span><span className={(mono ? "font-mono tabular-nums " : "") + "min-w-0 flex-1 break-all text-right text-foreground/90"}>{value}</span></div>;
}
