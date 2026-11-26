import type { ReactNode } from "react";
import { cn } from "@/lib/utils";

type Tone = "neutral" | "ok" | "blocked" | "warn" | "outline";

type Props = {
  tone?: Tone;
  className?: string;
  children: ReactNode;
};

const toneClass: Record<Tone, string> = {
  neutral: "text-text-secondary",
  ok:      "text-success",
  blocked: "text-danger",
  warn:    "text-warn",
  outline: "text-text-secondary",
};

/**
 * Small semantic state pill. Mono + tabular-nums.
 * Used in table cells for status, severity, action.
 */
export function Pill({ tone = "neutral", className, children }: Props) {
  return (
    <span
      className={cn(
        "inline-flex items-center py-[2px]",
        "font-mono text-[10px] font-normal tabular-nums",
        "whitespace-nowrap",
        toneClass[tone],
        className,
      )}
    >
      {children}
    </span>
  );
}
