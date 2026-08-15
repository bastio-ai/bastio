import { Fragment, useEffect, useRef, useState, type ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import { Link, useNavigate, useRouterState } from "@tanstack/react-router";
import {
  Activity,
  BarChart3,
  BookOpen,
  Bot,
  Building2,
  ChevronRight,
  FileCheck,
  FlaskConical,
  ExternalLink,
  GitBranch,
  Key,
  Layers,
  LayoutDashboard,
  Menu,
  MessagesSquare,
  Moon,
  Pause,
  Play,
  Plus,
  Search,
  Server,
  Settings2,
  Shield,
  ShieldAlert,
  Sparkles,
  Sun,
  Terminal,
  Users,
  Zap,
} from "lucide-react";

import { api } from "@/api/client";
import { BastioWordmark } from "@/components/bastio-wordmark";
import { CommandPalette, useCommandPalette } from "@/components/command-palette";
import { ConnectCliDialog } from "@/components/connect-cli-dialog";
import {
  DashboardControlsProvider,
  useDashboardControls,
  type DashboardRange,
} from "@/components/dashboard-controls-context";
import { FooterStrip } from "@/components/data/footer-strip";
import { useHeaderExtension } from "@/components/header-extension";
import type { LayoutNavSection } from "@/components/layout-extension";
import { useLayoutNav } from "@/components/layout-extension";
import { ThreatContextSidebar } from "@/components/observe/threat-context-sidebar";
import { WorkspaceContextSidebar } from "@/components/workspace-context-sidebar";
import {
  WorkspaceSidebarProvider,
  useWorkspaceSidebarState,
} from "@/components/workspace-sidebar-context";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger } from "@/components/ui/select";
import { Separator } from "@/components/ui/separator";
import { UserProfileWidget } from "@/components/user-profile-widget";
import { useProfileExtension } from "@/components/profile-extension";
import { cn } from "@/lib/utils";
import { DOCS_URL } from "@/lib/external-links";
import { useTheme } from "@/lib/use-theme";

const BUILD_SHA =
  (typeof import.meta !== "undefined" &&
    (import.meta as ImportMeta & { env: Record<string, string | undefined> }).env
      ?.VITE_BUILD_SHA) ||
  "dev";

const SIDEBAR_KEY = "bastio-sidebar-collapsed";

const navSections = [
  {
    label: "Observe",
    items: [
      { to: "/", label: "Overview", icon: LayoutDashboard },
      { to: "/traces", label: "Traces", icon: Activity },
      { to: "/cache", label: "Response Cache", icon: Zap },
      { to: "/sessions", label: "Sessions", icon: MessagesSquare },
      { to: "/users", label: "End Users", icon: Users },
      { to: "/analytics", label: "Analytics", icon: BarChart3 },
    ],
  },
  {
    label: "Security & Guardrails",
    items: [
      { to: "/threats", label: "Threats", icon: ShieldAlert },
      { to: "/mcp", label: "Agent & Tool Firewalls", icon: Bot },
      { to: "/security-settings", label: "Security Center", icon: Shield },
      { to: "/overlays", label: "Custom Policies", icon: Layers },
      { to: "/playground", label: "Security Playground", icon: FlaskConical },
      { to: "/compliance", label: "Compliance & Audit", icon: FileCheck },
    ],
  },
  {
    label: "Workforce",
    items: [{ to: "/workspace", label: "Private AI Portal", icon: Sparkles }],
  },
  {
    label: "Developer API",
    items: [
      { to: "/api-keys", label: "API Keys", icon: Key },
      { to: "/proxies", label: "LLM Gateways", icon: Server },
    ],
  },
] as const;

export function Layout({ children }: { children: ReactNode }) {
  return (
    <DashboardControlsProvider>
      <WorkspaceSidebarProvider>
        <LayoutFrame>{children}</LayoutFrame>
      </WorkspaceSidebarProvider>
    </DashboardControlsProvider>
  );
}

function LayoutFrame({ children }: { children: ReactNode }) {
  const navigate = useNavigate();
  const router = useRouterState();
  const currentPath = router.location.pathname;
  const { theme, toggleTheme } = useTheme();
  const palette = useCommandPalette();
  const controls = useDashboardControls();
  const headerExtension = useHeaderExtension();
  const [mobileOpen, setMobileOpen] = useState(false);
  const [workspaceDialogOpen, setWorkspaceDialogOpen] = useState(false);
  const [workspaceName, setWorkspaceName] = useState("");
  const [workspaceError, setWorkspaceError] = useState("");
  const [creatingWorkspace, setCreatingWorkspace] = useState(false);
  const [environmentDialogOpen, setEnvironmentDialogOpen] = useState(false);
  const [environmentName, setEnvironmentName] = useState("");
  const [environmentKind, setEnvironmentKind] = useState<"production" | "staging" | "development" | "custom">("custom");
  const [environmentDescription, setEnvironmentDescription] = useState("");
  const [environmentError, setEnvironmentError] = useState("");
  const [creatingEnvironment, setCreatingEnvironment] = useState(false);
  const [connectCliOpen, setConnectCliOpen] = useState(false);
  const [showGlobalNavigation, setShowGlobalNavigation] = useState(false);
  const [sidebarCollapsed, setSidebarCollapsed] = useState(() => {
    try {
      return localStorage.getItem(SIDEBAR_KEY) === "true";
    } catch {
      return false;
    }
  });
  const navExt = useLayoutNav();
  const profileExt = useProfileExtension();
  const isThreatWorkspace = currentPath === "/threats";
  const isFocusedWorkspace = ["/", "/traces", "/sessions", "/analytics", "/threats"].includes(currentPath);
  const { config: workspaceSidebar } = useWorkspaceSidebarState();
  const hasContextSidebar = currentPath !== "/" && (isThreatWorkspace || Boolean(workspaceSidebar));
  const showContextNavigation = hasContextSidebar && !sidebarCollapsed && !showGlobalNavigation;
  const sidebarViewKey = showContextNavigation
    ? `context:${isThreatWorkspace ? "threats" : currentPath}`
    : sidebarCollapsed
      ? "collapsed"
      : "global";

  useEffect(() => {
    setMobileOpen(false);
    setShowGlobalNavigation(false);
  }, [currentPath]);
  useEffect(() => {
    try {
      localStorage.setItem(SIDEBAR_KEY, String(sidebarCollapsed));
    } catch {
      // Local storage can be unavailable in hardened browser contexts.
    }
  }, [sidebarCollapsed]);

  const health = useQuery({
    queryKey: ["health"],
    queryFn: api.health,
    refetchInterval: 15_000,
    retry: false,
  });
  const healthStatus = health.data?.status as string | undefined;
  const connected = healthStatus === "healthy" || healthStatus === "operational";
  const workspaces = headerExtension.workspaces?.length
    ? headerExtension.workspaces
    : [{ id: "local", name: "Local deployment", detail: "Open source" }];
  const activeWorkspaceID = headerExtension.activeWorkspaceID ?? workspaces[0]?.id ?? "local";
  const activeWorkspace = workspaces.find((workspace) => workspace.id === activeWorkspaceID) ?? workspaces[0];
  const workspaceItems = [
    ...workspaces.map((workspace) => ({ value: workspace.id, label: workspace.name, detail: workspace.detail })),
    ...(headerExtension.workspaces?.length
      ? [{ value: "__manage_workspaces__", label: "Manage workspaces", detail: "Access, ownership, and isolation", icon: <Settings2 className="mt-0.5 h-3.5 w-3.5 flex-shrink-0" /> }]
      : []),
    ...(headerExtension.onCreateWorkspace
      ? [{ value: "__create_workspace__", label: "Create workspace", detail: "New tenant and separate billing", action: true }]
      : []),
  ];
  const environmentItems = [
    { value: "__all__", label: "All environments", detail: "No environment filter" },
    ...controls.environments.map((environment) => ({
      value: environment,
      label: environment,
      detail: controls.managedEnvironments.some((item) => item.name === environment)
        ? controls.managedEnvironments.find((item) => item.name === environment)?.kind
        : "Observed · not managed",
    })),
    ...(controls.environment && !controls.managedEnvironments.some((item) => item.name === controls.environment)
      ? [{ value: "__adopt_environment__", label: `Adopt ${controls.environment}`, detail: "Add the observed value to the registry", action: true }]
      : []),
    { value: "__create_environment__", label: "Create environment", detail: "Register a deployment boundary", action: true },
  ];

  const handleWorkspaceChange = (value: string) => {
    if (value === "__manage_workspaces__") {
      void navigate({ to: "/workspaces" });
      return;
    }
    if (value === "__create_workspace__") {
      setWorkspaceError("");
      setWorkspaceDialogOpen(true);
      return;
    }
    void headerExtension.onWorkspaceChange?.(value);
  };

  const handleEnvironmentChange = (value: string) => {
    if (value === "__create_environment__" || value === "__adopt_environment__") {
      const nextName = value === "__adopt_environment__" ? controls.environment : "";
      setEnvironmentName(nextName);
      setEnvironmentKind(guessEnvironmentKind(nextName));
      setEnvironmentDescription("");
      setEnvironmentError("");
      setEnvironmentDialogOpen(true);
      return;
    }
    controls.setEnvironment(value === "__all__" ? "" : value);
  };

  return (
    <div className="flex h-screen flex-col overflow-hidden bg-background">
      <header className="hidden h-14 flex-shrink-0 items-center border-b border-border-subtle bg-background md:flex">
        <button
          type="button"
          onClick={() => setSidebarCollapsed((value) => !value)}
          aria-label={sidebarCollapsed ? "Expand sidebar" : "Collapse sidebar"}
          className={cn(
            "flex h-full flex-shrink-0 items-center border-r border-border-subtle px-4 text-left transition-[width] duration-200 hover:bg-surface-1",
            sidebarCollapsed ? "w-14 justify-center px-0" : "w-[220px]",
          )}
        >
          <BastioWordmark height={sidebarCollapsed ? 12 : 26} mark={sidebarCollapsed} />
        </button>

        <div className="flex min-w-0 flex-1 items-center gap-2 px-3">
          <button
            type="button"
            onClick={palette.toggle}
            className="mx-auto flex h-8 min-w-[260px] max-w-[560px] flex-1 items-center gap-2 rounded-md border border-border-subtle bg-surface-1 px-3 text-[11px] text-muted-foreground transition-colors hover:border-border-default hover:text-foreground"
          >
            <Search className="h-3.5 w-3.5" />
            <span>Search or run a command…</span>
            <kbd className="ml-auto rounded border border-border-subtle bg-surface-2 px-1.5 py-0.5 font-mono text-[9px]">⌘ K</kbd>
          </button>

          <HeaderSelect
            className="hidden lg:flex"
            ariaLabel="Change time window"
            value={controls.range}
            onChange={(value) => controls.setRange(value as DashboardRange)}
            eyebrow="Time window"
            displayValue={controls.rangeLabel}
            items={[
              { value: "1h", label: "Last hour" },
              { value: "24h", label: "Last 24 hours" },
              { value: "7d", label: "Last 7 days" },
              { value: "30d", label: "Last 30 days" },
            ]}
          />
          <button
            type="button"
            onClick={() => controls.setLive(!controls.live)}
            aria-pressed={controls.live}
            className={cn(
              "hidden h-8 items-center gap-2 rounded-md border border-border-subtle bg-surface-1 px-2.5 text-[11px] transition-colors hover:border-border-default hover:text-foreground xl:flex",
              !controls.live && "text-muted-foreground",
            )}
          >
            {controls.live ? <Pause className="h-3.5 w-3.5" /> : <Play className="h-3.5 w-3.5" />}
            <span>{controls.live ? "Live" : "Paused"}</span>
          </button>
          <Button
            variant="outline"
            size="sm"
            onClick={() => setConnectCliOpen(true)}
            className="hidden sm:flex h-8 items-center gap-1.5 px-2.5 text-xs font-medium border-border-subtle bg-surface-1 hover:border-border-default hover:bg-surface-2 transition-colors"
          >
            <Terminal className="size-3.5 text-accent" />
            <span>Connect CLI &amp; MCP</span>
          </Button>
          <ConnectionStatus
            health={health}
            connected={connected}
            mode={headerExtension.statusMode ?? "dependencies"}
            statusPageURL={headerExtension.statusPageURL}
          />
          <Button variant="ghost" size="icon-sm" onClick={toggleTheme} aria-label="Toggle theme">
            {theme === "dark" ? <Sun className="h-3.5 w-3.5" /> : <Moon className="h-3.5 w-3.5" />}
          </Button>
          <UserProfileWidget {...profileExt.userWidget} variant="header" />
        </div>
      </header>

      <header className="flex h-12 flex-shrink-0 items-center gap-3 border-b border-border-subtle px-3 md:hidden">
        <button type="button" onClick={() => setMobileOpen(true)} aria-label="Open navigation" className="flex h-8 w-8 items-center justify-center rounded-md hover:bg-surface-2">
          <Menu className="h-4 w-4" />
        </button>
        <BastioWordmark height={20} mark />
        <button type="button" onClick={palette.toggle} className="ml-auto flex h-8 items-center gap-2 rounded-md border border-border-subtle px-2.5 text-[11px] text-muted-foreground">
          <Search className="h-3.5 w-3.5" /> Search
        </button>
        <Button variant="ghost" size="icon-sm" onClick={toggleTheme} aria-label="Toggle theme">
          {theme === "dark" ? <Sun className="h-3.5 w-3.5" /> : <Moon className="h-3.5 w-3.5" />}
        </Button>
        <UserProfileWidget {...profileExt.userWidget} variant="header" />
      </header>

      <div className="flex min-h-0 flex-1">
        {mobileOpen ? <button type="button" aria-label="Close navigation" onClick={() => setMobileOpen(false)} className="fixed inset-0 z-40 bg-black/60 md:hidden" /> : null}

        <aside
          className={cn(
            "flex flex-shrink-0 flex-col border-r border-border-subtle bg-background transition-[width,transform] duration-200",
            sidebarCollapsed ? "w-14" : "w-[220px]",
            "fixed inset-y-0 left-0 z-50 md:static",
            mobileOpen ? "translate-x-0 w-[260px]" : "-translate-x-full md:translate-x-0",
          )}
        >
          <div className="px-3 pb-2 pt-3 md:hidden"><BastioWordmark height={28} /></div>
          <SidebarContextSelect
            value={activeWorkspaceID}
            onChange={handleWorkspaceChange}
            disabled={!headerExtension.onWorkspaceChange || headerExtension.switchingWorkspace}
            ariaLabel="Switch workspace"
            eyebrow="Workspace"
            displayValue={headerExtension.switchingWorkspace ? "Switching…" : activeWorkspace?.name ?? "Workspace"}
            icon={<Building2 className="h-4 w-4" />}
            items={workspaceItems}
            collapsed={sidebarCollapsed && !mobileOpen}
            placement="top"
          />
          <div
            key={sidebarViewKey}
            className={cn(
              "flex min-h-0 flex-1 flex-col",
              showContextNavigation ? "sidebar-view-enter-forward" : !sidebarCollapsed && "sidebar-view-enter-back",
            )}
          >
            {showContextNavigation ? (
              isThreatWorkspace ? (
                <ThreatContextSidebar onBack={() => setShowGlobalNavigation(true)} onCollapse={() => setSidebarCollapsed(true)} />
              ) : workspaceSidebar ? (
                <WorkspaceContextSidebar config={workspaceSidebar} onBack={() => setShowGlobalNavigation(true)} onCollapse={() => setSidebarCollapsed(true)} />
              ) : null
            ) : (
              <GlobalNavigation
                currentPath={currentPath}
                collapsed={sidebarCollapsed}
                navExt={navExt}
                onExpand={() => setSidebarCollapsed(false)}
                theme={theme}
                toggleTheme={toggleTheme}
                onNavigate={() => setShowGlobalNavigation(false)}
              />
            )}
          </div>
          <SidebarContextSelect
            value={controls.environment || "__all__"}
            onChange={handleEnvironmentChange}
            ariaLabel="Filter by environment"
            eyebrow="Environment"
            displayValue={controls.environment || "All environments"}
            icon={<GitBranch className="h-4 w-4" />}
            status
            items={environmentItems}
            collapsed={sidebarCollapsed && !mobileOpen}
            placement="bottom"
          />
        </aside>

        <main className="flex min-w-0 flex-1 flex-col overflow-hidden bg-background">
          <div className={cn("min-h-0 flex-1 overflow-auto", isFocusedWorkspace ? "p-0" : "px-4 py-5 md:px-6")}>
            <div className={cn(isFocusedWorkspace ? "h-full min-h-0" : "mx-auto max-w-[1400px]")}>{children}</div>
          </div>
          {!isFocusedWorkspace ? <FooterStrip buildSha={BUILD_SHA.slice(0, 7)} connected={connected} /> : null}
        </main>
      </div>

      <CommandPalette open={palette.open} onOpenChange={palette.setOpen} />

      <Dialog open={workspaceDialogOpen} onOpenChange={setWorkspaceDialogOpen}>
        <DialogContent className="sm:max-w-md">
          <form
            onSubmit={async (event) => {
              event.preventDefault();
              if (!headerExtension.onCreateWorkspace || creatingWorkspace) return;
              setCreatingWorkspace(true);
              setWorkspaceError("");
              try {
                await headerExtension.onCreateWorkspace(workspaceName.trim());
              } catch (error) {
                setWorkspaceError(error instanceof Error ? error.message : "Unable to create workspace");
                setCreatingWorkspace(false);
              }
            }}
          >
            <DialogHeader>
              <DialogTitle>Create workspace</DialogTitle>
              <DialogDescription>Create an isolated tenant for a team, product, or business unit.</DialogDescription>
            </DialogHeader>
            <div className="space-y-4 py-5">
              <label className="block space-y-2 text-xs font-medium">
                Workspace name
                <Input autoFocus value={workspaceName} onChange={(event) => setWorkspaceName(event.target.value)} placeholder="Acme Security" minLength={2} maxLength={80} required />
              </label>
              <div className="rounded-lg border border-border-subtle bg-surface-1 p-3 text-[11px] leading-relaxed text-muted-foreground">
                This workspace gets its own data boundary, members, API credentials, usage, and billing. A 14-day trial starts when it is created.
              </div>
              {workspaceError ? <p className="text-xs text-danger">{workspaceError}</p> : null}
            </div>
            <DialogFooter>
              <Button type="button" variant="outline" onClick={() => setWorkspaceDialogOpen(false)}>Cancel</Button>
              <Button type="submit" disabled={creatingWorkspace || workspaceName.trim().length < 2}>{creatingWorkspace ? "Creating…" : "Create workspace"}</Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>

      <Dialog open={environmentDialogOpen} onOpenChange={setEnvironmentDialogOpen}>
        <DialogContent className="sm:max-w-md">
          <form
            onSubmit={async (event) => {
              event.preventDefault();
              if (creatingEnvironment) return;
              setCreatingEnvironment(true);
              setEnvironmentError("");
              try {
                await controls.createEnvironment({ name: environmentName, kind: environmentKind, description: environmentDescription });
                setCreatingEnvironment(false);
                setEnvironmentDialogOpen(false);
              } catch (error) {
                setEnvironmentError(error instanceof Error ? error.message : "Unable to create environment");
                setCreatingEnvironment(false);
              }
            }}
          >
            <DialogHeader>
              <DialogTitle>Create environment</DialogTitle>
              <DialogDescription>Register a stable deployment boundary for filtering telemetry and security events.</DialogDescription>
            </DialogHeader>
            <div className="space-y-4 py-5">
              <label className="block space-y-2 text-xs font-medium">
                Environment name
                <Input autoFocus value={environmentName} onChange={(event) => setEnvironmentName(event.target.value.toLowerCase())} placeholder="production-eu" pattern="[a-z][a-z0-9_-]{0,31}" maxLength={32} required />
              </label>
              <label className="block space-y-2 text-xs font-medium">
                Classification
                <Select value={environmentKind} onValueChange={(value) => value && setEnvironmentKind(value as typeof environmentKind)}>
                  <SelectTrigger className="w-full"><span className="capitalize">{environmentKind}</span></SelectTrigger>
                  <SelectContent>
                    {(["production", "staging", "development", "custom"] as const).map((kind) => <SelectItem key={kind} value={kind} className="capitalize">{kind}</SelectItem>)}
                  </SelectContent>
                </Select>
              </label>
              <label className="block space-y-2 text-xs font-medium">
                Description <span className="font-normal text-muted-foreground">optional</span>
                <Input value={environmentDescription} onChange={(event) => setEnvironmentDescription(event.target.value)} placeholder="Customer-facing EU workloads" maxLength={240} />
              </label>
              <p className="text-[10px] leading-relaxed text-muted-foreground">Send this exact value as the environment trace attribute. Existing observed values remain available until you adopt them.</p>
              {environmentError ? <p className="text-xs text-danger">{environmentError}</p> : null}
            </div>
            <DialogFooter>
              <Button type="button" variant="outline" onClick={() => setEnvironmentDialogOpen(false)}>Cancel</Button>
              <Button type="submit" disabled={creatingEnvironment || environmentName.length < 1}>{creatingEnvironment ? "Creating…" : "Create environment"}</Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>

      <ConnectCliDialog open={connectCliOpen} onOpenChange={setConnectCliOpen} />
    </div>
  );
}

type LayoutSelectItem = {
  value: string;
  label: string;
  detail?: string;
  action?: boolean;
  icon?: ReactNode;
};

function SidebarContextSelect({
  value,
  onChange,
  eyebrow,
  displayValue,
  items,
  icon,
  status = false,
  disabled = false,
  collapsed,
  placement,
  ariaLabel,
}: {
  value: string;
  onChange: (value: string) => void;
  eyebrow: string;
  displayValue: string;
  items: LayoutSelectItem[];
  icon: ReactNode;
  status?: boolean;
  disabled?: boolean;
  collapsed: boolean;
  placement: "top" | "bottom";
  ariaLabel: string;
}) {
  return (
    <div
      className={cn(
        "flex flex-shrink-0 items-center transition-[padding] duration-200",
        placement === "top" ? "border-b border-border-subtle py-1.5" : "border-t border-border-subtle bg-surface-1/30 py-1.5",
        collapsed ? "justify-center px-2" : "px-2",
      )}
    >
      <Select value={value} onValueChange={(next) => next && onChange(next)} disabled={disabled}>
        <SelectTrigger
          aria-label={ariaLabel}
          title={collapsed ? `${eyebrow}: ${displayValue}` : undefined}
          className={cn(
            "border-transparent bg-transparent text-left shadow-none transition-[width,height,padding,background-color] hover:border-transparent hover:bg-surface-2",
            collapsed
              ? "h-9 w-9 justify-center rounded-md p-0 data-[size=default]:h-9 [&>svg:last-child]:hidden"
              : "h-11 w-full min-w-0 rounded-md px-2 py-1 data-[size=default]:h-11",
          )}
        >
          {collapsed ? (
            <span className="relative flex h-5 w-5 items-center justify-center text-muted-foreground">
              {icon}
              {status ? <span className="absolute -bottom-0.5 -right-0.5 h-1.5 w-1.5 rounded-full border border-background bg-success" /> : null}
            </span>
          ) : (
            <span className="flex min-w-0 flex-1 items-center gap-2.5">
              <span className="relative flex h-7 w-7 flex-shrink-0 items-center justify-center rounded-md bg-surface-2 text-muted-foreground">
                {icon}
                {status ? <span className="absolute -bottom-0.5 -right-0.5 h-1.5 w-1.5 rounded-full border border-background bg-success" /> : null}
              </span>
              <span className="min-w-0 flex-1">
                <span className="block text-[9px] font-medium uppercase leading-3 tracking-widest text-muted-foreground/70">{eyebrow}</span>
                <span className="block truncate text-[11px] font-medium leading-4 text-foreground">{displayValue}</span>
              </span>
            </span>
          )}
        </SelectTrigger>
        <SelectContent align="start" side={placement === "bottom" ? "top" : "bottom"} className="min-w-[252px] p-1">
          {items.map((item) => (
            <SelectItem key={item.value} value={item.value} className="py-2">
              <span className="flex min-w-0 items-start gap-2">
                {item.icon ?? (item.action ? <Plus className="mt-0.5 h-3.5 w-3.5 flex-shrink-0" /> : null)}
                <span className="min-w-0">
                  <span className="block truncate text-xs font-medium">{item.label}</span>
                  {item.detail ? <span className="block truncate text-[10px] text-muted-foreground">{item.detail}</span> : null}
                </span>
              </span>
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    </div>
  );
}

function HeaderSelect({
  value,
  onChange,
  eyebrow,
  displayValue,
  items,
  status = false,
  disabled = false,
  className,
  ariaLabel,
}: {
  value: string;
  onChange: (value: string) => void;
  eyebrow: string;
  displayValue: string;
  items: LayoutSelectItem[];
  status?: boolean;
  disabled?: boolean;
  className?: string;
  ariaLabel: string;
}) {
  return (
    <Select value={value} onValueChange={(next) => next && onChange(next)} disabled={disabled}>
      <SelectTrigger aria-label={ariaLabel} className={cn("h-9 min-w-[126px] max-w-[180px] rounded-md border-border-subtle bg-surface-1 px-2.5 py-0 text-left hover:border-border-default", className)}>
        {status ? <span className="h-1.5 w-1.5 flex-shrink-0 rounded-full bg-success" /> : null}
        <span className="min-w-0 flex-1">
          <span className="block text-[9px] leading-3 text-muted-foreground">{eyebrow}</span>
          <span className="block truncate text-[11px] font-medium leading-4 text-foreground">{displayValue}</span>
        </span>
      </SelectTrigger>
      <SelectContent align="start" className="min-w-[220px]">
        {items.map((item) => (
          <SelectItem key={item.value} value={item.value} className="py-2">
            <span className="flex min-w-0 items-start gap-2">
              {item.icon ?? (item.action ? <Plus className="mt-0.5 h-3.5 w-3.5 flex-shrink-0" /> : null)}
              <span className="min-w-0">
              <span className="block truncate text-xs font-medium">{item.label}</span>
              {item.detail ? <span className="block truncate text-[10px] text-muted-foreground">{item.detail}</span> : null}
              </span>
            </span>
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}

function ConnectionStatus({
  health,
  connected,
  mode,
  statusPageURL,
}: {
  health: ReturnType<typeof useQuery<Awaited<ReturnType<typeof api.health>>>>;
  connected: boolean;
  mode: "dependencies" | "platform";
  statusPageURL?: string;
}) {
  const [open, setOpen] = useState(false);
  const wrapperRef = useRef<HTMLDivElement>(null);
  const unavailable = health.isError;

  useEffect(() => {
    if (!open) return;
    const close = (event: MouseEvent) => {
      if (wrapperRef.current && !wrapperRef.current.contains(event.target as Node)) setOpen(false);
    };
    document.addEventListener("mousedown", close);
    return () => document.removeEventListener("mousedown", close);
  }, [open]);

  const label = unavailable ? "Offline" : connected ? (mode === "platform" ? "Operational" : "Connected") : "Checking";
  const details = [
    ["Postgres", health.data?.postgres],
    ["Redis", health.data?.redis],
    ["ClickHouse", health.data?.clickhouse],
  ] as const;

  return (
    <div ref={wrapperRef} className="relative hidden xl:block">
      <button
        type="button"
        onClick={() => setOpen((value) => !value)}
        aria-expanded={open}
        className="flex h-8 items-center gap-2 rounded-md border border-border-subtle bg-surface-1 px-2.5 text-[11px] transition-colors hover:border-border-default"
      >
        <span className={cn("h-1.5 w-1.5 rounded-full", unavailable ? "bg-danger" : connected ? "bg-success" : "bg-warn")} />
        <span>{label}</span>
      </button>
      {open ? (
        <div className="absolute right-0 top-10 z-50 w-64 rounded-lg border border-border-default bg-popover p-2 shadow-md">
          <div className="flex items-start justify-between gap-3 px-2 py-1.5">
            <div>
              <p className="text-xs font-semibold tracking-tight text-foreground">{mode === "platform" ? "Bastio Cloud status" : "Gateway status"}</p>
              <p className="mt-0.5 text-[10px] text-muted-foreground">
                {health.dataUpdatedAt ? `Checked ${new Date(health.dataUpdatedAt).toLocaleTimeString()}` : "Waiting for health check"}
              </p>
            </div>
            <span className={cn("rounded-full px-2 py-0.5 text-[9px] font-medium", unavailable ? "bg-danger/10 text-danger" : connected ? "bg-success/10 text-success" : "bg-warn/10 text-warn")}>{label}</span>
          </div>
          <div className="my-2 h-px bg-border-subtle" />
          {mode === "dependencies" ? <div className="space-y-0.5">
            {details.map(([name, value]) => (
              <div key={name} className="flex items-center justify-between rounded-md px-2 py-1.5 text-[10px] hover:bg-surface-2">
                <span className="text-muted-foreground">{name}</span>
                <span className={cn("font-mono", value === "healthy" || value === "ok" ? "text-success" : "text-foreground")}>{value || "—"}</span>
              </div>
            ))}
          </div> : (
            <div className="rounded-md bg-surface-1 px-2.5 py-2 text-[10px] leading-relaxed text-muted-foreground">
              <div className="mb-1 flex items-center justify-between"><span>Bastio API</span><span className={connected ? "text-success" : "text-warn"}>{connected ? "Operational" : "Checking"}</span></div>
              Infrastructure dependencies and internal topology are monitored privately.
            </div>
          )}
          {unavailable ? <p className="px-2 py-1.5 text-[10px] leading-relaxed text-danger">The dashboard cannot reach the gateway health endpoint.</p> : null}
          <button type="button" onClick={() => void health.refetch()} className="mt-2 flex h-8 w-full items-center justify-center rounded-md border border-border-subtle text-[10px] font-medium transition-colors hover:bg-surface-2">Check again</button>
          {mode === "platform" && statusPageURL ? <a href={statusPageURL} target="_blank" rel="noreferrer" className="mt-1 flex h-8 w-full items-center justify-center text-[10px] font-medium text-muted-foreground hover:text-foreground">Open public status page <ChevronRight className="ml-1 h-3 w-3" /></a> : null}
        </div>
      ) : null}
    </div>
  );
}

function GlobalNavigation({
  currentPath,
  collapsed,
  navExt,
  onExpand,
  theme,
  toggleTheme,
  onNavigate,
}: {
  currentPath: string;
  collapsed: boolean;
  navExt: ReturnType<typeof useLayoutNav>;
  onExpand: () => void;
  theme: "light" | "dark";
  toggleTheme: () => void;
  onNavigate?: () => void;
}) {
  return (
    <>
      {collapsed ? (
        <div className="flex h-full flex-col items-center py-3">
          <button type="button" onClick={onExpand} className="mb-3 flex h-8 w-8 items-center justify-center rounded-md text-muted-foreground hover:bg-surface-2 hover:text-foreground" aria-label="Expand navigation">
            <ChevronRight className="h-4 w-4" />
          </button>
          <nav className="flex flex-1 flex-col items-center gap-1 overflow-y-auto">
            {navSections.map((section) =>
              section.items.map(({ to, label, icon: Icon }) => {
                const active = currentPath === to;
                return (
                  <Link key={to} to={to as never} title={label} onClick={onNavigate} className={cn("flex h-9 w-9 items-center justify-center rounded-md text-muted-foreground hover:bg-surface-2 hover:text-foreground", active && "bg-surface-2 text-foreground")}>
                    <Icon className="h-4 w-4" />
                  </Link>
                );
              }),
            )}
          </nav>
          <Button variant="ghost" size="icon-sm" onClick={toggleTheme} aria-label="Toggle theme">
            {theme === "dark" ? <Sun className="h-3.5 w-3.5" /> : <Moon className="h-3.5 w-3.5" />}
          </Button>
        </div>
      ) : (
        <>
          <nav className="flex-1 space-y-4 overflow-y-auto px-3 py-3">
            {navSections.map((section) => (
              <Fragment key={section.label}>
                <div>
                  <p className="mb-1.5 px-2 text-[10px] font-semibold uppercase tracking-widest text-muted-foreground/60">{section.label}</p>
                  <div className="space-y-0.5">
                    {section.items.map(({ to, label, icon: Icon }) => {
                      const active = currentPath === to;
                      return (
                        <Link key={to} to={to as never} onClick={onNavigate} className={cn("flex items-center gap-2.5 rounded-md px-2.5 py-[7px] text-[12px] transition-colors", active ? "bg-surface-2 font-medium text-foreground" : "text-muted-foreground hover:bg-surface-2/70 hover:text-foreground")}>
                          <Icon className="h-4 w-4 flex-shrink-0" /> {label}
                        </Link>
                      );
                    })}
                  </div>
                </div>
                {(navExt.sectionsAfter?.[section.label.toLowerCase()] ?? []).map((item) => renderExtSection(item, currentPath))}
              </Fragment>
            ))}
            {navExt.sections?.map((section) => renderExtSection(section, currentPath))}
          </nav>
          <Separator className="opacity-50" />
          <div className="flex items-center justify-between px-5 py-2.5">
            <a href={DOCS_URL} target="_blank" rel="noopener noreferrer" className="group flex items-center gap-1.5 text-[11px] text-muted-foreground transition-colors hover:text-foreground">
              <BookOpen className="h-3 w-3" /> Docs
              <ExternalLink className="h-2.5 w-2.5 opacity-50 transition-opacity group-hover:opacity-100" aria-hidden="true" />
              <span className="sr-only">(opens in a new tab)</span>
            </a>
            <Button variant="ghost" size="icon" className="h-6 w-6" onClick={toggleTheme} aria-label="Toggle theme">
              {theme === "dark" ? <Sun className="h-3 w-3" /> : <Moon className="h-3 w-3" />}
            </Button>
          </div>
        </>
      )}
    </>
  );
}

function guessEnvironmentKind(name: string): "production" | "staging" | "development" | "custom" {
  const value = name.toLowerCase();
  if (value.includes("prod")) return "production";
  if (value.includes("stag") || value.includes("preprod")) return "staging";
  if (value.includes("dev") || value.includes("local")) return "development";
  return "custom";
}

function renderExtSection(section: LayoutNavSection, currentPath: string) {
  return (
    <div key={`ext:${section.label}`}>
      <p className="mb-1.5 px-2 text-[10px] font-semibold uppercase tracking-widest text-muted-foreground/60">{section.label}</p>
      <div className="space-y-0.5">
        {section.items.map(({ to, label, icon: Icon }) => {
          const active = currentPath === to;
          return (
            <Link key={to} to={to as never} className={cn("flex items-center gap-2.5 rounded-md px-2.5 py-[7px] text-[12px] transition-colors", active ? "bg-surface-2 font-medium text-foreground" : "text-muted-foreground hover:bg-surface-2/70 hover:text-foreground")}>
              <Icon className="h-4 w-4 flex-shrink-0" /> {label}
            </Link>
          );
        })}
      </div>
    </div>
  );
}
