import type { ReactNode } from "react";
import { cn } from "@/lib/utils";

type Tone = "neutral" | "ok" | "blocked" | "warn" | "outline";

type Props = {
  tone?: Tone;
  className?: string;
  children: ReactNode;
};

const toneClass: Record<Tone, string> = {
  neutral: "bg-[color:rgba(255,255,255,0.06)] text-text-secondary",
  ok:      "bg-success-bg text-success",
  blocked: "bg-danger-bg text-danger",
  warn:    "bg-warn-bg text-warn",
  outline: "border border-border-default text-text-secondary",
};

/**
 * Small semantic state pill. Mono + tabular-nums.
 * Used in table cells for status, severity, action.
 */
export function Pill({ tone = "neutral", className, children }: Props) {
  return (
    <span
      className={cn(
        "inline-flex items-center rounded-[3px] px-1.5 py-[2px]",
        "font-mono text-[10px] font-medium uppercase tracking-[0.04em] tabular-nums",
        "whitespace-nowrap",
        toneClass[tone],
        className,
      )}
    >
      {children}
    </span>
  );
}
