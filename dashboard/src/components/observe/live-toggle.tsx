import { cn } from "@/lib/utils";

type Props = {
  live: boolean;
  onToggle: () => void;
};

/**
 * "Live" pill used on observe headers. On = muted green dot pulsing,
 * off = static gray. Monochrome except for the semantic live dot.
 */
export function LiveToggle({ live, onToggle }: Props) {
  return (
    <button
      type="button"
      onClick={onToggle}
      className={cn(
        "inline-flex items-center gap-2 rounded-full border px-3 py-1 font-mono text-[11px] tabular-nums transition-colors",
        live
          ? "border-success-border bg-success-bg text-success"
          : "border-border-subtle text-text-muted hover:border-border-default hover:text-text-primary",
      )}
      aria-pressed={live}
    >
      <span
        className={cn("inline-block rounded-full")}
        style={{
          width: 6,
          height: 6,
          background: live ? "var(--success)" : "var(--text-muted)",
          animation: live ? "bastio-pulse-success 2s ease-in-out infinite" : undefined,
        }}
        aria-hidden
      />
      {live ? "Live" : "Paused"}
    </button>
  );
}
