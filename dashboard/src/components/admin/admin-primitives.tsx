import type { ReactNode } from "react";
import { AlertTriangle, CheckCircle2, Info } from "lucide-react";
import { cn } from "@/lib/utils";

export function AdminSummaryStrip({
  items,
}: {
  items: Array<{ label: string; value: ReactNode; detail?: ReactNode; tone?: "default" | "success" | "warning" | "danger" }>;
}) {
  return (
    <section className="mb-6 overflow-hidden border-y border-border-subtle bg-background">
      <div className="grid divide-y divide-border/60 sm:grid-cols-2 sm:divide-x sm:divide-y-0 lg:grid-cols-4">
        {items.map((item) => (
          <div key={item.label} className="min-w-0 px-5 py-4">
            <p className="text-[11px] font-medium text-muted-foreground">
              {item.label}
            </p>
            <div
              className={cn(
                "mt-1.5 font-mono text-[23px] font-normal tracking-tight text-foreground",
                item.tone === "success" && "text-success",
                item.tone === "warning" && "text-warn",
                item.tone === "danger" && "text-destructive",
              )}
            >
              {item.value}
            </div>
            {item.detail ? <p className="mt-1 truncate text-xs text-muted-foreground">{item.detail}</p> : null}
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
        "flex items-start gap-3 rounded-md border px-4 py-3",
        tone === "info" && "border-border-subtle bg-surface-1/60",
        tone === "success" && "border-success-border bg-success-bg",
        tone === "warning" && "border-warn-border bg-warn-bg",
        className,
      )}
    >
      <Icon
        aria-hidden="true"
        className={cn(
          "mt-0.5 size-4 shrink-0",
          tone === "info" && "text-muted-foreground",
          tone === "success" && "text-success",
          tone === "warning" && "text-warn",
        )}
      />
      <div className="min-w-0 flex-1">
        <p className="text-[12px] font-medium text-foreground">{title}</p>
        <div className="mt-1 text-xs leading-relaxed text-muted-foreground">{children}</div>
      </div>
      {action ? <div className="shrink-0">{action}</div> : null}
    </div>
  );
}

export function FieldLabel({
  children,
  optional,
  htmlFor,
}: {
  children: ReactNode;
  optional?: boolean;
  htmlFor?: string;
}) {
  return (
    <div className="mb-1.5 flex items-center justify-between gap-3">
      <label htmlFor={htmlFor} className="text-xs font-medium text-foreground">{children}</label>
      {optional ? <span className="text-[11px] text-muted-foreground">Optional</span> : null}
    </div>
  );
}

export function MonoValue({ children, className }: { children: ReactNode; className?: string }) {
  return (
    <code className={cn("rounded-sm border border-border-default bg-muted/50 px-2 py-1 font-mono text-xs text-foreground", className)}>
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
  bordered = true,
  className,
}: {
  eyebrow: string;
  title: ReactNode;
  description?: ReactNode;
  badge?: ReactNode;
  actions?: ReactNode;
  bordered?: boolean;
  className?: string;
}) {
  return (
    <header
      className={cn(
        "mb-6 flex flex-col gap-4 sm:flex-row sm:items-end sm:justify-between",
        bordered && "border-b border-border-subtle pb-5",
        className,
      )}
    >
      <div className="min-w-0">
        <p className="text-[11px] text-muted-foreground">{eyebrow}</p>
        <div className="mt-1.5 flex flex-wrap items-center gap-2.5">
          <h1 className="min-w-0 text-[25px] font-medium tracking-[-0.025em] text-foreground">{title}</h1>
          {badge}
        </div>
        {description ? <div className="mt-1.5 max-w-3xl text-[12px] leading-relaxed text-muted-foreground">{description}</div> : null}
      </div>
      {actions ? <div className="flex shrink-0 flex-wrap items-center gap-2">{actions}</div> : null}
    </header>
  );
}

export function AdminUnderlineNav<T extends string>({
  ariaLabel,
  items,
  value,
  onChange,
  className,
}: {
  ariaLabel: string;
  items: ReadonlyArray<{ id: T; label: ReactNode; count?: ReactNode }>;
  value: T;
  onChange: (next: T) => void;
  className?: string;
}) {
  return (
    <nav
      aria-label={ariaLabel}
      className={cn("mb-6 flex gap-5 overflow-x-auto border-b border-border-subtle", className)}
    >
      {items.map((item) => {
        const active = item.id === value;
        return (
          <button
            key={item.id}
            type="button"
            aria-current={active ? "page" : undefined}
            onClick={() => onChange(item.id)}
            className={cn(
              "inline-flex shrink-0 cursor-pointer items-center gap-1.5 border-b-2 px-0.5 pb-3 text-[12px] transition-colors hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
              active
                ? "border-foreground/70 text-foreground"
                : "border-transparent text-muted-foreground",
            )}
          >
            <span>{item.label}</span>
            {item.count !== undefined ? (
              <span className="font-mono text-[11px] tabular-nums text-muted-foreground">
                {item.count}
              </span>
            ) : null}
          </button>
        );
      })}
    </nav>
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
    <section className={cn("overflow-hidden border-t border-border-subtle bg-background", className)}>
      {title || description || action ? (
        <div className="flex items-start justify-between gap-4 border-b border-border-subtle py-4">
          <div className="min-w-0">
            {title ? <h2 className="text-[13px] font-medium tracking-tight text-foreground">{title}</h2> : null}
            {description ? <div className="mt-1 text-xs leading-relaxed text-muted-foreground">{description}</div> : null}
          </div>
          {action ? <div className="shrink-0">{action}</div> : null}
        </div>
      ) : null}
      <div className={cn("py-4", contentClassName)}>{children}</div>
    </section>
  );
}
