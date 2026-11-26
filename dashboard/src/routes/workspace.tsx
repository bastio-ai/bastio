import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Link, useNavigate, useSearch } from "@tanstack/react-router";
import {
  ArrowRight,
  BarChart3,
  Bot,
  BookOpen,
  CheckCircle2,
  ExternalLink,
  Globe2,
  LayoutDashboard,
  MessageSquare,
  Plug,
  ScrollText,
  Settings2,
  ShieldCheck,
  Users,
  X,
  type LucideIcon,
} from "lucide-react";

import {
  AdminPageHeader,
  AdminUnderlineNav,
} from "@/components/admin/admin-primitives";
import { Button } from "@/components/ui/button";
import { useWorkspaceSidebar } from "@/components/workspace-sidebar-context";
import { DashboardTab } from "@/components/workspace/dashboard-tab";
import { AssistantsTab } from "@/components/workspace/assistants-tab";
import { MCPIntegrations } from "@/components/workspace/mcp-integrations";
import { KnowledgeTab } from "@/components/workspace/knowledge-tab";
import { AccessControls } from "@/components/workspace/access-controls";
import type {
  SettingsSection,
  WorkspaceSearch,
} from "@/components/workspace/navigation";
import { SettingsTab } from "@/components/workspace/settings-tab";
import { useWorkspaceExtension } from "@/components/workspace/extension-context";

type AdminTab =
  | "dashboard"
  | "assistants"
  | "knowledge"
  | "integrations"
  | "groups"
  | "settings";

const BASE_TABS: { id: AdminTab; label: string; icon: LucideIcon }[] = [
  { id: "dashboard", label: "Overview", icon: LayoutDashboard },
  { id: "assistants", label: "Assistants", icon: Bot },
  { id: "knowledge", label: "Knowledge", icon: BookOpen },
  { id: "integrations", label: "Integrations", icon: Plug },
  { id: "groups", label: "Groups & sharing", icon: Users },
  { id: "settings", label: "Workspace settings", icon: Settings2 },
];

const TAB_META: Record<AdminTab, { title: string; description: string }> = {
  dashboard: {
    title: "Employee workspace",
    description:
      "Set up your team’s governed AI workspace, manage access, and track usage.",
  },
  assistants: {
    title: "Assistants",
    description:
      "Define trusted AI roles, model defaults, and approved knowledge access.",
  },
  knowledge: {
    title: "Knowledge",
    description:
      "Manage the governed sources assistants can retrieve and cite.",
  },
  integrations: {
    title: "MCP integrations",
    description:
      "Share protected tools with your team and manage personal connections.",
  },
  groups: {
    title: "Groups & sharing",
    description:
      "Organize teams such as Marketing or Legal and choose who can use shared resources.",
  },
  settings: {
    title: "Workspace settings",
    description:
      "Manage your workspace identity, model access, privacy, and spending.",
  },
};

const EXTRA_TAB_ICONS: Record<string, LucideIcon> = {
  team: Users,
  integrations: Plug,
  "audit-log": ScrollText,
  audit: ScrollText,
  analytics: BarChart3,
  "custom-domains": Globe2,
  domains: Globe2,
};

export function WorkspacePage() {
  const pageRef = useRef<HTMLDivElement>(null);
  const { extraTabs, openWorkspaceURL, hideUpsell } = useWorkspaceExtension();
  const navigate = useNavigate();
  const search = useSearch({ strict: false }) as WorkspaceSearch;
  const section = search.section ?? "general";
  const [showSubscribedToast, setShowSubscribedToast] = useState(false);

  const allTabs = useMemo(
    () =>
      [
        ...BASE_TABS,
        ...extraTabs.map((item) => ({
          ...item,
          icon: EXTRA_TAB_ICONS[item.id] ?? ShieldCheck,
        })),
      ].sort((a, b) => {
        const order = [
          "dashboard",
          "team",
          "groups",
          "assistants",
          "knowledge",
          "integrations",
          "settings",
          "domains",
          "analytics",
          "audit",
        ];
        const rank = (id: string) => {
          const i = order.indexOf(id);
          return i < 0 ? order.length : i;
        };
        return rank(a.id) - rank(b.id);
      }),
    [extraTabs],
  );

  const tab = allTabs.some((item) => item.id === search.view)
    ? search.view!
    : "dashboard";

  useEffect(() => {
    pageRef.current?.scrollIntoView({ block: "start" });
  }, [tab, section]);

  useEffect(() => {
    if (search.subscribed === "1") {
      setShowSubscribedToast(true);
      void navigate({
        to: "/workspace",
        search: { ...search, subscribed: undefined },
        replace: true,
      });
    }
  }, [navigate, search]);

  const changeTab = useCallback(
    (nextTab: string, nextSection?: SettingsSection) => {
      void navigate({
        to: "/workspace",
        search: {
          view: nextTab === "dashboard" ? undefined : nextTab,
          section: nextTab === "settings" ? nextSection : undefined,
        },
      });
    },
    [navigate],
  );

  const activeTab = allTabs.find((item) => item.id === tab) ?? allTabs[0]!;
  const extraMeta: Record<string, { title: string; description: string }> = {
    team: {
      title: "Members",
      description:
        "Invite employees, assign roles, and set individual usage limits. Use Groups & sharing to organize teams.",
    },
    domains: {
      title: "Custom domains",
      description: "Give employees a workspace address on your own domain.",
    },
    analytics: {
      title: "Usage analytics",
      description:
        "Understand how your team uses AI and where costs come from.",
    },
    audit: {
      title: "Audit log",
      description: "Review workspace actions and security events.",
    },
  };
  const activeMeta = extraMeta[tab] ??
    TAB_META[tab as AdminTab] ?? {
      title: activeTab.label,
      description: "Configure and govern your private AI workspace.",
    };

  const sidebarConfig = useMemo(
    () => ({
      parentLabel: "Bastio",
      parentTo: "/",
      title: "Workspace",
      activeLabel: activeTab.label,
      activeIcon: activeTab.icon,
      hideActiveItem: true,
      views: allTabs.map((item) => ({
        label: item.label,
        icon: item.icon,
        active: item.id === tab,
        onClick: () => changeTab(item.id),
      })),
      filtersLabel: "Workspace",
      filters: (
        <div className="border-l-2 border-border-subtle pl-3 py-1">
          <div className="flex items-center gap-2 text-[11px] font-medium text-foreground">
            <span
              aria-hidden="true"
              className="size-1.5 rounded-full bg-success"
            />
            Employee workspace
          </div>
          <p className="mt-1 text-[11px] leading-relaxed text-muted-foreground">
            Manage governance here. Open the employee workspace to test chat as
            your team sees it.
          </p>
        </div>
      ),
    }),
    [activeTab.icon, activeTab.label, allTabs, changeTab, tab],
  );
  useWorkspaceSidebar(sidebarConfig);

  return (
    <div
      ref={pageRef}
      className="mx-auto max-w-[1180px] pb-10 pt-2 md:pt-4"
    >
      {showSubscribedToast ? (
        <SubscribedToast onDismiss={() => setShowSubscribedToast(false)} />
      ) : null}

      <AdminPageHeader
        eyebrow="Workspace & governance"
        title={activeMeta.title}
        description={activeMeta.description}
        bordered={false}
        className="mb-4"
        badge={
          <span className="inline-flex items-center gap-1.5 font-mono text-[11px] text-muted-foreground">
            <span
              aria-hidden="true"
              className="size-1.5 rounded-full bg-success"
            />
            {hideUpsell ? "managed" : "self-hosted"}
          </span>
        }
        actions={<OpenWorkspaceLink openWorkspaceURL={openWorkspaceURL} />}
      />

      <AdminUnderlineNav
        ariaLabel="Workspace sections"
        value={tab}
        onChange={(next) => changeTab(next)}
        items={allTabs.map((item) => ({ id: item.id, label: item.label }))}
      />

      {tab === "dashboard" ? (
        <>
          <DashboardTab
            onOpenChat={() => navigateToChat(openWorkspaceURL)}
            onOpenTab={changeTab}
          />
          {!hideUpsell ? <CloudUpsellSection /> : null}
        </>
      ) : null}
      {tab === "assistants" ? <AssistantsTab /> : null}
      {tab === "knowledge" ? <KnowledgeTab /> : null}
      {tab === "integrations" ? <MCPIntegrations /> : null}
      {tab === "groups" ? <AccessControls /> : null}
      {tab === "settings" ? (
        <SettingsTab
          section={section}
          onSectionChange={(next) => changeTab("settings", next)}
        />
      ) : null}
      {extraTabs.map((item) =>
        tab === item.id ? <div key={item.id}>{item.component}</div> : null,
      )}
    </div>
  );
}

function OpenWorkspaceLink({
  openWorkspaceURL,
}: {
  openWorkspaceURL: string | null;
}) {
  if (openWorkspaceURL) {
    return (
      <Button
        size="sm"
        className="h-8 text-xs"
        nativeButton={false}
        render={
          <a
            href={openWorkspaceURL}
            target="_blank"
            rel="noopener noreferrer"
          />
        }
      >
        Open employee workspace
        <ExternalLink className="size-3.5" aria-hidden="true" />
      </Button>
    );
  }

  return (
    <Button
      size="sm"
      className="h-8 text-xs"
      nativeButton={false}
      render={<Link to="/chat" />}
    >
      Open employee workspace
      <MessageSquare className="size-3.5" aria-hidden="true" />
    </Button>
  );
}

function navigateToChat(openWorkspaceURL: string | null) {
  if (openWorkspaceURL) {
    window.open(openWorkspaceURL, "_blank", "noopener,noreferrer");
    return;
  }
  window.location.assign("/chat");
}

function CloudUpsellSection() {
  return (
    <section className="mt-8 flex flex-col justify-between gap-4 border-y border-border-subtle py-5 lg:flex-row lg:items-center">
      <div>
        <p className="text-[11px] text-muted-foreground">Managed controls</p>
        <h2 className="mt-1 text-[13px] font-medium text-foreground">
          Extend the portal with enterprise identity and governance
        </h2>
        <p className="mt-1 max-w-3xl text-xs leading-relaxed text-muted-foreground">
          Bastio Cloud adds role-based access, SSO and SCIM, retained audit
          evidence, managed PII detection, and custom domains.
        </p>
      </div>
      <div className="shrink-0">
        <Button
          variant="outline"
          size="sm"
          className="h-8 text-xs"
          nativeButton={false}
          render={
            <a
              href="https://bastio.com/cloud"
              target="_blank"
              rel="noopener noreferrer"
            />
          }
        >
          Explore managed workspace
          <ArrowRight className="size-3.5" aria-hidden="true" />
        </Button>
      </div>
    </section>
  );
}

function SubscribedToast({ onDismiss }: { onDismiss: () => void }) {
  useEffect(() => {
    const timer = setTimeout(onDismiss, 8000);
    return () => clearTimeout(timer);
  }, [onDismiss]);

  return (
    <div className="mb-6 flex items-start gap-3 border-l-2 border-success pl-3.5 py-1">
      <CheckCircle2
        className="mt-0.5 size-4 shrink-0 text-success"
        aria-hidden="true"
      />
      <div className="min-w-0 flex-1">
        <p className="text-xs font-medium text-foreground">
          Workspace governance connected
        </p>
        <p className="mt-0.5 text-xs text-muted-foreground">
          Configure assistants and access controls here, then open the portal to
          verify the employee experience.
        </p>
      </div>
      <Button
        type="button"
        variant="ghost"
        size="icon-xs"
        onClick={onDismiss}
        aria-label="Dismiss notification"
      >
        <X className="size-3.5" aria-hidden="true" />
      </Button>
    </div>
  );
}

export type { AdminTab };
