import { useEffect, useState } from "react";
import { Link } from "@tanstack/react-router";
import { CheckCircle2, X, ExternalLink, ArrowRight, MessageSquare } from "lucide-react";

import { PageHeader } from "@/components/card";

import { DashboardTab } from "@/components/workspace/dashboard-tab";
import { AssistantsTab } from "@/components/workspace/assistants-tab";
import { KnowledgeTab } from "@/components/workspace/knowledge-tab";
import { SettingsTab } from "@/components/workspace/settings-tab";
import { useWorkspaceExtension } from "@/components/workspace/extension-context";

// AdminTab is the operator-facing view of Workspace. The chat surface
// itself is reachable in-app at /chat (OSS single-tenant);
// cloud deployments override the "Open Workspace" link via the
// extension context to point at workspace.bastio.com instead.
type AdminTab = "dashboard" | "assistants" | "knowledge" | "settings";

const BASE_TABS: { id: string; label: string }[] = [
  { id: "dashboard", label: "Dashboard" },
  { id: "assistants", label: "Assistants" },
  { id: "knowledge", label: "Knowledge Base" },
  { id: "settings", label: "Settings" },
];

// WorkspacePage is the ADMIN console for Workspace. Configure
// assistants, knowledge, integrations, settings.
//
// Cloud-dashboard injects extra tabs (Team, Audit Log, Analytics,
// Custom Domains) via WorkspaceExtensionProvider in its main.tsx —
// OSS source ships without those tabs because their backend
// (multi-user auth, billing, SSO) only exists in bastio-cloud.
export function WorkspacePage() {
  const { extraTabs, openWorkspaceURL, hideUpsell } = useWorkspaceExtension();

  const [tab, setTab] = useState<string>("dashboard");
  const [showSubscribedToast, setShowSubscribedToast] = useState(false);

  // Stripe Checkout success URL is /workspace?subscribed=1 — when the
  // query param is present we show a one-time toast and clean up the
  // URL so a refresh doesn't re-trigger the message.
  useEffect(() => {
    const url = new URL(window.location.href);
    if (url.searchParams.get("subscribed") === "1") {
      setShowSubscribedToast(true);
      url.searchParams.delete("subscribed");
      window.history.replaceState({}, "", url.toString());
    }
  }, []);

  const allTabs = [...BASE_TABS, ...extraTabs];

  return (
    <div className="space-y-6 p-6">
      {showSubscribedToast && (
        <SubscribedToast onDismiss={() => setShowSubscribedToast(false)} />
      )}
      <div className="flex items-start justify-between gap-4">
        <PageHeader
          title="Workspace"
          description="Configure the AI chat product your team uses. Assistants, knowledge bases, integrations, settings."
        />
        <OpenWorkspaceLink openWorkspaceURL={openWorkspaceURL} />
      </div>

      <div className="flex gap-1 overflow-x-auto border-b border-border">
        {allTabs.map((t) => (
          <button
            key={t.id}
            type="button"
            onClick={() => setTab(t.id)}
            className={`whitespace-nowrap border-b-2 px-4 py-2 text-sm transition ${
              tab === t.id
                ? "border-cyan-500 text-cyan-500"
                : "border-transparent text-muted-foreground hover:text-foreground"
            }`}
          >
            {t.label}
          </button>
        ))}
      </div>

      {tab === "dashboard" && (
        <>
          <DashboardTab onOpenChat={() => navigateToChat(openWorkspaceURL)} />
          {!hideUpsell && <CloudUpsellCard />}
        </>
      )}
      {tab === "assistants" && <AssistantsTab />}
      {tab === "knowledge" && <KnowledgeTab />}
      {tab === "settings" && <SettingsTab />}

      {/* Cloud-injected tabs render their own component below. */}
      {extraTabs.map(
        (et) => tab === et.id && <div key={et.id}>{et.component}</div>,
      )}
    </div>
  );
}

// OpenWorkspaceLink renders the "Open Workspace" affordance in the
// page header. In OSS (no openWorkspaceURL) it routes to the in-app
// /chat full-screen route. In cloud, it opens the dedicated
// employee SPA (workspace.bastio.com) in a new tab.
function OpenWorkspaceLink({
  openWorkspaceURL,
}: {
  openWorkspaceURL: string | null;
}) {
  const className =
    "inline-flex items-center gap-2 rounded-md border border-border bg-background px-3 py-1.5 text-xs text-muted-foreground transition hover:text-foreground";

  if (openWorkspaceURL) {
    return (
      <a
        href={openWorkspaceURL}
        target="_blank"
        rel="noopener noreferrer"
        className={className}
        title="Open Workspace as an employee"
      >
        Open Workspace
        <ExternalLink className="h-3 w-3" />
      </a>
    );
  }

  return (
    <Link to="/chat" className={className} title="Open chat">
      Open chat
      <MessageSquare className="h-3 w-3" />
    </Link>
  );
}

function navigateToChat(openWorkspaceURL: string | null) {
  if (openWorkspaceURL) {
    window.open(openWorkspaceURL, "_blank", "noopener,noreferrer");
    return;
  }
  window.location.assign("/chat");
}

// CloudUpsellCard renders below the Workspace dashboard in OSS to
// surface what Cloud unlocks (multi-user, SSO, audit retention,
// managed Presidio). Hidden when WorkspaceExtension.hideUpsell is
// true — i.e. on the hosted product where the user is already on
// Cloud and the funnel would be noise.
function CloudUpsellCard() {
  const items = [
    { label: "Team management", desc: "Invite users, roles, RBAC" },
    { label: "Single sign-on", desc: "SAML, OIDC, SCIM" },
    { label: "Stripe billing", desc: "Per-seat, per-token, mixed" },
    { label: "Audit retention", desc: "7-year tamper-evident export" },
    { label: "Custom domain", desc: "workspace.your-company.com" },
    { label: "Managed Presidio", desc: "PII classifier, no setup" },
  ];

  return (
    <div className="mt-6 rounded-lg border border-border bg-muted/30 p-6">
      <div className="flex items-start justify-between gap-6">
        <div className="max-w-xl">
          <p className="text-[10px] font-medium uppercase tracking-widest text-muted-foreground">
            Bastio Cloud
          </p>
          <h2 className="mt-2 text-xl font-semibold tracking-tight">
            Run Workspace as a managed product.
          </h2>
          <p className="mt-2 text-sm text-muted-foreground">
            OSS gives you the gateway, the chat surface, and one operator. Cloud
            adds everything you need to run it for a real team.
          </p>
        </div>
        <a
          href="https://bastio.com/cloud"
          target="_blank"
          rel="noopener noreferrer"
          className="inline-flex shrink-0 items-center gap-1.5 rounded-md bg-foreground px-3 py-2 text-xs font-medium text-background transition hover:opacity-90"
        >
          See Cloud
          <ArrowRight className="h-3 w-3" />
        </a>
      </div>
      <div className="mt-5 grid gap-x-6 gap-y-3 sm:grid-cols-2 lg:grid-cols-3">
        {items.map((it) => (
          <div key={it.label} className="text-xs">
            <p className="font-medium text-foreground">{it.label}</p>
            <p className="text-muted-foreground">{it.desc}</p>
          </div>
        ))}
      </div>
    </div>
  );
}

// SubscribedToast renders a one-time success banner after Stripe
// Checkout returns the user to /workspace?subscribed=1. Self-dismissing
// after 8 seconds; the user can also close it explicitly. The query
// param scrub in the parent effect ensures a refresh doesn't re-show.
function SubscribedToast({ onDismiss }: { onDismiss: () => void }) {
  useEffect(() => {
    const timer = setTimeout(onDismiss, 8000);
    return () => clearTimeout(timer);
  }, [onDismiss]);

  return (
    <div className="flex items-start gap-3 rounded-lg border border-cyan-500/40 bg-cyan-500/10 p-4">
      <CheckCircle2 className="h-5 w-5 shrink-0 text-cyan-500" />
      <div className="flex-1">
        <p className="text-sm font-semibold">Your governance audit is now plugged in.</p>
        <p className="mt-1 text-xs text-muted-foreground">
          Workspace is live. Configure assistants and team in the tabs below; your
          employees will use it at workspace.bastio.com.
        </p>
      </div>
      <button
        type="button"
        onClick={onDismiss}
        aria-label="Dismiss"
        className="text-muted-foreground hover:text-foreground"
      >
        <X className="h-4 w-4" />
      </button>
    </div>
  );
}

// Used by AdminTab type so eslint doesn't flag the export-only type
// when the strip drops to 5 tabs from 8.
export type { AdminTab };
