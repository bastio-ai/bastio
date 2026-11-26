import { cn } from "@/lib/utils";

export type SummaryMetric = {
  value: string;
  label: string;
  sub?: string;
  tone?: "danger" | "warning" | "success";
};

export function WorkspaceSummaryStrip({ metrics }: { metrics: SummaryMetric[] }) {
  return (
    <section aria-label="Summary metrics" className={cn("grid flex-shrink-0 gap-y-5 border-b border-border-subtle bg-background px-5 py-6", metrics.length === 5 ? "grid-cols-2 lg:grid-cols-5" : metrics.length === 3 ? "grid-cols-1 sm:grid-cols-3" : "grid-cols-2 md:grid-cols-4")}>
      {metrics.map((metric, index) => (
        <div key={`${metric.label}:${index}`} className={cn(
          "min-w-0 px-4 first:pl-0",
          index > 0 && "border-l border-border-subtle",
          index > 0 && metrics.length === 3 && "border-l-0 pl-0 sm:border-l sm:pl-4",
          index > 0 && index % 2 === 0 && metrics.length === 5 && "border-l-0 pl-0 lg:border-l lg:pl-4",
          index > 0 && index % 2 === 0 && metrics.length !== 3 && metrics.length !== 5 && "border-l-0 pl-0 md:border-l md:pl-4",
        )}>
          <p className="mb-2 text-[11px] text-text-secondary">{metric.label}</p>
          <p className={cn("truncate font-mono text-[23px] font-normal tracking-tight tabular-nums", metric.tone === "danger" && "text-danger", metric.tone === "warning" && "text-warn", metric.tone === "success" && "text-success")}>{metric.value}</p>
          {metric.sub ? <p className="mt-2 truncate text-[10px] text-muted-foreground" title={metric.sub}>{metric.sub}</p> : null}
        </div>
      ))}
    </section>
  );
}
