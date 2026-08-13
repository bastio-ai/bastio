import {
  createContext,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from "react";
import type { LucideIcon } from "lucide-react";

export type WorkspaceSidebarView = {
  label: string;
  count?: number | string;
  icon: LucideIcon;
  active?: boolean;
  onClick?: () => void;
};

export type WorkspaceSidebarConfig = {
  parentLabel: string;
  parentTo: string;
  title: string;
  activeLabel: string;
  activeIcon: LucideIcon;
  hideActiveItem?: boolean;
  views: WorkspaceSidebarView[];
  filters?: ReactNode;
  filtersLabel?: string;
  timeLabel?: string;
};

type WorkspaceSidebarContextValue = {
  config: WorkspaceSidebarConfig | null;
  setConfig: (config: WorkspaceSidebarConfig | null) => void;
};

const WorkspaceSidebarContext = createContext<WorkspaceSidebarContextValue | null>(null);

export function WorkspaceSidebarProvider({ children }: { children: ReactNode }) {
  const [config, setConfig] = useState<WorkspaceSidebarConfig | null>(null);
  const value = useMemo(() => ({ config, setConfig }), [config]);
  return <WorkspaceSidebarContext.Provider value={value}>{children}</WorkspaceSidebarContext.Provider>;
}

export function useWorkspaceSidebar(config: WorkspaceSidebarConfig | null) {
  const context = useContext(WorkspaceSidebarContext);
  const setConfig = context?.setConfig;
  useEffect(() => {
    if (!setConfig) return;
    setConfig(config);
    return () => setConfig(null);
  }, [config, setConfig]);
}

export function useWorkspaceSidebarState() {
  const context = useContext(WorkspaceSidebarContext);
  if (!context) throw new Error("useWorkspaceSidebarState must be used inside WorkspaceSidebarProvider");
  return context;
}
