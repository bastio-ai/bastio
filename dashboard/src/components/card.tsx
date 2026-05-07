import type { ReactNode } from "react";
import { cn } from "@/lib/utils";

export { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
export { Badge } from "@/components/ui/badge";

/**
 * Page header — title + optional description + optional badge slot + optional action.
 * Matches the Overview inline header shape so every page reads the same.
 */
export function PageHeader({
  title,
  description,
  action,
  badge,
  className,
}: {
  title: string;
  description?: string;
  action?: ReactNode;
  badge?: ReactNode;
  className?: string;
}) {
  return (
    <header className={cn("flex items-start gap-3 mb-5", className)}>
      <div className="flex-1 min-w-0">
        <div className="flex items-center gap-3">
          <h1 className="text-[22px] font-semibold tracking-[-0.015em] text-text-primary leading-tight">
            {title}
          </h1>
          {badge}
        </div>
        {description && (
          <p className="text-[12px] text-text-muted mt-1">{description}</p>
        )}
      </div>
      {action && <div className="flex-shrink-0">{action}</div>}
    </header>
  );
}

/**
 * Empty state — monochrome icon on surface-2, title + description + optional action.
 * Used inside DataPanels and on zero-result list pages.
 */
export function EmptyState({
  icon,
  title,
  description,
  action,
  className,
}: {
  icon: ReactNode;
  title: string;
  description: string;
  action?: ReactNode;
  className?: string;
}) {
  return (
    <div className={cn("flex flex-col items-center justify-center py-14 px-6 text-center", className)}>
      <div className="flex h-11 w-11 items-center justify-center rounded-md bg-surface-2 text-text-muted mb-4 border border-border-subtle">
        {icon}
      </div>
      <h3 className="text-[13px] font-medium text-text-primary mb-1">{title}</h3>
      <p className="text-[12px] text-text-muted max-w-[360px]">{description}</p>
      {action && <div className="mt-4">{action}</div>}
    </div>
  );
}

/**
 * Section heading inside a page — smaller than PageHeader.
 */
export function SectionHeader({
  title,
  description,
  action,
  className,
}: {
  title: string;
  description?: string;
  action?: ReactNode;
  className?: string;
}) {
  return (
    <div className={cn("flex items-center justify-between mb-3", className)}>
      <div className="min-w-0">
        <h2 className="text-[14px] font-semibold tracking-tight text-text-primary">{title}</h2>
        {description && (
          <p className="text-[11px] text-text-muted mt-0.5">{description}</p>
        )}
      </div>
      {action}
    </div>
  );
}
