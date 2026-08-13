import type { ReactNode } from "react";
import { AlertTriangle, CheckCircle2, Info } from "lucide-react";
import { cn } from "@/lib/utils";

export function AdminSummaryStrip({
  items,
}: {
  items: Array<{ label: string; value: ReactNode; detail?: ReactNode; tone?: "default" | "success" | "warning" | "danger" }>;
}) {
  return (
    <section className="mb-5 overflow-hidden rounded-xl border border-border/70 bg-card">
      <div className="grid divide-y divide-border/60 sm:grid-cols-2 sm:divide-x sm:divide-y-0 lg:grid-cols-4">
        {items.map((item) => (
          <div key={item.label} className="min-w-0 px-4 py-3.5">
            <p className="text-[10px] font-medium uppercase tracking-[0.12em] text-muted-foreground">
              {item.label}
            </p>
            <div
              className={cn(
                "mt-1 font-mono text-[18px] font-medium tracking-tight text-foreground",
                item.tone === "success" && "text-success",
                item.tone === "warning" && "text-warn",
                item.tone === "danger" && "text-destructive",
              )}
            >
              {item.value}
            </div>
            {item.detail ? <p className="mt-0.5 truncate text-[11px] text-muted-foreground">{item.detail}</p> : null}
          </div>
        ))}
      </div>
    </section>
  );
}

export function SecurityNotice({
  title,
  children,
  tone = "info",
  action,
  className,
}: {
  title: string;
  children: ReactNode;
  tone?: "info" | "success" | "warning";
  action?: ReactNode;
  className?: string;
}) {
  const Icon = tone === "warning" ? AlertTriangle : tone === "success" ? CheckCircle2 : Info;

  return (
    <div
      className={cn(
        "flex items-start gap-3 rounded-xl border px-4 py-3",
        tone === "info" && "border-border/70 bg-muted/30",
        tone === "success" && "border-success-border bg-success-bg",
        tone === "warning" && "border-warn-border bg-warn-bg",
        className,
      )}
    >
      <Icon
        className={cn(
          "mt-0.5 size-4 shrink-0",
          tone === "info" && "text-muted-foreground",
          tone === "success" && "text-success",
          tone === "warning" && "text-warn",
        )}
      />
      <div className="min-w-0 flex-1">
        <p className="text-[12px] font-medium text-foreground">{title}</p>
        <div className="mt-0.5 text-[11px] leading-relaxed text-muted-foreground">{children}</div>
      </div>
      {action ? <div className="shrink-0">{action}</div> : null}
    </div>
  );
}

export function FieldLabel({
  children,
  optional,
}: {
  children: ReactNode;
  optional?: boolean;
}) {
  return (
    <div className="mb-1.5 flex items-center justify-between gap-3">
      <label className="text-[11px] font-medium text-foreground">{children}</label>
      {optional ? <span className="text-[10px] text-muted-foreground">Optional</span> : null}
    </div>
  );
}

export function MonoValue({ children, className }: { children: ReactNode; className?: string }) {
  return (
    <code className={cn("rounded-md border border-border/60 bg-muted/40 px-2 py-1 font-mono text-[11px] text-foreground", className)}>
      {children}
    </code>
  );
}

export function AdminPageHeader({
  eyebrow,
  title,
  description,
  badge,
  actions,
  className,
}: {
  eyebrow: string;
  title: ReactNode;
  description?: ReactNode;
  badge?: ReactNode;
  actions?: ReactNode;
  className?: string;
}) {
  return (
    <header className={cn("mb-5 flex flex-col gap-4 border-b border-border/60 pb-4 sm:flex-row sm:items-end sm:justify-between", className)}>
      <div className="min-w-0">
        <p className="text-[10px] font-medium uppercase tracking-[0.16em] text-muted-foreground">{eyebrow}</p>
        <div className="mt-1.5 flex flex-wrap items-center gap-2.5">
          <h1 className="min-w-0 text-[24px] font-bold tracking-tight text-foreground">{title}</h1>
          {badge}
        </div>
        {description ? <div className="mt-1 max-w-3xl text-[12px] leading-relaxed text-muted-foreground">{description}</div> : null}
      </div>
      {actions ? <div className="flex shrink-0 flex-wrap items-center gap-2">{actions}</div> : null}
    </header>
  );
}

export function AdminPanel({
  title,
  description,
  action,
  children,
  className,
  contentClassName,
}: {
  title?: ReactNode;
  description?: ReactNode;
  action?: ReactNode;
  children: ReactNode;
  className?: string;
  contentClassName?: string;
}) {
  return (
    <section className={cn("overflow-hidden rounded-xl border border-border/70 bg-card", className)}>
      {title || description || action ? (
        <div className="flex items-start justify-between gap-4 border-b border-border/60 px-4 py-3">
          <div className="min-w-0">
            {title ? <h2 className="text-[13px] font-bold tracking-tight text-foreground">{title}</h2> : null}
            {description ? <div className="mt-0.5 text-[11px] leading-relaxed text-muted-foreground">{description}</div> : null}
          </div>
          {action ? <div className="shrink-0">{action}</div> : null}
        </div>
      ) : null}
      <div className={cn("p-4", contentClassName)}>{children}</div>
    </section>
  );
}
