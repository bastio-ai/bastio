import { cn } from "@/lib/utils";

type Props = {
  /** ms per bucket, chronological oldest → newest */
  series: number[];
  /** optional reference line in ms (e.g. p95 or SLO target) */
  threshold?: number;
  height?: number;
  className?: string;
};

/**
 * Line chart of latency over time. Grayscale by default; if a threshold is
 * provided it renders as a dashed muted-amber horizontal reference line.
 */
export function LatencyChart({ series, threshold, height = 160, className }: Props) {
  const n = series.length;
  const nonZero = series.some((v) => v > 0);

  if (n < 2 || !nonZero) {
    return (
      <div
        className={cn(
          "flex items-center justify-center text-[11px] text-text-muted",
          className,
        )}
        style={{ height }}
      >
        No latency data yet.
      </div>
    );
  }

  const max = Math.max(...series, threshold ?? 0, 1);
  const w = 800;
  const h = height;
  const pad = 8;
  const step = w / (n - 1);

  const coords = series.map(
    (v, i) => [i * step, h - pad - (v / max) * (h - pad * 2)] as const,
  );
  const line = coords
    .map(([x, y], i) => `${i === 0 ? "M" : "L"}${x.toFixed(2)},${y.toFixed(2)}`)
    .join(" ");
  const area = `${line} L${w},${h} L0,${h} Z`;

  const thresholdY = threshold
    ? h - pad - (threshold / max) * (h - pad * 2)
    : null;

  return (
    <svg
      viewBox={`0 0 ${w} ${h}`}
      preserveAspectRatio="none"
      className={cn("w-full block", className)}
      style={{ height }}
      aria-hidden
    >
      <path d={area} fill="rgba(250, 250, 250, 0.06)" stroke="none" />
      <path
        d={line}
        fill="none"
        stroke="var(--text-secondary)"
        strokeWidth={1}
        strokeLinejoin="round"
      />
      {thresholdY !== null && (
        <line
          x1={0}
          y1={thresholdY}
          x2={w}
          y2={thresholdY}
          stroke="var(--warn)"
          strokeWidth={0.75}
          strokeDasharray="3 3"
          strokeOpacity={0.6}
        />
      )}
    </svg>
  );
}
