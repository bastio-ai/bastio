import { ShieldAlert, ShieldCheck } from "lucide-react";
import type { TraceThreatDetection } from "@/api/client";
import { Badge } from "@/components/ui/badge";

type Props = {
  threats: TraceThreatDetection[];
  onSelect?: (threat: TraceThreatDetection) => void;
};

const severityClass: Record<string, string> = {
  critical: "bg-destructive text-destructive-foreground",
  high: "bg-destructive/80 text-destructive-foreground",
  medium: "bg-warn text-background",
  low: "bg-warn-bg text-warn",
};

// Vertical list of per-detector threat detections, already server-sorted by
// severity then score. Clicking a row lets the parent focus the offending
// span (when it can be correlated by detector payload).
export function ThreatCascade({ threats, onSelect }: Props) {
  if (!threats.length) {
    return (
      <div className="flex flex-col items-center gap-2 py-10 text-muted-foreground">
        <ShieldCheck className="h-5 w-5" />
        <p className="text-xs">No detectors fired for this trace.</p>
      </div>
    );
  }

  return (
    <div className="divide-y divide-border/30">
      {threats.map((t) => (
        <button
          key={t.id}
          type="button"
          onClick={() => onSelect?.(t)}
          className="grid w-full grid-cols-[auto_1fr_auto] items-start gap-3 px-3 py-2 text-left hover:bg-muted/30"
        >
          <ShieldAlert className="mt-0.5 h-4 w-4 text-destructive" />
          <div className="min-w-0 space-y-1">
            <div className="flex items-center gap-2">
              <span className="font-mono text-xs font-semibold">{t.threat_type}</span>
              <Badge
                className={`text-[10px] px-1.5 py-0 ${severityClass[t.severity] ?? "bg-muted"}`}
              >
                {t.severity}
              </Badge>
              <span className="text-[10px] text-muted-foreground">
                {t.detector_name} · {((t.confidence ?? 0) * 100).toFixed(0)}%
              </span>
            </div>
            {t.matched_pattern ? (
              <p className="truncate font-mono text-[11px] text-muted-foreground">
                pattern: {t.matched_pattern}
              </p>
            ) : null}
            {t.matched_content ? (
              <p className="line-clamp-2 whitespace-pre-wrap break-words font-mono text-[11px] text-foreground/80">
                {t.matched_content}
              </p>
            ) : null}
          </div>
          <span className="text-[10px] font-mono text-muted-foreground tabular-nums">
            {t.action_taken}
          </span>
        </button>
      ))}
    </div>
  );
}
