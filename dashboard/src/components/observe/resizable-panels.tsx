import { useCallback, useEffect, useRef, useState } from "react";
import { cn } from "@/lib/utils";

type Props = {
  left: React.ReactNode;
  right: React.ReactNode;
  storageKey?: string;
  defaultLeftPct?: number;
  minLeftPx?: number;
  minRightPx?: number;
  className?: string;
};

// Two-column resizable split. Drag the 4px gutter between the panes.
// Persists the left-side width percentage under storageKey so the layout
// sticks between visits.
export function ResizablePanels({
  left,
  right,
  storageKey,
  defaultLeftPct = 40,
  minLeftPx = 280,
  minRightPx = 320,
  className,
}: Props) {
  const containerRef = useRef<HTMLDivElement | null>(null);
  const [leftPct, setLeftPct] = useState<number>(() => {
    if (!storageKey) return defaultLeftPct;
    if (typeof window === "undefined") return defaultLeftPct;
    const saved = window.localStorage.getItem(storageKey);
    const parsed = saved ? parseFloat(saved) : NaN;
    return Number.isFinite(parsed) ? parsed : defaultLeftPct;
  });
  const dragging = useRef(false);

  useEffect(() => {
    if (!storageKey) return;
    window.localStorage.setItem(storageKey, String(leftPct));
  }, [leftPct, storageKey]);

  const onDown = useCallback((e: React.PointerEvent<HTMLDivElement>) => {
    e.preventDefault();
    dragging.current = true;
    (e.target as HTMLDivElement).setPointerCapture?.(e.pointerId);
  }, []);

  const onMove = useCallback(
    (e: React.PointerEvent<HTMLDivElement>) => {
      if (!dragging.current || !containerRef.current) return;
      const rect = containerRef.current.getBoundingClientRect();
      const x = e.clientX - rect.left;
      const width = rect.width;
      const minLeft = (minLeftPx / width) * 100;
      const maxLeft = 100 - (minRightPx / width) * 100;
      const pct = Math.min(maxLeft, Math.max(minLeft, (x / width) * 100));
      setLeftPct(pct);
    },
    [minLeftPx, minRightPx],
  );

  const onUp = useCallback(() => {
    dragging.current = false;
  }, []);

  return (
    <div
      ref={containerRef}
      className={cn("flex h-full w-full overflow-hidden", className)}
    >
      <div
        className="min-h-0 min-w-0 overflow-auto"
        style={{ width: `${leftPct}%` }}
      >
        {left}
      </div>
      <div
        role="separator"
        aria-orientation="vertical"
        onPointerDown={onDown}
        onPointerMove={onMove}
        onPointerUp={onUp}
        onPointerCancel={onUp}
        className="w-1 cursor-col-resize bg-border/50 hover:bg-foreground/20 transition-colors"
      />
      <div
        className="min-h-0 min-w-0 flex-1 overflow-auto"
        style={{ width: `${100 - leftPct}%` }}
      >
        {right}
      </div>
    </div>
  );
}
