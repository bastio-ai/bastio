import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Link, useNavigate, useSearch } from "@tanstack/react-router";
import {
  AlertCircle,
  ChevronDown,
  ChevronLeft,
  CircleDot,
  RotateCcw,
  ShieldAlert,
  ShieldCheck,
} from "lucide-react";

import { api } from "@/api/client";
import { dashboardRangeLabel, useDashboardControls, type DashboardRange } from "@/components/dashboard-controls-context";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { cn } from "@/lib/utils";

type ThreatSidebarSearch = {
  severity?: string;
  action_taken?: string;
  detector_name?: string;
  threat_subtype?: string;
  end_user_id?: string;
  ip_address?: string;
  from?: string;
  to?: string;
  page?: number;
  [key: string]: unknown;
};

type Props = {
  onBack: () => void;
  onCollapse: () => void;
};

const ALL = "__all__";

export function ThreatContextSidebar({ onBack, onCollapse }: Props) {
  const navigate = useNavigate();
  const controls = useDashboardControls();
  const search = useSearch({ from: "/threats" }) as ThreatSidebarSearch;
  const [filtersOpen, setFiltersOpen] = useState(true);

  const queryParams = useMemo(
    () => ({
      limit: 50,
      severity: asString(search.severity),
      action_taken: asString(search.action_taken),
      detector_name: asString(search.detector_name),
      threat_subtype: asString(search.threat_subtype),
      end_user_id: asString(search.end_user_id),
      ip_address: asString(search.ip_address),
      from: asString(search.from) || controls.timeWindow.from,
      to: asString(search.to) || controls.timeWindow.to,
      environment: controls.environment || undefined,
    }),
    [controls.environment, controls.timeWindow, search],
  );

  const { data } = useQuery({
    queryKey: ["threats-context-sidebar", queryParams],
    queryFn: () => api.threats.list(queryParams),
    placeholderData: (previous) => previous,
  });

  const rows = data ?? [];
  const criticalHigh = rows.filter(
    (row) => row.severity === "critical" || row.severity === "high",
  ).length;
  const blocked = rows.filter((row) => row.action_taken === "block").length;
  const detectors = Array.from(
    new Set(rows.map((row) => row.detector_name).filter(Boolean)),
  ).sort();

  const updateSearch = (patch: Partial<ThreatSidebarSearch>) => {
    navigate({
      to: "/threats",
      search: {
        ...search,
        ...patch,
        page: undefined,
      } as never,
    });
  };

  const resetFilters = () => {
    updateSearch({
      severity: undefined,
      action_taken: undefined,
      detector_name: undefined,
      threat_subtype: undefined,
      end_user_id: undefined,
      ip_address: undefined,
      from: undefined,
      to: undefined,
    });
  };

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
          Threats
        </div>
        <Link
          to="/threats"
          search={{} as never}
          className="mt-1.5 flex h-8 items-center gap-2 rounded-md bg-surface-2 px-2.5 text-xs font-medium text-foreground"
        >
          <ShieldAlert className="h-3.5 w-3.5" /> Threat events
        </Link>
      </div>

      <div className="min-h-0 flex-1 overflow-y-auto px-3 py-3">
        <SectionLabel>Saved views</SectionLabel>
        <div className="space-y-0.5">
          <ViewLink icon={CircleDot} label="All threats" count={rows.length} />
          <ViewButton
            icon={AlertCircle}
            label="Critical + High"
            count={criticalHigh}
            active={search.severity === "critical,high"}
            onClick={() => updateSearch({ severity: "critical,high" })}
          />
          <ViewButton
            icon={ShieldCheck}
            label="Blocked"
            count={blocked}
            active={search.action_taken === "block"}
            onClick={() => updateSearch({ action_taken: "block" })}
          />
        </div>

        <div className="my-4 h-px bg-border-subtle" />

        <button
          type="button"
          onClick={() => setFiltersOpen((value) => !value)}
          className="flex w-full items-center justify-between px-1.5 text-[10px] font-semibold uppercase tracking-[0.16em] text-muted-foreground/70"
        >
          Filters
          <ChevronDown
            className={cn(
              "h-3.5 w-3.5 transition-transform",
              !filtersOpen && "-rotate-90",
            )}
          />
        </button>

        {filtersOpen ? (
          <div className="mt-3 space-y-3">
            <Filter label="Time window">
              <Select
                value={controls.range}
                onValueChange={(value) => controls.setRange(value as DashboardRange)}
              >
                <SelectTrigger className="w-full">
                  <SelectValue>{dashboardRangeLabel(controls.range)}</SelectValue>
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="1h">Last hour</SelectItem>
                  <SelectItem value="24h">Last 24 hours</SelectItem>
                  <SelectItem value="7d">Last 7 days</SelectItem>
                  <SelectItem value="30d">Last 30 days</SelectItem>
                </SelectContent>
              </Select>
            </Filter>

            <Filter label="Environment">
              <Select value={controls.environment || ALL} onValueChange={(value) => controls.setEnvironment(value === ALL ? "" : value ?? "")}>
                <SelectTrigger className="w-full"><SelectValue>{controls.environment || "All environments"}</SelectValue></SelectTrigger>
                <SelectContent>
                  <SelectItem value={ALL}>All environments</SelectItem>
                  {controls.environments.map((environment) => <SelectItem key={environment} value={environment}>{environment}</SelectItem>)}
                </SelectContent>
              </Select>
            </Filter>

            <Filter label="Severity">
              <Select
                value={search.severity ?? ALL}
                onValueChange={(value) =>
                  updateSearch({ severity: value === ALL ? undefined : value ?? undefined })
                }
              >
                <SelectTrigger className="w-full"><SelectValue>{severityLabel(search.severity)}</SelectValue></SelectTrigger>
                <SelectContent>
                  <SelectItem value={ALL}>All severities</SelectItem>
                  <SelectItem value="critical">Critical</SelectItem>
                  <SelectItem value="high">High</SelectItem>
                  <SelectItem value="medium">Medium</SelectItem>
                  <SelectItem value="low">Low</SelectItem>
                </SelectContent>
              </Select>
            </Filter>

            <Filter label="Action">
              <Select
                value={search.action_taken ?? ALL}
                onValueChange={(value) =>
                  updateSearch({ action_taken: value === ALL ? undefined : value ?? undefined })
                }
              >
                <SelectTrigger className="w-full"><SelectValue>{actionLabel(search.action_taken)}</SelectValue></SelectTrigger>
                <SelectContent>
                  <SelectItem value={ALL}>All actions</SelectItem>
                  <SelectItem value="block">Block</SelectItem>
                  <SelectItem value="warn">Warn</SelectItem>
                  <SelectItem value="log">Log only</SelectItem>
                </SelectContent>
              </Select>
            </Filter>

            <Filter label="Detector">
              <Select
                value={search.detector_name ?? ALL}
                onValueChange={(value) =>
                  updateSearch({ detector_name: value === ALL ? undefined : value ?? undefined })
                }
              >
                <SelectTrigger className="w-full"><SelectValue>{search.detector_name || "All detectors"}</SelectValue></SelectTrigger>
                <SelectContent>
                  <SelectItem value={ALL}>All detectors</SelectItem>
                  {detectors.map((detector) => (
                    <SelectItem key={detector} value={detector}>{detector}</SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </Filter>

            <TextFilter
              label="Subtype"
              placeholder="e.g. injection"
              value={asString(search.threat_subtype) ?? ""}
              onCommit={(value) => updateSearch({ threat_subtype: value || undefined })}
            />
            <TextFilter
              label="End user"
              placeholder="Search end user…"
              value={asString(search.end_user_id) ?? ""}
              onCommit={(value) => updateSearch({ end_user_id: value || undefined })}
            />
            <TextFilter
              label="IP address"
              placeholder="Search IP…"
              value={asString(search.ip_address) ?? ""}
              onCommit={(value) => updateSearch({ ip_address: value || undefined })}
            />

            <button
              type="button"
              onClick={resetFilters}
              className="flex h-8 w-full items-center justify-center gap-1.5 rounded-md border border-border-subtle text-[11px] text-muted-foreground transition-colors hover:bg-surface-2 hover:text-foreground"
            >
              <RotateCcw className="h-3 w-3" /> Reset filters
            </button>
          </div>
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

function SectionLabel({ children }: { children: React.ReactNode }) {
  return (
    <div className="mb-1.5 px-1.5 text-[10px] font-semibold uppercase tracking-[0.16em] text-muted-foreground/70">
      {children}
    </div>
  );
}

function ViewLink({
  icon: Icon,
  label,
  count,
}: {
  icon: typeof CircleDot;
  label: string;
  count: number;
}) {
  return (
    <Link
      to="/threats"
      search={{} as never}
      className="flex h-8 items-center gap-2 rounded-md px-2 text-[11px] text-muted-foreground transition-colors hover:bg-surface-2 hover:text-foreground"
    >
      <Icon className="h-3.5 w-3.5" />
      <span>{label}</span>
      <span className="ml-auto font-mono tabular-nums text-[10px]">{count}</span>
    </Link>
  );
}

function ViewButton({
  icon: Icon,
  label,
  count,
  active = false,
  onClick,
}: {
  icon: typeof CircleDot;
  label: string;
  count: number;
  active?: boolean;
  onClick?: () => void;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={cn(
        "flex h-8 w-full items-center gap-2 rounded-md px-2 text-[11px] text-muted-foreground transition-colors hover:bg-surface-2 hover:text-foreground",
        active && "bg-surface-2 text-foreground",
      )}
    >
      <Icon className="h-3.5 w-3.5" />
      <span>{label}</span>
      <span className="ml-auto font-mono tabular-nums text-[10px]">{count}</span>
    </button>
  );
}

function Filter({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <label className="block space-y-1">
      <span className="text-[10px] text-muted-foreground">{label}</span>
      {children}
    </label>
  );
}

function TextFilter({
  label,
  value,
  placeholder,
  onCommit,
}: {
  label: string;
  value: string;
  placeholder: string;
  onCommit: (value: string) => void;
}) {
  const [draft, setDraft] = useState(value);
  return (
    <Filter label={label}>
      <Input
        value={draft}
        placeholder={placeholder}
        onChange={(event) => setDraft(event.target.value)}
        onBlur={() => onCommit(draft.trim())}
        onKeyDown={(event) => {
          if (event.key === "Enter") onCommit(draft.trim());
        }}
        className="text-xs"
      />
    </Filter>
  );
}

function asString(value: unknown): string | undefined {
  return typeof value === "string" && value ? value : undefined;
}

function severityLabel(value: unknown) {
  if (value === "critical,high") return "Critical + High";
  if (typeof value !== "string" || !value) return "All severities";
  return value.charAt(0).toUpperCase() + value.slice(1);
}

function actionLabel(value: unknown) {
  if (typeof value !== "string" || !value) return "All actions";
  if (value === "log_only") return "Log only";
  return value.charAt(0).toUpperCase() + value.slice(1);
}
