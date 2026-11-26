import { cn } from "@/lib/utils";

const outcomes: Record<string, { label: string; tone: string }> = {
  ok: { label: "Success", tone: "text-text-secondary" },
  blocked: { label: "Blocked", tone: "text-warn" },
  error: { label: "Error", tone: "text-danger" },
  rate_limited: { label: "Rate limited", tone: "text-warn" },
};

/** Request status is independent of the detector's signals and policy action. */
export function TraceStatus({ status }: { status: string }) {
  const outcome = outcomes[status] ?? {
    label: status || "Unknown",
    tone: "text-muted-foreground",
  };
  return (
    <span
      className={cn(
        "inline-flex items-center gap-1.5 whitespace-nowrap text-[11px]",
        outcome.tone,
      )}
    >
      <span
        aria-hidden="true"
        className={cn(
          "h-1 w-1 rounded-full bg-current",
          status === "ok" && "bg-success",
        )}
      />
      {outcome.label}
    </span>
  );
}
