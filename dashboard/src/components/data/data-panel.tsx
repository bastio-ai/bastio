import type { ReactNode } from "react";
import { cn } from "@/lib/utils";

/**
 * Panel wrapper with a head (title, optional subtitle, optional trailing link)
 * and a table body. Paired with DataTable / DataRow / DataCell for dense
 * 28px-row tables on the Overview, Traces, etc.
 */
export function DataPanel({
  title,
  sub,
  action,
  children,
  className,
}: {
  title: string;
  sub?: string;
  action?: ReactNode;
  children: ReactNode;
  className?: string;
}) {
  return (
    <section className={cn("surface-card flex flex-col overflow-hidden", className)}>
      <header className="flex items-center gap-2 px-4 py-3 border-b border-border-subtle">
        <h2 className="text-[14px] font-semibold text-text-primary leading-none">{title}</h2>
        {sub && <span className="font-mono text-[11px] text-text-muted">· {sub}</span>}
        {action && <div className="ml-auto">{action}</div>}
      </header>
      <div className="flex-1 min-h-0 overflow-y-auto">{children}</div>
    </section>
  );
}

type Header = readonly [label: string] | readonly [label: string, align: "left" | "right"];

export function DataTable({
  headers,
  children,
}: {
  /** [label] or [label, alignment] — alignment "right" for numerics */
  headers: readonly Header[];
  children: ReactNode;
}) {
  return (
    <div className="overflow-x-auto">
    <table className="w-full border-collapse min-w-[640px]">
      <thead>
        <tr>
          {headers.map((h, i) => {
            const label = h[0];
            const align = h.length === 2 ? h[1] : "left";
            return (
              <th
                key={i}
                className={cn(
                  "sticky top-0 z-10 bg-background border-b border-border-subtle",
                  "px-3 py-2 font-medium text-[10px] uppercase tracking-[0.1em] text-text-muted",
                  "whitespace-nowrap",
                  align === "right" ? "text-right" : "text-left",
                )}
              >
                {label}
              </th>
            );
          })}
        </tr>
      </thead>
      <tbody>{children}</tbody>
    </table>
    </div>
  );
}

type Rail = "ok" | "blocked" | "warn" | "none";

const railColor: Record<Rail, string> = {
  ok:      "var(--success)",
  blocked: "var(--danger)",
  warn:    "var(--warn)",
  none:    "transparent",
};

/**
 * 28px-tall table row with a leading 2px status rail.
 */
export function DataRow({
  rail = "none",
  onClick,
  children,
}: {
  rail?: Rail;
  onClick?: () => void;
  children: ReactNode;
}) {
  return (
    <tr
      onClick={onClick}
      className={cn(
        "h-7 border-b border-border-subtle last:border-b-0 relative",
        "hover:bg-surface-2 transition-colors duration-150",
        onClick && "cursor-pointer",
      )}
      style={{
        boxShadow: rail !== "none" ? `inset 2px 0 0 0 ${railColor[rail]}` : undefined,
      }}
    >
      {children}
    </tr>
  );
}

/**
 * Table cell. Use `mono` for IDs/models, `num` for right-aligned numerics.
 */
export function DataCell({
  mono,
  num,
  strong,
  className,
  children,
  colSpan,
}: {
  mono?: boolean;
  num?: boolean;
  strong?: boolean;
  className?: string;
  children: ReactNode;
  colSpan?: number;
}) {
  return (
    <td
      colSpan={colSpan}
      className={cn(
        "px-3 align-middle text-[12px] whitespace-nowrap",
        (mono || num) && "font-mono tabular-nums",
        num ? "text-right text-text-primary" : "text-text-secondary",
        strong && "text-text-primary",
        className,
      )}
    >
      {children}
    </td>
  );
}
