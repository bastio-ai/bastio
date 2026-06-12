import { clsx, type ClassValue } from "clsx"
import { twMerge } from "tailwind-merge"

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs))
}

export function formatNumber(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`;
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}K`;
  return n.toString();
}

export function formatDuration(ms: number): string {
  if (!Number.isFinite(ms) || ms < 0) return "—";
  if (ms < 10) return `${ms.toFixed(1)}ms`;
  if (ms < 1000) return `${Math.round(ms)}ms`;
  if (ms < 60_000) return `${(ms / 1000).toFixed(2)}s`;
  const mins = Math.floor(ms / 60_000);
  const secs = Math.round((ms % 60_000) / 1000);
  return `${mins}m ${secs}s`;
}

export function formatCost(cents: number): string {
  if (cents < 1) return `$${cents.toFixed(4)}`;
  return `$${(cents / 100).toFixed(2)}`;
}

// weightedThreatScore returns the weighted score (score × confidence,
// clamped to [0, 1]) — the value the engine's threshold check actually
// compares against. Prefers the server-provided weighted_score; rows
// persisted before that field existed report 0, so fall back to
// deriving it from confidence, and finally to the raw severity score
// so historical traces never render as 0%.
export function weightedThreatScore(
  score: number | undefined,
  confidence: number | undefined,
  weighted?: number,
): number {
  const clamp = (n: number) => Math.min(Math.max(n, 0), 1);
  if (typeof weighted === "number" && weighted > 0) return clamp(weighted);
  const s = typeof score === "number" ? score : 0;
  if (typeof confidence === "number" && confidence > 0) {
    return clamp(s * confidence);
  }
  return clamp(s);
}
