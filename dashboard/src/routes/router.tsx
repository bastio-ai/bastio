import { lazy, Suspense } from "react";
import {
  createRootRoute,
  createRoute,
  Outlet,
  redirect,
} from "@tanstack/react-router";
import { Layout } from "@/components/layout";
import { OverviewPage } from "./overview";
import { TracesPage } from "./traces";
import { TraceDetailPage } from "./trace-detail";
import { SessionsPage } from "./sessions";
import { SessionDetailPage } from "./session-detail";
import { UsersPage } from "./users";
import { UserDetailPage } from "./user-detail";
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
import { ChatsPage } from "./chats";
import { CompliancePage } from "./compliance";
import { WorkspacesPage } from "./workspaces";

// Workspace admin console — configure assistants, knowledge, settings.
// The chat surface is also reachable in-app at /workspace/chat for OSS
// (single-tenant) deployments; cloud deployments override the "Open
// Workspace" link via WorkspaceExtension.openWorkspaceURL to point at
// the dedicated employee SPA at workspace.bastio.com.
const WorkspacePage = lazy(() =>
  import("./workspace").then((m) => ({ default: m.WorkspacePage })),
);

const WorkspaceChatPage = lazy(() =>
  import("./workspace-chat").then((m) => ({ default: m.WorkspaceChatPage })),
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

function WorkspaceChatLazyRoute() {
  return (
    <Suspense
      fallback={
        <div className="flex h-screen items-center justify-center text-sm text-muted-foreground">
          Loading chat…
        </div>
      }
    >
      <WorkspaceChatPage />
    </Suspense>
  );
}

import { NotFound } from "@/components/not-found";

const rootRoute = createRootRoute({
  component: () => (
    <Layout>
      <Outlet />
    </Layout>
  ),
  notFoundComponent: NotFound,
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
  beforeLoad: () => {
    throw redirect({ to: "/workspace" });
  },
  component: () => null,
});

const promptDetailRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/prompts/$name",
  beforeLoad: () => {
    throw redirect({ to: "/workspace" });
  },
  component: () => null,
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

// Renamed from /security to /security-settings as part of the
// Security Center IA refactor (BAS-28). The tab structure inside
// SecurityPage now hosts not only the OSS detector configuration
// (the original /security content) but also any cloud-only tabs
// injected via SecurityExtensionProvider, so "settings" is a more
// honest noun than "center" for the URL slug.
const securityRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/security-settings",
  component: SecurityPage,
});

// Backward-compat redirect for the old /security URL. Anyone with a
// runbook link or browser bookmark lands on /security-settings
// automatically. Pure redirect — no component, no fallback.
const securityRedirectRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/security",
  beforeLoad: () => {
    throw redirect({ to: "/security-settings" });
  },
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

const workspacesRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/workspaces",
  component: WorkspacesPage,
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

const workspaceRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/workspace",
  component: WorkspaceLazyRoute,
});

// Path is /chat at the root, not /workspace/chat — TanStack Router
// treats sibling literal-segment routes that share a prefix
// (`/workspace` + `/workspace/chat`) ambiguously and routes the longer
// one to the index. /chat as a top-level route avoids the collision
// and reads cleaner in the URL bar anyway.
const workspaceChatRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/chat",
  component: WorkspaceChatLazyRoute,
});

// Paginated all-chats list. Linked from chat-tab's "View all chats"
// footer when the recent-only sidebar runs out of room.
const chatsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/chats",
  component: ChatsPage,
});

// Conversation deep link. /c/<uuid> renders the same chat surface as
// /chat; chat-tab reads readConversationIDFromPath() on mount and
// seeds the active conversation. Matches the in-app pushState URL the
// chat-tab uses when a user clicks a thread in the sidebar, so a
// row click on /chats opens the right thread without a hard reload.
const conversationDeepLinkRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/c/$id",
  component: WorkspaceChatLazyRoute,
});

import { ProfilePage } from "./profile";

const profileRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/profile",
  component: ProfilePage,
});

import { CachePage } from "./cache";

const cacheRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/cache",
  component: CachePage,
});

const complianceRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/compliance",
  component: CompliancePage,
});

const billingRedirectRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/billing",
  beforeLoad: () => {
    throw redirect({ to: "/settings" });
  },
  component: () => null,
});

// rootRoute + ossChildRoutes are exported so bastio-cloud's dashboard
// can compose its own route tree without duplicating this list.
export { rootRoute };

export const ossChildRoutes = [
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
  securityRedirectRoute,
  playgroundRoute,
  apiKeysRoute,
  workspacesRoute,
  profileRoute,
  complianceRoute,
  cacheRoute,
  settingsRoute,
  overlaysRoute,
  overlayNewRoute,
  overlayDetailRoute,
  overlayVersionNewRoute,
  overlayTemplatesRoute,
  workspaceRoute,
  workspaceChatRoute,
  chatsRoute,
  conversationDeepLinkRoute,
];

export const routeTree = rootRoute.addChildren([...ossChildRoutes, billingRedirectRoute]);
