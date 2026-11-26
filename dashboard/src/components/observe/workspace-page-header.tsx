import type { ReactNode } from "react";

export function WorkspacePageHeader({
  title,
  description,
  actions,
}: {
  title: string;
  description: string;
  actions?: ReactNode;
}) {
  return (
    <header className="flex flex-shrink-0 flex-wrap items-center justify-between gap-3 px-5 pb-1 pt-6">
      <div className="min-w-0">
        <h1 className="text-[25px] font-medium leading-tight tracking-tight">
          {title}
        </h1>
        <p className="mt-1.5 text-xs text-muted-foreground">{description}</p>
      </div>
      {actions ? (
        <div className="flex items-center gap-2">{actions}</div>
      ) : null}
    </header>
  );
}
