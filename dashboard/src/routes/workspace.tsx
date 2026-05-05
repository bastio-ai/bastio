import { useEffect, useState } from "react";
import { CheckCircle2, X, ExternalLink } from "lucide-react";

import { PageHeader } from "@/components/card";

import { DashboardTab } from "@/components/workspace/dashboard-tab";
import { TeamTab } from "@/components/workspace/team-tab";
import { AssistantsTab } from "@/components/workspace/assistants-tab";
import { KnowledgeTab } from "@/components/workspace/knowledge-tab";
import { IntegrationsTab } from "@/components/workspace/integrations-tab";
import { SettingsTab } from "@/components/workspace/settings-tab";
import { AnalyticsTab } from "@/components/workspace/analytics-tab";
import { AuditTab } from "@/components/workspace/audit-tab";

// AdminTab is the operator-facing view of Workspace. The chat product
// itself lives in a separate app at workspace.bastio.com (or the
// customer's custom domain) — employees never see the admin surface,
// admins never need to embed the chat surface inside their dashboard.
type AdminTab =
  | "dashboard"
  | "team"
  | "assistants"
  | "knowledge"
  | "integrations"
  | "settings"
  | "analytics"
  | "audit";

const TABS: { id: AdminTab; label: string }[] = [
  { id: "dashboard", label: "Dashboard" },
  { id: "team", label: "Team" },
  { id: "assistants", label: "Assistants" },
  { id: "knowledge", label: "Knowledge Base" },
  { id: "integrations", label: "Integrations" },
  { id: "settings", label: "Settings" },
  { id: "analytics", label: "Analytics" },
  { id: "audit", label: "Audit Log" },
];

// WorkspacePage is the ADMIN console for Workspace. Configure
// assistants, knowledge bases, team membership, retention, billing.
// The actual chat product employees use is a separate Vite app
// served from a Workspace host (e.g. workspace.example.com or a
// customer's branded domain).
export function WorkspacePage() {
  const [tab, setTab] = useState<AdminTab>("dashboard");
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

  // The dashboard tab originally surfaced an "open chat" affordance.
  // Chat now lives at a separate host (workspace.bastio.com), so the
  // affordance opens that URL in a new tab. workspaceURL falls back
  // to a sensible local-dev default if the env var isn't set.
  const workspaceURL =
    (import.meta as ImportMeta & { env: Record<string, string | undefined> }).env
      ?.VITE_WORKSPACE_URL ?? "http://workspace.localhost:3000";
  const openChat = () => {
    window.open(workspaceURL, "_blank", "noopener,noreferrer");
  };

  return (
    <div className="space-y-6 p-6">
      {showSubscribedToast && (
        <SubscribedToast onDismiss={() => setShowSubscribedToast(false)} />
      )}
      <div className="flex items-start justify-between gap-4">
        <PageHeader
          title="Workspace"
          description="Configure the AI chat product your team uses. Assistants, knowledge bases, team membership, retention, billing."
        />
        <a
          href={workspaceURL}
          target="_blank"
          rel="noopener noreferrer"
          className="inline-flex items-center gap-2 rounded-md border border-border bg-background px-3 py-1.5 text-xs text-muted-foreground transition hover:text-foreground"
          title="Open Workspace as an employee"
        >
          Open Workspace
          <ExternalLink className="h-3 w-3" />
        </a>
      </div>

      <div className="flex gap-1 overflow-x-auto border-b border-border">
        {TABS.map((t) => (
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

      {tab === "dashboard" && <DashboardTab onOpenChat={openChat} />}
      {tab === "team" && <TeamTab />}
      {tab === "assistants" && <AssistantsTab />}
      {tab === "knowledge" && <KnowledgeTab />}
      {tab === "integrations" && <IntegrationsTab />}
      {tab === "settings" && <SettingsTab />}
      {tab === "analytics" && <AnalyticsTab />}
      {tab === "audit" && <AuditTab />}
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
