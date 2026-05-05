import { lazy, Suspense } from "react";
import {
  createRootRoute,
  createRoute,
  Outlet,
} from "@tanstack/react-router";
import { Layout } from "@/components/layout";
import { OverviewPage } from "./overview";
import { TracesPage } from "./traces";
import { TraceDetailPage } from "./trace-detail";
import { SessionsPage } from "./sessions";
import { SessionDetailPage } from "./session-detail";
import { UsersPage } from "./users";
import { UserDetailPage } from "./user-detail";
import { PromptsPage } from "./prompts";
import { PromptDetailPage } from "./prompt-detail";
import { ThreatsPage, validateThreatsSearch } from "./threats";
import { ThreatDetailPage } from "./threat-detail";
import { AnalyticsPage } from "./analytics";
import { ProxiesPage } from "./proxies";
import { ProxyDetailPage } from "./proxy-detail";
import { SecurityPage } from "./security";
import { PlaygroundPage } from "./playground";
import { ApiKeysPage } from "./api-keys";
import { SettingsPage } from "./settings";
import { OverlaysPage } from "./overlays";
import { OverlayDetailPage } from "./overlay-detail";
import { OverlayTemplatesPage } from "./overlay-templates";
import { OverlayNewPage } from "./overlays-new";
import { OverlayVersionNewPage } from "./overlay-version-new";

// Governance ships as its own chunk — pulls 8 tab components + a long policy
// editor + chart-shaped pilot report. Code-split at the route boundary so the
// initial bundle stays lean for users who never visit the Governance section.
const GovernancePage = lazy(() =>
  import("./governance").then((m) => ({ default: m.GovernancePage })),
);

function GovernanceRouteFallback() {
  return (
    <div className="flex h-screen items-center justify-center text-sm text-muted-foreground">
      Loading governance…
    </div>
  );
}

function GovernanceLazyRoute() {
  return (
    <Suspense fallback={<GovernanceRouteFallback />}>
      <GovernancePage />
    </Suspense>
  );
}

// Workspace admin console — configure assistants, team, knowledge,
// integrations, retention, billing. The chat product employees use
// is a separate Vite app at workspace.bastio.com (or a custom domain),
// not embedded in the admin dashboard.
const WorkspacePage = lazy(() =>
  import("./workspace").then((m) => ({ default: m.WorkspacePage })),
);

function WorkspaceLazyRoute() {
  return (
    <Suspense
      fallback={
        <div className="flex h-screen items-center justify-center text-sm text-muted-foreground">
          Loading workspace…
        </div>
      }
    >
      <WorkspacePage />
    </Suspense>
  );
}

const rootRoute = createRootRoute({
  component: () => (
    <Layout>
      <Outlet />
    </Layout>
  ),
});

const indexRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/",
  component: OverviewPage,
});

const tracesRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/traces",
  component: TracesPage,
});

const traceDetailRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/traces/$id",
  component: TraceDetailPage,
});

const sessionsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/sessions",
  component: SessionsPage,
});

const sessionDetailRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/sessions/$id",
  component: SessionDetailPage,
});

const usersRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/users",
  component: UsersPage,
});

const userDetailRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/users/$id",
  component: UserDetailPage,
});

const promptsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/prompts",
  component: PromptsPage,
});

const promptDetailRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/prompts/$name",
  component: PromptDetailPage,
});

const threatsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/threats",
  component: ThreatsPage,
  validateSearch: validateThreatsSearch,
});

const threatDetailRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/threats/$id",
  component: ThreatDetailPage,
});

const analyticsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/analytics",
  component: AnalyticsPage,
});

const proxiesRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/proxies",
  component: ProxiesPage,
});

const proxyDetailRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/proxies/$id",
  component: ProxyDetailPage,
});

const securityRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/security",
  component: SecurityPage,
});

const playgroundRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/playground",
  component: PlaygroundPage,
});

const apiKeysRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/api-keys",
  component: ApiKeysPage,
});

const settingsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/settings",
  component: SettingsPage,
});

const overlaysRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/overlays",
  component: OverlaysPage,
});

// /overlays/new must be registered before /overlays/$id so TanStack
// Router's literal-beats-dynamic ranking actually picks this up.
// Both literally work in any order, but declaring this one first
// makes the intent obvious.
type OverlayNewSearch = {
  template: string | undefined;
  from_threat: string | undefined;
};

const overlayNewRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/overlays/new",
  component: OverlayNewPage,
  validateSearch: (search: Record<string, unknown>): OverlayNewSearch => ({
    template:
      typeof search.template === "string" ? search.template : undefined,
    from_threat:
      typeof search.from_threat === "string" ? search.from_threat : undefined,
  }),
});

const overlayDetailRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/overlays/$id",
  component: OverlayDetailPage,
});

type OverlayVersionNewSearch = {
  from_threat: string | undefined;
};

const overlayVersionNewRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/overlays/$id/versions/new",
  component: OverlayVersionNewPage,
  validateSearch: (
    search: Record<string, unknown>,
  ): OverlayVersionNewSearch => ({
    from_threat:
      typeof search.from_threat === "string" ? search.from_threat : undefined,
  }),
});

const overlayTemplatesRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/overlay-templates",
  component: OverlayTemplatesPage,
});

const governanceRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/governance",
  component: GovernanceLazyRoute,
});

const workspaceRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/workspace",
  component: WorkspaceLazyRoute,
});

export const routeTree = rootRoute.addChildren([
  indexRoute,
  tracesRoute,
  traceDetailRoute,
  sessionsRoute,
  sessionDetailRoute,
  usersRoute,
  userDetailRoute,
  promptsRoute,
  promptDetailRoute,
  threatsRoute,
  threatDetailRoute,
  analyticsRoute,
  proxiesRoute,
  proxyDetailRoute,
  securityRoute,
  playgroundRoute,
  apiKeysRoute,
  settingsRoute,
  overlaysRoute,
  overlayNewRoute,
  overlayDetailRoute,
  overlayVersionNewRoute,
  overlayTemplatesRoute,
  governanceRoute,
  workspaceRoute,
]);
