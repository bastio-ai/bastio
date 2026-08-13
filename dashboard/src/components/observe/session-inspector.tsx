import { Link } from "@tanstack/react-router";
import { ExternalLink, X } from "lucide-react";

import type { Session } from "@/api/client";
import { Badge } from "@/components/ui/badge";
import { Button, buttonVariants } from "@/components/ui/button";
import { formatCost, formatDuration, formatNumber } from "@/lib/utils";

export function SessionInspector({ session, onClose }: { session: Session; onClose: () => void }) {
  const environment = (session as Session & { environment?: string }).environment;
  return (
    <aside className="hidden h-full w-[326px] flex-shrink-0 flex-col border-l border-border-subtle bg-background xl:flex">
      <div className="flex items-start gap-2 border-b border-border-subtle px-4 py-4">
        <span className={session.threat_count ? "mt-1 h-2 w-2 rounded-full bg-danger" : "mt-1 h-2 w-2 rounded-full bg-success"} />
        <div className="min-w-0 flex-1">
          <div className="flex items-center gap-2"><h2 className="truncate text-sm font-semibold">Session</h2><Badge variant={session.threat_count ? "destructive" : "success"} className="px-1.5 py-0 text-[9px]">{session.threat_count ? "review" : "clean"}</Badge></div>
          <p className="mt-1 break-all font-mono text-[10px] leading-4 text-muted-foreground">{session.id}</p>
        </div>
        <Button variant="ghost" size="icon-xs" onClick={onClose} aria-label="Close session details"><X className="h-3.5 w-3.5" /></Button>
      </div>

      <div className="min-h-0 flex-1 space-y-3 overflow-y-auto p-3">
        <InspectorCard title="Activity">
          <Metric label="Traces" value={formatNumber(session.trace_count ?? 0)} mono />
          <Metric label="Last activity" value={new Date(session.last_completed_at).toLocaleString()} mono />
          <Metric label="Wall clock" value={formatDuration(session.wall_clock_ms ?? 0)} mono />
          <Metric label="Environment" value={environment || "—"} />
        </InspectorCard>
        <InspectorCard title="Usage">
          <Metric label="Tokens" value={formatNumber(session.total_tokens ?? 0)} mono />
          <Metric label="Cost" value={formatCost(session.total_cost_cents ?? 0)} mono />
          <Metric label="Avg / trace" value={formatCost(session.trace_count ? (session.total_cost_cents ?? 0) / session.trace_count : 0)} mono />
        </InspectorCard>
        <InspectorCard title="Security">
          <Metric label="Threats" value={formatNumber(session.threat_count ?? 0)} mono />
          <Metric label="Verdict" value={session.threat_count ? "Needs review" : "Clean"} />
        </InspectorCard>
        <InspectorCard title="Context">
          <Metric label="End user" value={session.end_user_id || "—"} mono />
          <Metric label="Session ID" value={session.id} mono />
        </InspectorCard>
      </div>

      <div className="border-t border-border-subtle p-3">
        <Link to="/sessions/$id" params={{ id: session.id }} className={buttonVariants({ size: "sm" }) + " w-full text-[11px]"}>Open full session <ExternalLink className="h-3.5 w-3.5" /></Link>
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
