import { cn } from "@/lib/utils";

type Props = {
  /** total request counts per bucket */
  total: number[];
  /** blocked request counts per bucket (same length as total) */
  blocked: number[];
  className?: string;
  height?: number;
};

/**
 * Full-width area chart that sits between the KPI row and data panels
 * on the Overview page. Total volume in grayscale, blocked overlaid
 * in muted red. Tokens only — no brand color.
 */
export function RequestVolumeChart({
  total,
  blocked,
  className,
  height = 96,
}: Props) {
  const n = total.length;
  if (n < 2) return null;

  const max = Math.max(1, ...total);
  const w = 800;
  const h = height;
  const pad = 6;
  const step = w / (n - 1);

  const project = (series: number[]) =>
    series.map((v, i) => {
      const x = i * step;
      const y = h - pad - (v / max) * (h - pad * 2);
      return [x, y] as const;
    });

  const toLine = (coords: readonly (readonly [number, number])[]) =>
    coords.map(([x, y], i) => `${i === 0 ? "M" : "L"}${x.toFixed(2)},${y.toFixed(2)}`).join(" ");
  const toArea = (line: string) => `${line} L${w},${h} L0,${h} Z`;

  const totalCoords = project(total);
  const blockedCoords = project(blocked);
  const totalLine = toLine(totalCoords);
  const blockedLine = toLine(blockedCoords);

  return (
    <section className={cn("surface-card px-4 py-3", className)}>
      <header className="flex items-baseline gap-3 mb-2">
        <p className="text-[10px] font-medium uppercase tracking-[0.1em] text-text-muted">
          Request volume · last 24h
        </p>
        <p className="font-mono text-[11px] text-text-muted tabular-nums">req/s</p>
        <div className="ml-auto flex gap-4 font-mono text-[10px] text-text-muted">
          <span className="inline-flex items-center gap-1.5">
            <span className="inline-block w-2 h-2 rounded-[1px] bg-text-secondary" />
            total
          </span>
          <span className="inline-flex items-center gap-1.5">
            <span className="inline-block w-2 h-2 rounded-[1px] bg-danger" />
            blocked
          </span>
        </div>
      </header>

      <svg
        viewBox={`0 0 ${w} ${h}`}
        preserveAspectRatio="none"
        className="w-full block"
        style={{ height }}
        aria-hidden
      >
        {/* total fill */}
        <path d={toArea(totalLine)} fill="rgba(250, 250, 250, 0.06)" stroke="none" />
        {/* total line */}
        <path d={totalLine} fill="none" stroke="var(--text-secondary)" strokeWidth={1} strokeLinejoin="round" />
        {/* blocked fill */}
        <path d={toArea(blockedLine)} fill="rgba(248, 113, 113, 0.12)" stroke="none" />
        {/* blocked line */}
        <path d={blockedLine} fill="none" stroke="var(--danger)" strokeWidth={1} strokeLinejoin="round" />
      </svg>
    </section>
  );
}
