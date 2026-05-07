import { cn } from "@/lib/utils";

type Props = {
  points: number[];
  tone?: "neutral" | "danger" | "success" | "warn";
  className?: string;
  /** height in px — default 36 */
  height?: number;
  /** fill area under the line with a subtle tint */
  filled?: boolean;
};

/**
 * Inline SVG sparkline. Renders a 200-wide viewBox that stretches to the
 * container width via preserveAspectRatio=none. Grayscale by default; pass
 * tone="danger" for threat metrics only.
 */
export function Sparkline({ points, tone = "neutral", className, height = 36, filled = true }: Props) {
  if (points.length < 2) return null;

  const max = Math.max(...points);
  const min = Math.min(...points);
  const range = max - min || 1;
  const pad = 2;
  const w = 200;
  const h = height;
  const step = w / (points.length - 1);

  const coords = points.map((p, i) => {
    const x = i * step;
    const y = h - pad - ((p - min) / range) * (h - pad * 2);
    return [x, y] as const;
  });

  const line = coords.map(([x, y], i) => `${i === 0 ? "M" : "L"}${x.toFixed(2)},${y.toFixed(2)}`).join(" ");
  const area = `${line} L${w},${h} L0,${h} Z`;

  const stroke = {
    neutral: "var(--text-secondary)",
    danger:  "var(--danger)",
    success: "var(--success)",
    warn:    "var(--warn)",
  }[tone];

  const fill = {
    neutral: "rgba(250, 250, 250, 0.06)",
    danger:  "rgba(248, 113, 113, 0.08)",
    success: "rgba(74, 222, 128, 0.08)",
    warn:    "rgba(251, 191, 36, 0.08)",
  }[tone];

  return (
    <svg
      viewBox={`0 0 ${w} ${h}`}
      preserveAspectRatio="none"
      className={cn("w-full", className)}
      style={{ height }}
      aria-hidden
    >
      {filled && <path d={area} fill={fill} stroke="none" />}
      <path d={line} fill="none" stroke={stroke} strokeWidth={1} strokeLinejoin="round" />
    </svg>
  );
}
