import { useState } from "react";
import { Link } from "@tanstack/react-router";
import {
  Check,
  Copy,
  ExternalLink,
  FileJson,
  ListPlus,
  ShieldPlus,
  X,
} from "lucide-react";

import type { ThreatEvent } from "@/api/client";
import { Badge } from "@/components/ui/badge";
import { Button, buttonVariants } from "@/components/ui/button";
import { cn, weightedThreatScore } from "@/lib/utils";

export function ThreatInspector({
  threat,
  onClose,
}: {
  threat: ThreatEvent;
  onClose: () => void;
}) {
  const details = (threat as ThreatEvent & { details?: Record<string, string> }).details;
  const userAgent = (threat as ThreatEvent & { user_agent?: string }).user_agent;
  const endUserID = (threat as ThreatEvent & { end_user_id?: string }).end_user_id;
  const ipAddress = (threat as ThreatEvent & { ip_address?: string }).ip_address;
  const weighted = weightedThreatScore(threat.score, threat.confidence, threat.weighted_score);

  return (
    <aside className="hidden h-full w-[326px] flex-shrink-0 flex-col border-l border-border-subtle bg-background xl:flex">
      <div className="flex items-start gap-2 border-b border-border-subtle px-4 py-4">
        <span className={cn("mt-1 h-2 w-2 rounded-full", threat.severity === "critical" ? "bg-danger" : "bg-warn")} />
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-2">
            <h2 className="truncate text-sm font-semibold">{threat.threat_type}</h2>
            <Badge variant={threat.severity === "critical" ? "destructive" : "warning"} className="px-1.5 py-0 text-[9px]">{threat.severity}</Badge>
          </div>
          <p className="mt-1 text-[10px] leading-4 text-muted-foreground">
            Detected by <span className="font-mono text-foreground/80">{threat.detector_name}</span><br />
            {new Date(threat.detected_at).toLocaleString()}
          </p>
        </div>
        <Button variant="ghost" size="icon-xs" onClick={onClose} aria-label="Close details"><X className="h-3.5 w-3.5" /></Button>
      </div>

      <div className="min-h-0 flex-1 space-y-3 overflow-y-auto p-3">
        <InspectorCard title="Outcome">
          <MetricRow label="Action"><Badge variant={threat.action_taken === "block" ? "destructive" : "outline"} className="px-1.5 py-0 text-[9px]">{threat.action_taken}</Badge></MetricRow>
          <MetricRow label="Score (weighted)" mono>{(weighted * 100).toFixed(0)}%</MetricRow>
          <MetricRow label="Severity × Confidence" mono>{threat.score.toFixed(2)} × {threat.confidence.toFixed(2)}</MetricRow>
        </InspectorCard>

        <InspectorCard title="Matched pattern">
          <CopyRow value={threat.matched_pattern || "—"} />
        </InspectorCard>

        <InspectorCard title="Context">
          <MetricRow label="Detector" mono>{threat.detector_name}</MetricRow>
          <MetricRow label="Subtype" mono>{threat.threat_subtype || "—"}</MetricRow>
          <MetricRow label="Source" mono>{threat.source || "—"}</MetricRow>
          <MetricRow label="End user" mono>{endUserID || "—"}</MetricRow>
          <MetricRow label="IP address" mono copy>{ipAddress || "—"}</MetricRow>
          <MetricRow label="User-Agent" mono>{userAgent || "—"}</MetricRow>
          <MetricRow label="Trace ID" mono copy>{threat.trace_id}</MetricRow>
          <MetricRow label="Event ID" mono copy>{threat.id}</MetricRow>
        </InspectorCard>

        {details && Object.keys(details).length ? (
          <InspectorCard title="Detector metadata">
            {Object.entries(details).map(([key, value]) => <MetricRow key={key} label={key} mono>{value}</MetricRow>)}
          </InspectorCard>
        ) : null}

        {threat.matched_content ? (
          <InspectorCard title="Matched content (excerpt)">
            <CopyBlock value={threat.matched_content} />
          </InspectorCard>
        ) : null}

        <Link to="/threats/$id" params={{ id: threat.id }} className={buttonVariants({ variant: "outline", size: "sm" }) + " w-full justify-between text-[10px]"}>
          <span className="flex items-center gap-1.5"><FileJson className="h-3 w-3" /> Raw event</span>
          View JSON
        </Link>
      </div>

      <div className="space-y-2 border-t border-border-subtle p-3">
        <Link to="/overlays" className={buttonVariants({ size: "sm" }) + " w-full text-[11px]"}><ShieldPlus className="h-3.5 w-3.5" /> Create policy from event</Link>
        <Link to="/traces/$id" params={{ id: threat.trace_id }} className={buttonVariants({ variant: "outline", size: "sm" }) + " w-full text-[11px]"}>Open related trace <ExternalLink className="h-3 w-3" /></Link>
        <Button variant="outline" size="sm" className="w-full text-[11px]"><ListPlus className="h-3.5 w-3.5" /> Add to investigation</Button>
      </div>
    </aside>
  );
}

function InspectorCard({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <section className="rounded-md border border-border-subtle bg-surface-1 p-3">
      <h3 className="mb-2 text-[10px] font-medium text-muted-foreground">{title}</h3>
      <div className="space-y-1.5">{children}</div>
    </section>
  );
}

function MetricRow({
  label,
  children,
  mono = false,
  copy = false,
}: {
  label: string;
  children: React.ReactNode;
  mono?: boolean;
  copy?: boolean;
}) {
  const text = typeof children === "string" ? children : "";
  return (
    <div className="flex min-w-0 items-start gap-3 text-[10px] leading-4">
      <span className="w-[92px] flex-shrink-0 text-muted-foreground">{label}</span>
      <span className={cn("min-w-0 flex-1 break-all text-right text-foreground/90", mono && "font-mono tabular-nums")}>{children}</span>
      {copy && text ? <CopyButton value={text} /> : null}
    </div>
  );
}

function CopyRow({ value }: { value: string }) {
  return <div className="flex items-center gap-2"><code className="min-w-0 flex-1 truncate text-[10px] text-foreground/90">{value}</code><CopyButton value={value} /></div>;
}

function CopyBlock({ value }: { value: string }) {
  return (
    <div className="group relative rounded-md border border-border-subtle bg-background p-2 pr-8">
      <pre className="max-h-28 overflow-auto whitespace-pre-wrap break-words font-mono text-[10px] leading-4 text-foreground/85">{value}</pre>
      <CopyButton value={value} className="absolute right-1.5 top-1.5" />
    </div>
  );
}

function CopyButton({ value, className }: { value: string; className?: string }) {
  const [copied, setCopied] = useState(false);
  return (
    <button
      type="button"
      className={cn("flex h-5 w-5 flex-shrink-0 items-center justify-center rounded text-muted-foreground hover:bg-surface-2 hover:text-foreground", className)}
      aria-label="Copy"
      onClick={async () => {
        try {
          await navigator.clipboard.writeText(value);
          setCopied(true);
          window.setTimeout(() => setCopied(false), 1200);
        } catch {
          // Clipboard may be unavailable in embedded preview contexts.
        }
      }}
    >
      {copied ? <Check className="h-3 w-3" /> : <Copy className="h-3 w-3" />}
    </button>
  );
}
