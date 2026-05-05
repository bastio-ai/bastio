import { cn } from "@/lib/utils";

type Tone = "success" | "danger" | "warn" | "neutral";

type Props = {
  tone?: Tone;
  pulse?: boolean;
  size?: 4 | 5 | 6 | 8;
  className?: string;
};

const color: Record<Tone, string> = {
  success: "var(--success)",
  danger:  "var(--danger)",
  warn:    "var(--warn)",
  neutral: "var(--text-muted)",
};

const pulseAnim: Record<Tone, string | undefined> = {
  success: "bastio-pulse-success 2s ease-in-out infinite",
  danger:  "bastio-pulse-danger 2s ease-in-out infinite",
  warn:    "bastio-pulse-warn 2s ease-in-out infinite",
  neutral: undefined,
};

/** Tiny colored dot, optionally pulsing. Used for live/healthy indicators. */
export function StatusDot({ tone = "success", pulse = false, size = 6, className }: Props) {
  return (
    <span
      className={cn("inline-block rounded-full", className)}
      style={{
        width: size,
        height: size,
        background: color[tone],
        animation: pulse ? pulseAnim[tone] : undefined,
      }}
      aria-hidden
    />
  );
}
