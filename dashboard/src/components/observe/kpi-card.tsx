import type { ReactNode } from "react";
import { cn } from "@/lib/utils";
import { Sparkline } from "@/components/data/sparkline";

type DeltaTone = "success" | "danger" | "warn" | "neutral";
type OldTone = "default" | "danger" | "success" | "warn";
type SparkTone = "neutral" | "danger" | "success" | "warn";

type Props = {
  label: string;
  value: string | number;
  /** small unit rendered inline with the value (e.g. "ms") */
  unit?: string;
  /** New API — e.g. { value: "12.3% · 24h", direction: "up", tone: "success" } */
  delta?: {
    value: string;
    direction: "up" | "down";
    tone: DeltaTone;
  };
  /** Small metric line below the delta (e.g. "p50 98ms · p95 214ms · p99 412ms") */
  sub?: string;
  sparkline?: number[];
  sparklineTone?: SparkTone;
  /** DEPRECATED — kept for back-compat with pages that haven't migrated yet.
      Under the new design the value is always white; `tone` now only biases the sparkline color. */
  tone?: OldTone;
  /** DEPRECATED — no decorative icon box in the new design. Accepted but ignored. */
  icon?: ReactNode;
  className?: string;
};

const deltaClass: Record<DeltaTone, string> = {
  success: "text-success",
  danger:  "text-danger",
  warn:    "text-warn",
  neutral: "text-text-secondary",
};

/**
 * KPI card — variant B (Datadog density).
 * Label → big mono number → semantic delta → sub-metrics line → inline sparkline.
 */
export function KpiCard({
  label,
  value,
  unit,
  delta,
  sub,
  sparkline,
  sparklineTone,
  tone,
  className,
}: Props) {
  // Back-compat: map deprecated `tone` to `sparklineTone` when caller didn't pass one explicitly.
  const effectiveSparklineTone: SparkTone =
    sparklineTone ??
    (tone === "danger" ? "danger" : tone === "success" ? "success" : tone === "warn" ? "warn" : "neutral");

  return (
    <div className={cn("surface-card p-4 flex flex-col gap-0", className)}>
      <p className="text-[10px] font-medium uppercase tracking-[0.1em] text-text-muted">
        {label}
      </p>

      <p className="mt-3 font-mono text-[28px] leading-none font-medium tracking-[-0.01em] text-text-primary tabular-nums">
        {String(value)}
        {unit && <span className="ml-0.5 text-[18px] text-text-secondary">{unit}</span>}
      </p>

      {delta ? (
        <p className={cn("mt-1.5 font-mono text-[11px] tabular-nums", deltaClass[delta.tone])}>
          {delta.direction === "up" ? "↗" : "↘"} {delta.value}
        </p>
      ) : (
        <p className="mt-1.5 h-[11px]" aria-hidden />
      )}

      {sub && (
        <p className="mt-2 font-mono text-[10px] text-text-muted tabular-nums">
          {sub}
        </p>
      )}

      {sparkline && sparkline.length > 1 && (
        <div className="mt-auto pt-3">
          <Sparkline points={sparkline} tone={effectiveSparklineTone} height={36} />
        </div>
      )}
    </div>
  );
}
