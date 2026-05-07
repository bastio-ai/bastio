import { cn } from "@/lib/utils";

/**
 * Shimmering placeholder block. Uses dedicated --skeleton token so it reads
 * correctly in both dark and light themes, and respects prefers-reduced-motion
 * (the animate-pulse keyframe is suppressed globally via index.css).
 */
export function Skeleton({ className }: { className?: string }) {
  return (
    <div
      className={cn("animate-pulse rounded-md", className)}
      style={{ background: "var(--skeleton)" }}
      aria-hidden
    />
  );
}

/**
 * Horizontal bars sized to mimic a dense 28–32px table row.
 * Use inside a list or table body to preserve vertical rhythm while loading.
 */
export function SkeletonRows({ count = 5 }: { count?: number }) {
  return (
    <div className="space-y-2 p-4">
      {Array.from({ length: count }).map((_, i) => (
        <Skeleton key={i} className="h-7 w-full" />
      ))}
    </div>
  );
}
