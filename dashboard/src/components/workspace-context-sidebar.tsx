import { useState } from "react";
import { ChevronDown, ChevronLeft } from "lucide-react";

import type { WorkspaceSidebarConfig } from "@/components/workspace-sidebar-context";
import { cn } from "@/lib/utils";

export function WorkspaceContextSidebar({
  config,
  onBack,
  onCollapse,
}: {
  config: WorkspaceSidebarConfig;
  onBack: () => void;
  onCollapse: () => void;
}) {
  const [filtersOpen, setFiltersOpen] = useState(true);
  const ActiveIcon = config.activeIcon;
  return (
    <div className="flex h-full min-h-0 flex-col">
      <div className="border-b border-border-subtle px-3 py-3">
        <button
          type="button"
          onClick={onBack}
          className="mb-3 flex h-7 items-center gap-1.5 rounded-md px-1.5 text-[11px] text-muted-foreground transition-colors hover:bg-surface-2 hover:text-foreground"
        >
          <ChevronLeft className="h-3.5 w-3.5" /> All navigation
        </button>
        <div className="px-1.5 text-[10px] font-semibold uppercase tracking-[0.16em] text-muted-foreground/70">
          {config.title}
        </div>
        {!config.hideActiveItem ? (
          <div className="mt-1.5 flex h-8 items-center gap-2 rounded-md bg-surface-2 px-2.5 text-xs font-medium text-foreground">
            <ActiveIcon className="h-3.5 w-3.5" /> {config.activeLabel}
          </div>
        ) : null}
      </div>

      <div className="min-h-0 flex-1 overflow-y-auto px-3 py-3">
        {config.views.length ? (
          <>
            <div className="mb-1.5 px-1.5 text-[10px] font-semibold uppercase tracking-[0.16em] text-muted-foreground/70">
              Views
            </div>
            <div className="space-y-0.5">
              {config.views.map((view) => {
                const Icon = view.icon;
                return (
                  <button
                    key={view.label}
                    type="button"
                    onClick={view.onClick}
                    className={cn(
                      "flex h-8 w-full items-center gap-2 rounded-md px-2 text-[11px] text-muted-foreground transition-colors hover:bg-surface-2 hover:text-foreground",
                      view.active && "bg-surface-2 text-foreground",
                    )}
                  >
                    <Icon className="h-3.5 w-3.5" />
                    <span>{view.label}</span>
                    {view.count !== undefined ? (
                      <span className="ml-auto font-mono tabular-nums text-[10px]">{view.count}</span>
                    ) : null}
                  </button>
                );
              })}
            </div>
          </>
        ) : null}

        {config.filters ? (
          <>
            {config.views.length ? <div className="my-4 h-px bg-border-subtle" /> : null}
            <button
              type="button"
              onClick={() => setFiltersOpen((value) => !value)}
              className="flex w-full items-center justify-between px-1.5 text-[10px] font-semibold uppercase tracking-[0.16em] text-muted-foreground/70"
            >
              {config.filtersLabel ?? "Filters"}
              <ChevronDown className={cn("h-3.5 w-3.5 transition-transform", !filtersOpen && "-rotate-90")} />
            </button>
            {filtersOpen ? <div className="mt-3">{config.filters}</div> : null}
          </>
        ) : null}
      </div>

      <div className="border-t border-border-subtle p-3">
        <button
          type="button"
          onClick={onCollapse}
          className="flex h-8 w-full items-center gap-2 rounded-md px-2 text-[11px] text-muted-foreground transition-colors hover:bg-surface-2 hover:text-foreground"
        >
          <ChevronLeft className="h-3.5 w-3.5" /> Collapse sidebar
        </button>
      </div>
    </div>
  );
}
