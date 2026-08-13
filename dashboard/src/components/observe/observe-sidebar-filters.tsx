import { RotateCcw } from "lucide-react";

import { dashboardRangeLabel, type DashboardRange } from "@/components/dashboard-controls-context";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { emptyFilters, type ObserveFilters } from "@/components/observe/filter-bar";

const ALL = "__all__";

export function ObserveSidebarFilters({
  value,
  onChange,
  environments = [],
  mode,
  range,
  onRangeChange,
  environment,
  onEnvironmentChange,
}: {
  value: ObserveFilters;
  onChange: (value: ObserveFilters) => void;
  environments?: string[];
  mode: "traces" | "sessions";
  range?: DashboardRange;
  onRangeChange?: (range: DashboardRange) => void;
  environment?: string;
  onEnvironmentChange?: (environment: string) => void;
}) {
  const set = <K extends keyof ObserveFilters>(key: K, next: ObserveFilters[K]) =>
    onChange({ ...value, [key]: next });
  const active = Object.values(value).some(Boolean) || Boolean(environment);

  return (
    <div className="space-y-3">
      <Filter label="Time window">
        <Select
          value={range ?? (value.from ? "custom" : ALL)}
          onValueChange={(preset) => {
            if (onRangeChange && (preset === "1h" || preset === "24h" || preset === "7d" || preset === "30d")) onRangeChange(preset);
            else if (preset === "hour") onChange({ ...value, from: localDateTime(Date.now() - 3_600_000), to: "" });
            else if (preset === "day") onChange({ ...value, from: localDateTime(Date.now() - 86_400_000), to: "" });
            else onChange({ ...value, from: "", to: "" });
          }}
        >
          <SelectTrigger className="w-full"><SelectValue>{range ? dashboardRangeLabel(range) : value.from ? "Custom range" : "All time"}</SelectValue></SelectTrigger>
          <SelectContent>
            {onRangeChange ? (
              <>
                <SelectItem value="1h">Last hour</SelectItem>
                <SelectItem value="24h">Last 24 hours</SelectItem>
                <SelectItem value="7d">Last 7 days</SelectItem>
                <SelectItem value="30d">Last 30 days</SelectItem>
              </>
            ) : (
              <>
                <SelectItem value={ALL}>All time</SelectItem>
                <SelectItem value="hour">Last hour</SelectItem>
                <SelectItem value="day">Last 24 hours</SelectItem>
                {value.from ? <SelectItem value="custom">Custom range</SelectItem> : null}
              </>
            )}
          </SelectContent>
        </Select>
      </Filter>

      {mode === "traces" ? (
        <>
          <Filter label="Status">
            <Select value={value.status || ALL} onValueChange={(next) => set("status", next === ALL ? "" : next ?? "")}>
              <SelectTrigger className="w-full"><SelectValue>{value.status || "All statuses"}</SelectValue></SelectTrigger>
              <SelectContent>
                <SelectItem value={ALL}>All statuses</SelectItem>
                <SelectItem value="ok">OK</SelectItem>
                <SelectItem value="error">Error</SelectItem>
                <SelectItem value="blocked">Blocked</SelectItem>
                <SelectItem value="rate_limited">Rate limited</SelectItem>
              </SelectContent>
            </Select>
          </Filter>
          <Filter label="Security">
            <Select value={value.security || ALL} onValueChange={(next) => set("security", next === ALL ? "" : next ?? "")}>
              <SelectTrigger className="w-full"><SelectValue>{value.security === "threat" ? "Threat detected" : value.security === "clean" ? "Clean" : "Any security"}</SelectValue></SelectTrigger>
              <SelectContent>
                <SelectItem value={ALL}>Any security</SelectItem>
                <SelectItem value="clean">Clean</SelectItem>
                <SelectItem value="threat">Threat detected</SelectItem>
              </SelectContent>
            </Select>
          </Filter>
        </>
      ) : null}

      <Filter label="Environment">
        <Select value={(environment ?? value.environment) || ALL} onValueChange={(next) => onEnvironmentChange ? onEnvironmentChange(next === ALL ? "" : next ?? "") : set("environment", next === ALL ? "" : next ?? "")}>
          <SelectTrigger className="w-full"><SelectValue>{(environment ?? value.environment) || "All environments"}</SelectValue></SelectTrigger>
          <SelectContent>
            <SelectItem value={ALL}>All environments</SelectItem>
            {environments.map((environment) => <SelectItem key={environment} value={environment}>{environment}</SelectItem>)}
          </SelectContent>
        </Select>
      </Filter>

      {mode === "traces" ? (
        <>
          <TextFilter label="Provider" placeholder="e.g. openai" value={value.provider} onChange={(next) => set("provider", next)} />
          <TextFilter label="Model" placeholder="e.g. gpt-4o" value={value.model} onChange={(next) => set("model", next)} />
          <TextFilter label="Trace name" placeholder="e.g. guard_chat" value={value.traceName} onChange={(next) => set("traceName", next)} />
          <TextFilter label="Release" placeholder="e.g. v1.4.0" value={value.release} onChange={(next) => set("release", next)} />
          <TextFilter label="Tags" placeholder="feature:checkout" value={value.tags} onChange={(next) => set("tags", next)} />
        </>
      ) : null}

      <TextFilter label="End user" placeholder="Search end user…" value={value.endUser} onChange={(next) => set("endUser", next)} />
      <TextFilter label={mode === "traces" ? "Content" : "Session"} placeholder={mode === "traces" ? "Search traces…" : "Search sessions…"} value={value.search} onChange={(next) => set("search", next)} />

      <button
        type="button"
        onClick={() => {
          onChange(emptyFilters);
          onEnvironmentChange?.("");
        }}
        disabled={!active}
        className="flex h-8 w-full items-center justify-center gap-1.5 rounded-md border border-border-subtle text-[11px] text-muted-foreground transition-colors hover:bg-surface-2 hover:text-foreground disabled:opacity-40"
      >
        <RotateCcw className="h-3 w-3" /> Reset filters
      </button>
    </div>
  );
}

function Filter({ label, children }: { label: string; children: React.ReactNode }) {
  return <label className="block space-y-1"><span className="text-[10px] text-muted-foreground">{label}</span>{children}</label>;
}

function TextFilter({ label, placeholder, value, onChange }: { label: string; placeholder: string; value: string; onChange: (value: string) => void }) {
  return <Filter label={label}><Input value={value} placeholder={placeholder} onChange={(event) => onChange(event.target.value)} className="text-xs" /></Filter>;
}

function localDateTime(timestamp: number) {
  const date = new Date(timestamp);
  const pad = (part: number) => String(part).padStart(2, "0");
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}`;
}
