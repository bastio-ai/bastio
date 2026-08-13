import { cn } from "@/lib/utils";

export type SummaryMetric = {
  value: string;
  label: string;
  sub?: string;
  tone?: "danger" | "warning" | "success";
};

export function WorkspaceSummaryStrip({ metrics }: { metrics: SummaryMetric[] }) {
  return (
    <div className={cn("grid flex-shrink-0 border-b border-border-subtle bg-surface-1/50", metrics.length === 3 ? "grid-cols-1 sm:grid-cols-3" : "grid-cols-2 md:grid-cols-4")}>
      {metrics.map((metric, index) => (
        <div key={`${metric.label}:${index}`} className={cn("min-w-0 px-4 py-3", index > 0 && "border-l border-border-subtle")}>
          <div className="truncate text-[14px] font-semibold tabular-nums">
            <span className={cn("font-mono", metric.tone === "danger" && "text-danger", metric.tone === "warning" && "text-warn", metric.tone === "success" && "text-success")}>{metric.value}</span>{" "}
            <span className="text-[12px] font-medium text-foreground/85">{metric.label}</span>
          </div>
          {metric.sub ? <div className="mt-0.5 truncate text-[10px] text-muted-foreground">{metric.sub}</div> : null}
        </div>
      ))}
    </div>
  );
}
