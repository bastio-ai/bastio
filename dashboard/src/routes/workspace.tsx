import { useCallback, useEffect, useMemo, useState } from "react";
import { Link } from "@tanstack/react-router";
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

import { PageHeader } from "@/components/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { useWorkspaceSidebar } from "@/components/workspace-sidebar-context";
import { DashboardTab } from "@/components/workspace/dashboard-tab";
import { AssistantsTab } from "@/components/workspace/assistants-tab";
import { KnowledgeTab } from "@/components/workspace/knowledge-tab";
import { SettingsTab } from "@/components/workspace/settings-tab";
import { useWorkspaceExtension } from "@/components/workspace/extension-context";

type AdminTab = "dashboard" | "assistants" | "knowledge" | "settings";

const BASE_TABS: { id: AdminTab; label: string; icon: LucideIcon }[] = [
  { id: "dashboard", label: "Overview", icon: LayoutDashboard },
  { id: "assistants", label: "Assistants", icon: Bot },
  { id: "knowledge", label: "Knowledge", icon: BookOpen },
  { id: "settings", label: "AI controls", icon: Settings2 },
];

const TAB_META: Record<AdminTab, { title: string; description: string }> = {
  dashboard: {
    title: "Private AI Portal",
    description: "Operate the secure AI workspace your team uses every day.",
  },
  assistants: {
    title: "Assistants",
    description: "Define trusted AI roles, model defaults, and approved knowledge access.",
  },
  knowledge: {
    title: "Knowledge",
    description: "Manage the governed sources assistants can retrieve and cite.",
  },
  settings: {
    title: "AI controls",
    description: "Set the shared AI identity and control which models employees can use.",
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
  const { extraTabs, openWorkspaceURL, hideUpsell } = useWorkspaceExtension();
  const [tab, setTab] = useState(() => {
    const candidate = new URL(window.location.href).searchParams.get("view");
    return candidate || "dashboard";
  });
  const [showSubscribedToast, setShowSubscribedToast] = useState(false);

  const allTabs = useMemo(
    () => [
      ...BASE_TABS,
      ...extraTabs.map((item) => ({
        ...item,
        icon: EXTRA_TAB_ICONS[item.id] ?? ShieldCheck,
      })),
    ],
    [extraTabs],
  );

  useEffect(() => {
    if (!allTabs.some((item) => item.id === tab)) setTab("dashboard");
  }, [allTabs, tab]);

  useEffect(() => {
    const url = new URL(window.location.href);
    if (url.searchParams.get("subscribed") === "1") {
      setShowSubscribedToast(true);
      url.searchParams.delete("subscribed");
      window.history.replaceState({}, "", url.toString());
    }
  }, []);

  const changeTab = useCallback((nextTab: string) => {
    setTab(nextTab);
    const url = new URL(window.location.href);
    if (nextTab === "dashboard") url.searchParams.delete("view");
    else url.searchParams.set("view", nextTab);
    window.history.replaceState({}, "", url.toString());
  }, []);

  const activeTab = allTabs.find((item) => item.id === tab) ?? allTabs[0]!;
  const activeMeta = TAB_META[tab as AdminTab] ?? {
    title: activeTab.label,
    description: "Configure and govern your private AI workspace.",
  };

  const sidebarConfig = useMemo(
    () => ({
      parentLabel: "Bastio",
      parentTo: "/",
      title: "Private AI Portal",
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
        <div className="rounded-lg border border-border-subtle bg-surface-1 px-3 py-2.5">
          <div className="flex items-center gap-2 text-[11px] font-medium text-foreground">
            <span className="h-1.5 w-1.5 rounded-full bg-success" />
            Portal available
          </div>
          <p className="mt-1 text-[10px] leading-relaxed text-muted-foreground">
            Team chats are protected by the same gateway policies and audit trail as API traffic.
          </p>
        </div>
      ),
    }),
    [activeTab.icon, activeTab.label, allTabs, changeTab, tab],
  );
  useWorkspaceSidebar(sidebarConfig);

  return (
    <div className="mx-auto w-full max-w-[1560px] px-4 py-4 sm:px-5 lg:px-6 lg:py-5">
      {showSubscribedToast ? (
        <SubscribedToast onDismiss={() => setShowSubscribedToast(false)} />
      ) : null}

      <PageHeader
        title={activeMeta.title}
        description={activeMeta.description}
        badge={
          <Badge variant={hideUpsell ? "success" : "outline"} className="font-mono text-[10px]">
            {hideUpsell ? "managed" : "self-hosted"}
          </Badge>
        }
        action={<OpenWorkspaceLink openWorkspaceURL={openWorkspaceURL} />}
      />

      {tab === "dashboard" ? (
        <>
          <DashboardTab onOpenChat={() => navigateToChat(openWorkspaceURL)} />
          {!hideUpsell ? <CloudUpsellCard /> : null}
        </>
      ) : null}
      {tab === "assistants" ? <AssistantsTab /> : null}
      {tab === "knowledge" ? <KnowledgeTab /> : null}
      {tab === "settings" ? <SettingsTab /> : null}
      {extraTabs.map((item) =>
        tab === item.id ? <div key={item.id}>{item.component}</div> : null,
      )}
    </div>
  );
}

function OpenWorkspaceLink({ openWorkspaceURL }: { openWorkspaceURL: string | null }) {
  if (openWorkspaceURL) {
    return (
      <Button nativeButton={false} render={<a href={openWorkspaceURL} target="_blank" rel="noopener noreferrer" />}>
        Open portal
        <ExternalLink data-icon="inline-end" />
      </Button>
    );
  }

  return (
    <Button nativeButton={false} render={<Link to="/chat" />}>
      Open portal
      <MessageSquare data-icon="inline-end" />
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

function CloudUpsellCard() {
  return (
    <section className="mt-5 overflow-hidden rounded-xl border border-border/70 bg-card">
      <div className="grid lg:grid-cols-[minmax(0,1fr)_auto]">
        <div className="p-5">
          <div className="flex items-center gap-2">
            <ShieldCheck className="h-4 w-4 text-muted-foreground" />
            <p className="text-[11px] font-semibold uppercase tracking-[0.14em] text-muted-foreground">
              Managed controls
            </p>
          </div>
          <h2 className="mt-3 text-[15px] font-semibold tracking-tight text-foreground">
            Extend the portal with enterprise identity and governance.
          </h2>
          <p className="mt-1 max-w-3xl text-[12px] leading-relaxed text-muted-foreground">
            Bastio Cloud adds role-based access, SSO and SCIM, retained audit evidence, managed PII detection, and custom domains.
          </p>
        </div>
        <div className="flex items-center border-t border-border/60 px-5 py-4 lg:border-l lg:border-t-0">
          <Button
            variant="outline"
            nativeButton={false}
            render={<a href="https://bastio.com/cloud" target="_blank" rel="noopener noreferrer" />}
          >
            Explore managed workspace
            <ArrowRight data-icon="inline-end" />
          </Button>
        </div>
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
    <div className="mb-5 flex items-start gap-3 rounded-xl border border-success-border bg-success-bg p-4">
      <CheckCircle2 className="mt-0.5 h-4 w-4 shrink-0 text-success" />
      <div className="min-w-0 flex-1">
        <p className="text-[12px] font-medium text-foreground">Workspace governance connected</p>
        <p className="mt-0.5 text-[11px] text-muted-foreground">
          Configure assistants and access controls here, then open the portal to verify the employee experience.
        </p>
      </div>
      <Button variant="ghost" size="icon-xs" onClick={onDismiss} aria-label="Dismiss notification">
        <X />
      </Button>
    </div>
  );
}

export type { AdminTab };
