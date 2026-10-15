import { ShieldAlert, ShieldCheck, Info } from "lucide-react";
import type { TraceThreatDetection } from "@/api/client";
import { Badge } from "@/components/ui/badge";
import { cn, weightedThreatScore } from "@/lib/utils";

import { securityDecision } from "./security-decision";

type Props = {
  threats: TraceThreatDetection[];
  onSelect?: (threat: TraceThreatDetection) => void;
  selectedThreatId?: string | null;
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
export function ThreatCascade({ threats, onSelect, selectedThreatId }: Props) {
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
      {threats.map((t) => {
        const decision = securityDecision(t.action_taken);
        const Icon = decision.tone === "neutral" ? Info : decision.tone === "success" ? ShieldCheck : ShieldAlert;
        return (
          <button
            key={t.id}
            type="button"
            onClick={() => onSelect?.(t)}
            aria-pressed={selectedThreatId === t.id}
            className={cn(
              "grid w-full grid-cols-[auto_1fr_auto] items-start gap-3 border-l-2 px-3 py-2.5 text-left transition-colors hover:bg-surface-2",
              selectedThreatId === t.id
                ? "border-l-primary bg-surface-2"
                : "border-l-transparent",
            )}
          >
            <Icon className={cn("mt-0.5 h-4 w-4", decision.tone === "danger" ? "text-danger" : decision.tone === "warning" ? "text-warn" : decision.tone === "success" ? "text-success" : "text-muted-foreground")} />
            <div className="min-w-0 space-y-1">
              <div className="flex flex-wrap items-center gap-2">
                <span className="font-mono text-xs font-semibold">{t.threat_type}</span>
                <Badge
                  className={`text-[10px] px-1.5 py-0 ${decision.tone === "neutral" ? "bg-muted text-muted-foreground" : severityClass[t.severity] ?? "bg-muted"}`}
                >
                  {t.severity}
                </Badge>
                <span
                  className="text-[10px] text-muted-foreground tabular-nums"
                  title={`${(t.score ?? 0).toFixed(2)} × ${(t.confidence ?? 0).toFixed(2)} (severity × confidence) — what the threshold compares against`}
                >
                  {t.detector_name} ·{" "}
                  {(
                    weightedThreatScore(t.score, t.confidence, t.weighted_score) *
                    100
                  ).toFixed(0)}
                  % weighted
                </span>
              </div>
              <p className="text-[11px] text-muted-foreground">{decision.explanation}</p>
              {t.threat_subtype || t.source ? <p className="font-mono text-[11px] text-muted-foreground">{[t.source, t.threat_subtype].filter(Boolean).join(" · ")}</p> : null}
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
            <span className="max-w-32 text-right text-[10px] font-mono text-muted-foreground tabular-nums">
              {decision.label}
            </span>
          </button>
        );
      })}
    </div>
  );
}
