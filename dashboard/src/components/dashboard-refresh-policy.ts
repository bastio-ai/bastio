export const REFRESH_INTERVALS = [
  { value: 30_000, label: "Every 30 seconds", short: "30s" },
  { value: 60_000, label: "Every minute", short: "1m" },
  { value: 120_000, label: "Every 2 minutes", short: "2m" },
  { value: 300_000, label: "Every 5 minutes", short: "5m" },
] as const;
export type RefreshInterval = (typeof REFRESH_INTERVALS)[number]["value"];
export function parseRefreshInterval(value: string): RefreshInterval {
  return REFRESH_INTERVALS.find((option) => String(option.value) === value)?.value ?? 60_000;
}
export function shouldRefreshDashboard(live: boolean, visibility: string, elapsed: number, interval: number) {
  return live && visibility === "visible" && elapsed >= interval;
}
