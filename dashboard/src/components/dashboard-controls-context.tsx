import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";

import { api, type CreateEnvironmentRequest, type Environment } from "@/api/client";
import { parseRefreshInterval, shouldRefreshDashboard, type RefreshInterval } from "./dashboard-refresh-policy";

export type DashboardRange = "1h" | "24h" | "7d" | "30d";

type DashboardControls = {
  range: DashboardRange;
  setRange: (range: DashboardRange) => void;
  rangeLabel: string;
  timeWindow: { from: string; to: string };
  environment: string;
  setEnvironment: (environment: string) => void;
  environments: string[];
  managedEnvironments: Environment[];
  observedEnvironments: string[];
  createEnvironment: (input: CreateEnvironmentRequest) => Promise<Environment>;
  live: boolean;
  setLive: (live: boolean) => void;
  refreshInterval: RefreshInterval;
  setRefreshInterval: (interval: RefreshInterval) => void;
  refresh: () => void;
};

const RANGE_META: Record<DashboardRange, { label: string; durationMs: number }> = {
  "1h": { label: "Last hour", durationMs: 60 * 60 * 1000 },
  "24h": { label: "Last 24 hours", durationMs: 24 * 60 * 60 * 1000 },
  "7d": { label: "Last 7 days", durationMs: 7 * 24 * 60 * 60 * 1000 },
  "30d": { label: "Last 30 days", durationMs: 30 * 24 * 60 * 60 * 1000 },
};

const RANGE_KEY = "bastio-dashboard-range";
const ENVIRONMENT_KEY = "bastio-dashboard-environment";
const LIVE_KEY = "bastio-dashboard-live";
const INTERVAL_KEY = "bastio-dashboard-refresh-interval";

const DashboardControlsContext = createContext<DashboardControls | null>(null);

export function DashboardControlsProvider({ children }: { children: ReactNode }) {
  const queryClient = useQueryClient();
  const [range, setRangeState] = useState<DashboardRange>(() => readRange());
  const [environment, setEnvironmentState] = useState(() => readStored(ENVIRONMENT_KEY));
  const [live, setLiveState] = useState(() => readStored(LIVE_KEY) !== "false");
  const [refreshInterval, setIntervalState] = useState(() => parseRefreshInterval(readStored(INTERVAL_KEY)));
  const [now, setNow] = useState(() => Date.now());

  const observedEnvironmentQuery = useQuery({
    queryKey: ["dashboard-controls", "observed-environments"],
    queryFn: () => api.traces.facets(),
    staleTime: 300_000,
  });

  const managedEnvironmentQuery = useQuery({
    queryKey: ["dashboard-controls", "managed-environments"],
    queryFn: api.environments.list,
    staleTime: 60_000,
  });

  const managedEnvironments = managedEnvironmentQuery.data ?? [];
  const observedEnvironments = useMemo(() => Array.from(new Set(
    (observedEnvironmentQuery.data?.environments ?? [])
      .map((environment) => environment.trim())
      .filter(Boolean),
  )).sort((a, b) => a.localeCompare(b)), [observedEnvironmentQuery.data]);

  const environments = useMemo(() => {
    const values = new Set(managedEnvironments.map((item) => item.name));
    observedEnvironments.forEach((item) => values.add(item));
    if (environment) values.add(environment);
    return Array.from(values).sort((a, b) => a.localeCompare(b));
  }, [environment, managedEnvironments, observedEnvironments]);

  const createEnvironment = async (input: CreateEnvironmentRequest) => {
    const created = await api.environments.create(input);
    await queryClient.invalidateQueries({ queryKey: ["dashboard-controls", "managed-environments"] });
    setEnvironmentState(created.name);
    store(ENVIRONMENT_KEY, created.name);
    return created;
  };

  const timeWindow = useMemo(() => {
    const to = new Date(now);
    const from = new Date(to.getTime() - RANGE_META[range].durationMs);
    return { from: from.toISOString(), to: to.toISOString() };
  }, [now, range]);

  const refresh = useCallback(() => {
    setNow(Date.now());
    // Moving-window queries refresh through their new time-window key.
    // Only explicitly opted-in static dashboard metrics need invalidation.
    void queryClient.invalidateQueries({ type: "active", predicate: (query) => query.meta?.dashboardLive === true });
  }, [queryClient]);

  useEffect(() => {
    if (!live) return;
    const tick = () => {
      if (shouldRefreshDashboard(live, document.visibilityState, Date.now() - now, refreshInterval)) refresh();
    };
    const timer = window.setInterval(tick, refreshInterval);
    document.addEventListener("visibilitychange", tick);
    return () => { window.clearInterval(timer); document.removeEventListener("visibilitychange", tick); };
  }, [live, now, refresh, refreshInterval]);

  const setRange = (next: DashboardRange) => {
    setRangeState(next);
    setNow(Date.now());
    store(RANGE_KEY, next);
  };
  const setEnvironment = (next: string) => {
    setEnvironmentState(next);
    store(ENVIRONMENT_KEY, next);
  };
  const setLive = (next: boolean) => {
    setLiveState(next);
    store(LIVE_KEY, String(next));
    if (next) {
      refresh();
    }
  };
  const setRefreshInterval = (next: RefreshInterval) => {
    setIntervalState(next);
    store(INTERVAL_KEY, String(next));
  };

  return (
    <DashboardControlsContext.Provider
      value={{
        range,
        setRange,
        rangeLabel: RANGE_META[range].label,
        timeWindow,
        environment,
        setEnvironment,
        environments,
        managedEnvironments,
        observedEnvironments,
        createEnvironment,
        live,
        setLive,
        refreshInterval,
        setRefreshInterval,
        refresh,
      }}
    >
      {children}
    </DashboardControlsContext.Provider>
  );
}

export function useDashboardControls(): DashboardControls {
  const context = useContext(DashboardControlsContext);
  if (!context) throw new Error("useDashboardControls must be used inside DashboardControlsProvider");
  return context;
}

export function dashboardRangeLabel(range: DashboardRange): string {
  return RANGE_META[range].label;
}

function readRange(): DashboardRange {
  const stored = readStored(RANGE_KEY);
  return stored === "1h" || stored === "24h" || stored === "7d" || stored === "30d" ? stored : "24h";
}

function readStored(key: string): string {
  try {
    return localStorage.getItem(key) ?? "";
  } catch {
    return "";
  }
}

function store(key: string, value: string) {
  try {
    if (value) localStorage.setItem(key, value);
    else localStorage.removeItem(key);
  } catch {
    // Local storage can be unavailable in hardened browser contexts.
  }
}
