import { Link } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import {
  ArrowRight,
  Bot,
  BookOpen,
  CheckCircle2,
  MessageSquarePlus,
  Settings2,
  ShieldCheck,
  Users,
  Plug,
} from "lucide-react";

import {
  AdminSummaryStrip,
  SecurityNotice,
} from "@/components/admin/admin-primitives";
import { Button } from "@/components/ui/button";
import { SkeletonRows } from "@/components/skeleton";
import { cn, formatNumber } from "@/lib/utils";
import {
  workspaceApi,
  formatCents,
  formatTokens,
  relativeTime,
  type ConversationListItem,
} from "./types";
import type { SettingsSection } from "./navigation";
import { useWorkspaceExtension } from "./extension-context";

type Props = {
  onOpenChat: (conversationID?: string) => void;
  onOpenTab?: (tab: string, section?: SettingsSection) => void;
};

export function DashboardTab({ onOpenChat, onOpenTab }: Props) {
  const ext = useWorkspaceExtension();
  const summary = useQuery({
    queryKey: ["workspace", "analytics", "summary"],
    queryFn: workspaceApi.analyticsSummary,
  });
  const recent = useQuery({
    queryKey: ["workspace", "conversations", "recent"],
    queryFn: () => workspaceApi.listConversations(5),
  });
  const assistants = useQuery({
    queryKey: ["workspace", "assistants"],
    queryFn: workspaceApi.listAssistants,
  });
  const knowledge = useQuery({
    queryKey: ["workspace", "knowledge"],
    queryFn: workspaceApi.listKnowledge,
  });
  const settings = useQuery({
    queryKey: ["workspace", "settings"],
    queryFn: workspaceApi.getSettings,
  });

  const readiness = useQuery({
    queryKey: ["workspace", "provider-readiness"],
    queryFn: async () => {
      const r = await fetch("/v1/workspace/me/provider-readiness", {
        credentials: "include",
      });
      if (!r.ok) throw new Error("Provider readiness unavailable");
      return r.json() as Promise<{
        providers: { provider: string; configured: boolean }[];
      }>;
    },
  });
  const spending = useQuery({
    queryKey: ["workspace", "spending"],
    queryFn: async () => {
      const r = await fetch("/v1/workspace/spending", {
        credentials: "include",
      });
      if (!r.ok) throw new Error("Spending status unavailable");
      return r.json() as Promise<{
        used_cents: number;
        limit_cents: number | null;
        near_limit: boolean;
      }>;
    },
  });
  const connected =
    readiness.data?.providers.filter((p) => p.configured).length ?? 0;
  const assistantCount = assistants.data?.assistants.length ?? 0;
  const readySources =
    knowledge.data?.sources.filter((item) => item.status === "ready").length ??
    0;
  const allowedModelCount = settings.data?.allowed_models?.length ?? 0;
  const hasPersona = Boolean(
    settings.data?.ai_persona_name ||
    settings.data?.ai_persona_personality ||
    settings.data?.ai_persona_tone,
  );

  return (
    <div>
      <section className="mb-5 rounded-xl border border-border bg-card p-5 sm:p-6">
        <div className="flex flex-wrap items-start justify-between gap-4">
          <div>
            <h2 className="text-lg font-bold tracking-tight">
              Set up your workspace
            </h2>
            <p className="mt-1 max-w-2xl text-sm leading-relaxed text-muted-foreground">
              A clear path from your first settings to your team’s first
              conversation. Return to any step as your workspace grows.
            </p>
          </div>
          <Button
            variant="outline"
            size="sm"
            onClick={() => onOpenTab?.("settings")}
          >
            Workspace settings
            <Settings2 data-icon="inline-end" />
          </Button>
        </div>
        <ol className="mt-5 grid gap-3 md:grid-cols-2 2xl:grid-cols-4">
          <SetupStep
            number={1}
            icon={Settings2}
            title="Choose your AI models"
            description="Decide which models employees can use. Configure provider keys if you bring your own."
            detail={
              readiness.isPending
                ? "Checking providers…"
                : readiness.error
                  ? "Provider status unavailable"
                  : `${connected} provider${connected === 1 ? "" : "s"} configured`
            }
            action={
              <Button
                variant="outline"
                size="sm"
                onClick={() => onOpenTab?.("settings", "models")}
              >
                Choose models
                <ArrowRight data-icon="inline-end" />
              </Button>
            }
            secondary={
              <Link
                to="/proxies"
                className="text-xs text-muted-foreground underline underline-offset-4"
              >
                Manage provider keys
              </Link>
            }
          />
          <SetupStep
            number={2}
            icon={ShieldCheck}
            title="Set privacy and limits"
            description="Choose whether chats are saved and set a monthly spending limit for your workspace."
            detail={
              settings.isPending
                ? "Checking privacy…"
                : settings.error
                  ? "Privacy status unavailable"
                  : settings.data?.store_conversation_content === false
                    ? "Temporary chats enabled"
                    : "Chat history is saved"
            }
            action={
              <Button
                variant="outline"
                size="sm"
                onClick={() => onOpenTab?.("settings", "privacy")}
              >
                Review privacy
                <ArrowRight data-icon="inline-end" />
              </Button>
            }
            secondary={
              <button
                type="button"
                className="text-xs text-muted-foreground underline underline-offset-4"
                onClick={() => onOpenTab?.("settings", "usage")}
              >
                Set spending limit
              </button>
            }
          />
          <SetupStep
            number={3}
            icon={Users}
            title="Organize your team"
            description="Create team groups, then share resources with everyone, a group, or selected people."
            detail="Groups work across assistants, knowledge, and tools"
            action={
              <Button
                variant="outline"
                size="sm"
                onClick={() =>
                  onOpenTab?.(
                    ext.extraTabs.some((t) => t.id === "team")
                      ? "team"
                      : "groups",
                  )
                }
              >
                {ext.extraTabs.some((t) => t.id === "team")
                  ? "Invite members"
                  : "Manage groups"}
                <ArrowRight data-icon="inline-end" />
              </Button>
            }
            secondary={
              <button
                type="button"
                className="text-xs text-muted-foreground underline underline-offset-4"
                onClick={() => onOpenTab?.("groups")}
              >
                Create groups & set sharing
              </button>
            }
          />
          <SetupStep
            number={4}
            icon={MessageSquarePlus}
            title="Try the employee experience"
            description="Open the workspace and send a sample prompt. Check the answer and security outcome before rollout."
            detail="Use a harmless example for your first test"
            action={
              <Button size="sm" onClick={() => onOpenChat()}>
                Try a conversation
                <ArrowRight data-icon="inline-end" />
              </Button>
            }
          />
        </ol>
      </section>
      <section
        className="mb-5 grid gap-3 lg:grid-cols-3"
        aria-label="Add workspace resources"
      >
        {[
          {
            id: "assistants",
            icon: Bot,
            title: "Assistants",
            detail: assistants.isPending
              ? "Loading…"
              : assistants.error
                ? "Status unavailable"
                : `${assistantCount} configured`,
            description: "Create helpful AI roles for your team.",
          },
          {
            id: "knowledge",
            icon: BookOpen,
            title: "Knowledge",
            detail: knowledge.isPending
              ? "Loading…"
              : knowledge.error
                ? "Status unavailable"
                : `${readySources} ready sources`,
            description: "Upload documents and choose who can use them.",
          },
          {
            id: "integrations",
            icon: Plug,
            title: "Tool integrations",
            detail: "MCP connections",
            description: "Connect business tools and share them securely.",
          },
        ].map((item) => (
          <button
            type="button"
            key={item.id}
            onClick={() => onOpenTab?.(item.id)}
            className="group flex min-h-24 items-start gap-3 rounded-xl border border-border bg-card p-4 text-left transition-colors hover:bg-muted/40 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
          >
            <item.icon className="mt-0.5 h-5 w-5 shrink-0 text-muted-foreground" />
            <div className="min-w-0 flex-1">
              <div className="flex flex-wrap items-center justify-between gap-2">
                <h2 className="text-sm font-bold tracking-tight">
                  {item.title}
                </h2>
                <span className="text-xs text-muted-foreground">
                  {item.detail}
                </span>
              </div>
              <p className="mt-1 text-xs text-muted-foreground">
                {item.description}
              </p>
            </div>
            <ArrowRight className="mt-1 h-4 w-4 shrink-0 text-muted-foreground" />
          </button>
        ))}
      </section>
      <AdminSummaryStrip
        items={[
          {
            label: "Messages this month",
            value: summary.data
              ? formatNumber(summary.data.messages_this_month)
              : "—",
            detail: "Across portal conversations",
          },
          {
            label: "Tokens used",
            value: summary.data
              ? formatTokens(summary.data.tokens_this_month)
              : "—",
            detail: "Current billing month",
          },
          {
            label: "Estimated cost",
            value: spending.data ? formatCents(spending.data.used_cents) : "—",
            detail: spending.data?.near_limit
              ? "Approaching monthly limit"
              : "Includes in-flight reservations",
          },
          {
            label: "Active users",
            value: summary.data
              ? `${summary.data.active_users}/${summary.data.seat_limit}`
              : "—",
            detail: "Seats used this month",
            tone: summary.data?.active_users ? "success" : "default",
          },
        ]}
      />

      <div className="grid gap-5 xl:grid-cols-[minmax(0,1.6fr)_minmax(320px,0.7fr)]">
        <section className="overflow-hidden rounded-xl border border-border/70 bg-card">
          <div className="flex items-center justify-between gap-4 border-b border-border/60 px-4 py-3.5">
            <div>
              <h2 className="text-[13px] font-semibold text-foreground">
                Recent conversations
              </h2>
              <p className="mt-0.5 text-[11px] text-muted-foreground">
                Your latest conversations. Open the employee workspace to
                continue chatting.
              </p>
            </div>
            <Button size="sm" onClick={() => onOpenChat()}>
              <MessageSquarePlus data-icon="inline-start" />
              New chat
            </Button>
          </div>

          {recent.isLoading ? (
            <div className="p-4">
              <SkeletonRows count={5} />
            </div>
          ) : recent.data?.conversations.length === 0 ? (
            <div className="flex min-h-64 flex-col items-center justify-center px-6 py-12 text-center">
              <div className="flex h-10 w-10 items-center justify-center rounded-lg border border-border/70 bg-muted/40 text-muted-foreground">
                <MessageSquarePlus className="h-4 w-4" />
              </div>
              <h3 className="mt-4 text-[13px] font-medium text-foreground">
                No conversations yet
              </h3>
              <p className="mt-1 max-w-sm text-[11px] leading-relaxed text-muted-foreground">
                Open the portal to test the employee experience and create the
                first governed conversation.
              </p>
              <Button
                className="mt-4"
                size="sm"
                variant="outline"
                onClick={() => onOpenChat()}
              >
                Open portal <ArrowRight data-icon="inline-end" />
              </Button>
            </div>
          ) : (
            <div className="divide-y divide-border/50">
              {recent.data?.conversations.map((item) => (
                <RecentConversationRow
                  key={item.id}
                  item={item}
                  onClick={() => onOpenChat(item.id)}
                />
              ))}
            </div>
          )}
        </section>

        <div className="space-y-5">
          <section className="overflow-hidden rounded-xl border border-border/70 bg-card">
            <div className="border-b border-border/60 px-4 py-3.5">
              <div className="flex items-center gap-2">
                <ShieldCheck className="h-4 w-4 text-muted-foreground" />
                <h2 className="text-[13px] font-semibold text-foreground">
                  Workspace configuration
                </h2>
              </div>
              <p className="mt-1 text-[11px] text-muted-foreground">
                Configuration that shapes every employee conversation.
              </p>
            </div>
            <div className="divide-y divide-border/50">
              <ReadinessRow
                icon={Bot}
                label="Assistants"
                value={
                  assistants.isLoading
                    ? "Checking…"
                    : assistantCount
                      ? `${assistantCount} configured`
                      : "Not configured"
                }
                ready={assistantCount > 0}
                onClick={() => onOpenTab?.("assistants")}
              />
              <ReadinessRow
                icon={BookOpen}
                label="Knowledge"
                value={
                  knowledge.isLoading
                    ? "Checking…"
                    : readySources
                      ? `${readySources} ready`
                      : "No ready sources"
                }
                ready={readySources > 0}
                onClick={() => onOpenTab?.("knowledge")}
              />
              <ReadinessRow
                icon={Settings2}
                label="Model access"
                value={
                  settings.isLoading
                    ? "Checking…"
                    : allowedModelCount
                      ? `${allowedModelCount} approved`
                      : "All curated models"
                }
                ready={allowedModelCount > 0}
                neutral={allowedModelCount === 0}
                onClick={() => onOpenTab?.("settings", "models")}
              />
              <ReadinessRow
                icon={ShieldCheck}
                label="AI identity"
                value={
                  settings.isLoading
                    ? "Checking…"
                    : hasPersona
                      ? "Configured"
                      : "Assistant defaults"
                }
                ready={hasPersona}
                neutral={!hasPersona}
                onClick={() => onOpenTab?.("settings", "general")}
              />
            </div>
          </section>

          <SecurityNotice
            title="Gateway policies remain authoritative"
            tone="success"
          >
            Portal requests pass through Bastio security controls. Assistant
            prompts and knowledge access do not bypass threat inspection, policy
            enforcement, or trace logging.
          </SecurityNotice>

          {ext.quickActions ? (
            <section className="rounded-xl border border-border/70 bg-card p-4">
              <p className="mb-3 text-[11px] font-semibold uppercase tracking-[0.14em] text-muted-foreground">
                Organization actions
              </p>
              <div className="flex flex-col gap-2">{ext.quickActions}</div>
            </section>
          ) : null}
        </div>
      </div>
    </div>
  );
}

function ReadinessRow({
  icon: Icon,
  label,
  value,
  ready,
  neutral = false,
  onClick,
}: {
  icon: typeof Bot;
  label: string;
  value: string;
  ready: boolean;
  neutral?: boolean;
  onClick?: () => void;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className="flex w-full items-center gap-3 px-4 py-3 text-left hover:bg-muted/30 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring"
    >
      <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg border border-border/60 bg-muted/30 text-muted-foreground">
        <Icon className="h-3.5 w-3.5" />
      </div>
      <div className="min-w-0 flex-1">
        <p className="text-[11px] font-medium text-foreground">{label}</p>
        <p className="truncate text-[10px] text-muted-foreground">{value}</p>
      </div>
      <CheckCircle2
        className={cn(
          "h-3.5 w-3.5 shrink-0",
          ready
            ? "text-success"
            : neutral
              ? "text-muted-foreground/50"
              : "text-muted-foreground/30",
        )}
      />
    </button>
  );
}

function RecentConversationRow({
  item,
  onClick,
}: {
  item: ConversationListItem;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className="group flex w-full items-center gap-4 px-4 py-3 text-left transition-colors hover:bg-muted/30 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring"
    >
      <div className="min-w-0 flex-1">
        <p className="truncate text-[12px] font-medium text-foreground">
          {item.title}
        </p>
        <p className="mt-0.5 truncate text-[10px] text-muted-foreground">
          {item.last_message_peek
            ? `${item.last_message_role === "assistant" ? "AI" : "User"}: ${item.last_message_peek}`
            : "No message preview"}
        </p>
      </div>
      <div className="hidden shrink-0 items-center gap-4 text-[10px] text-muted-foreground sm:flex">
        <span className="font-mono tabular-nums">
          {item.message_count} msgs
        </span>
        <span>{relativeTime(item.last_message_at)}</span>
        <ArrowRight className="h-3.5 w-3.5 opacity-0 transition-opacity group-hover:opacity-100" />
      </div>
    </button>
  );
}

function SetupStep({
  number,
  icon: Icon,
  title,
  description,
  detail,
  action,
  secondary,
}: {
  number: number;
  icon: typeof Bot;
  title: string;
  description: string;
  detail: string;
  action: import("react").ReactNode;
  secondary?: import("react").ReactNode;
}) {
  return (
    <li className="flex flex-col rounded-xl border border-border bg-muted/10 p-4">
      <div className="flex items-center justify-between">
        <span className="flex h-7 w-7 items-center justify-center rounded-full border border-border text-xs font-mono text-muted-foreground">
          {number}
        </span>
        <Icon className="h-4 w-4 text-muted-foreground" />
      </div>
      <h3 className="mt-3 text-sm font-bold tracking-tight">{title}</h3>
      <p className="mt-1 text-xs leading-relaxed text-muted-foreground">
        {description}
      </p>
      <p className="mt-3 text-xs font-medium">{detail}</p>
      <div className="mt-auto flex flex-wrap items-center gap-3 pt-4">
        {action}
        {secondary}
      </div>
    </li>
  );
}
