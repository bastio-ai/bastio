import { WorkspaceSetupGuide } from "./setup-guide";
import { useQuery } from "@tanstack/react-query";
import { ArrowRight, MessageSquarePlus } from "lucide-react";

import {
  AdminPanel,
  AdminSummaryStrip,
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
      <WorkspaceSetupGuide
        onOpenChat={() => onOpenChat()}
        onOpenTab={onOpenTab}
      />

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

      <div className="grid gap-8 xl:grid-cols-[minmax(0,1.5fr)_minmax(300px,0.8fr)]">
        <AdminPanel
          title="Recent conversations"
          description="Your latest conversations. Open the employee workspace to continue chatting."
          action={
            <Button
              type="button"
              size="sm"
              className="h-8 text-xs"
              onClick={() => onOpenChat()}
            >
              <MessageSquarePlus className="size-3.5" aria-hidden="true" />
              New chat
            </Button>
          }
          contentClassName="py-0"
        >
          {recent.isLoading ? (
            <div className="border-b border-border-subtle py-4">
              <SkeletonRows count={5} />
            </div>
          ) : !recent.data || recent.data.conversations.length === 0 ? (
            <div className="border-b border-border-subtle py-10 text-center">
              <h3 className="text-[13px] font-medium text-foreground">
                No conversations yet
              </h3>
              <p className="mx-auto mt-1 max-w-sm text-xs leading-relaxed text-muted-foreground">
                Open the portal to test the employee experience and create the
                first governed conversation.
              </p>
              <Button
                type="button"
                className="mt-4 h-8 text-xs"
                size="sm"
                variant="outline"
                onClick={() => onOpenChat()}
              >
                Open portal
                <ArrowRight className="size-3.5" aria-hidden="true" />
              </Button>
            </div>
          ) : (
            <div className="divide-y divide-border-subtle border-b border-border-subtle">
              {recent.data.conversations.map((item) => (
                <RecentConversationRow
                  key={item.id}
                  item={item}
                  onClick={() => onOpenChat(item.id)}
                />
              ))}
            </div>
          )}
        </AdminPanel>

        <div className="space-y-6">
          <AdminPanel
            title="Workspace configuration"
            description="Configuration that shapes every employee conversation."
            contentClassName="py-0"
          >
            <div className="divide-y divide-border-subtle border-b border-border-subtle">
              <ReadinessRow
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
                label="Model access"
                value={
                  settings.isLoading
                    ? "Checking…"
                    : allowedModelCount
                      ? `${allowedModelCount} approved`
                      : "Connected provider models"
                }
                ready={allowedModelCount > 0}
                neutral={allowedModelCount === 0}
                onClick={() => onOpenTab?.("settings", "models")}
              />
              <ReadinessRow
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
          </AdminPanel>

          <div className="border-l-2 border-success/60 pl-3.5 text-xs leading-relaxed text-muted-foreground">
            <span className="font-medium text-foreground">
              Gateway policies remain authoritative.
            </span>{" "}
            Portal requests pass through Bastio security controls. Assistant
            prompts and knowledge access do not bypass threat inspection, policy
            enforcement, or trace logging.
          </div>

          {ext.quickActions ? (
            <AdminPanel title="Organization actions">
              <div className="flex flex-col gap-2 border-b border-border-subtle pb-4">
                {ext.quickActions}
              </div>
            </AdminPanel>
          ) : null}
        </div>
      </div>
    </div>
  );
}

function ReadinessRow({
  label,
  value,
  ready,
  neutral = false,
  onClick,
}: {
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
      className="group flex w-full cursor-pointer items-center justify-between gap-3 py-3 text-left transition-colors hover:bg-surface-1/60 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring"
    >
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-2">
          <span
            aria-hidden="true"
            className={cn(
              "size-1.5 shrink-0 rounded-full",
              ready
                ? "bg-success"
                : neutral
                  ? "bg-muted-foreground/50"
                  : "bg-muted-foreground/30",
            )}
          />
          <span className="text-xs font-medium text-foreground">{label}</span>
        </div>
        <p className="mt-0.5 truncate pl-3.5 text-[11px] text-muted-foreground">
          {value}
        </p>
      </div>
      <ArrowRight
        className="size-3.5 shrink-0 text-muted-foreground transition-transform group-hover:translate-x-0.5 group-hover:text-foreground"
        aria-hidden="true"
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
      className="group flex w-full cursor-pointer items-center gap-4 py-3 text-left transition-colors hover:bg-surface-1/60 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring"
    >
      <div className="min-w-0 flex-1">
        <p className="truncate text-xs font-medium text-foreground">
          {item.title}
        </p>
        <p className="mt-0.5 truncate text-[11px] text-muted-foreground">
          {item.last_message_peek
            ? `${item.last_message_role === "assistant" ? "AI" : "User"}: ${item.last_message_peek}`
            : "No message preview"}
        </p>
      </div>
      <div className="hidden shrink-0 items-center gap-4 font-mono text-[11px] tabular-nums text-muted-foreground sm:flex">
        <span>{item.message_count} msgs</span>
        <span>{relativeTime(item.last_message_at)}</span>
        <ArrowRight
          className="size-3.5 opacity-0 transition-opacity group-hover:opacity-100"
          aria-hidden="true"
        />
      </div>
    </button>
  );
}
