import { RefreshCw } from "lucide-react";
import { useDashboardControls } from "./dashboard-controls-context";
import { REFRESH_INTERVALS, parseRefreshInterval } from "./dashboard-refresh-policy";
import { Button } from "./ui/button";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "./ui/select";

export function DashboardRefreshControl({ showRefresh = true }: { showRefresh?: boolean }) {
  const controls = useDashboardControls();
  return <div className="flex shrink-0 items-center gap-1.5">
    {showRefresh ? <Button variant="outline" size="icon-sm" aria-label="Refresh dashboard now" title="Refresh now without changing automatic refresh" onClick={controls.refresh}><RefreshCw className="size-3.5" /></Button> : null}
    <Select value={controls.live ? String(controls.refreshInterval) : "manual"} onValueChange={(value) => {
      if (!value) return;
      if (value === "manual") { controls.setLive(false); return; }
      controls.setRefreshInterval(parseRefreshInterval(value));
      if (!controls.live) controls.setLive(true);
    }}>
      <SelectTrigger aria-label="Dashboard refresh interval" className="h-8 w-[140px] cursor-pointer text-xs"><SelectValue>{controls.live ? `Auto · ${REFRESH_INTERVALS.find((option) => option.value === controls.refreshInterval)?.short}` : "Manual refresh"}</SelectValue></SelectTrigger>
      <SelectContent>
        <SelectItem value="manual">Manual refresh</SelectItem>
        {REFRESH_INTERVALS.map((option) => <SelectItem key={option.value} value={String(option.value)}>{option.label}</SelectItem>)}
      </SelectContent>
    </Select>
  </div>;
}
