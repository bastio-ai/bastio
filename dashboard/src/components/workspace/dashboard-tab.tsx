import { useQuery } from "@tanstack/react-query";
import {
  ArrowRight,
  Bot,
  BookOpen,
  CheckCircle2,
  MessageSquarePlus,
  Settings2,
  ShieldCheck,
} from "lucide-react";

import { AdminSummaryStrip, SecurityNotice } from "@/components/admin/admin-primitives";
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
import { useWorkspaceExtension } from "./extension-context";

type Props = {
  onOpenChat: (conversationID?: string) => void;
};

export function DashboardTab({ onOpenChat }: Props) {
  const ext = useWorkspaceExtension();
  const summary = useQuery({
    queryKey: ["workspace", "analytics", "summary"],
    queryFn: workspaceApi.analyticsSummary,
  });
  const recent = useQuery({
    queryKey: ["workspace", "conversations", "recent"],
    queryFn: () => workspaceApi.listConversations(10),
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

  const assistantCount = assistants.data?.assistants.length ?? 0;
  const readySources = knowledge.data?.sources.filter((item) => item.status === "ready").length ?? 0;
  const allowedModelCount = settings.data?.allowed_models?.length ?? 0;
  const hasPersona = Boolean(
    settings.data?.ai_persona_name ||
      settings.data?.ai_persona_personality ||
      settings.data?.ai_persona_tone,
  );

  return (
    <div>
      <AdminSummaryStrip
        items={[
          {
            label: "Messages this month",
            value: summary.data ? formatNumber(summary.data.messages_this_month) : "—",
            detail: "Across portal conversations",
          },
          {
            label: "Tokens used",
            value: summary.data ? formatTokens(summary.data.tokens_this_month) : "—",
            detail: "Current billing month",
          },
          {
            label: "Estimated cost",
            value: summary.data ? formatCents(summary.data.cost_cents_this_month) : "—",
            detail: "Model usage estimate",
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
              <h2 className="text-[13px] font-semibold text-foreground">Recent conversations</h2>
              <p className="mt-0.5 text-[11px] text-muted-foreground">
                Latest employee activity in the private portal.
              </p>
            </div>
            <Button size="sm" onClick={() => onOpenChat()}>
              <MessageSquarePlus data-icon="inline-start" />
              New chat
            </Button>
          </div>

          {recent.isLoading ? (
            <div className="p-4"><SkeletonRows count={5} /></div>
          ) : recent.data?.conversations.length === 0 ? (
            <div className="flex min-h-64 flex-col items-center justify-center px-6 py-12 text-center">
              <div className="flex h-10 w-10 items-center justify-center rounded-lg border border-border/70 bg-muted/40 text-muted-foreground">
                <MessageSquarePlus className="h-4 w-4" />
              </div>
              <h3 className="mt-4 text-[13px] font-medium text-foreground">No conversations yet</h3>
              <p className="mt-1 max-w-sm text-[11px] leading-relaxed text-muted-foreground">
                Open the portal to test the employee experience and create the first governed conversation.
              </p>
              <Button className="mt-4" size="sm" variant="outline" onClick={() => onOpenChat()}>
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
                <h2 className="text-[13px] font-semibold text-foreground">Workspace readiness</h2>
              </div>
              <p className="mt-1 text-[11px] text-muted-foreground">
                Configuration that shapes every employee conversation.
              </p>
            </div>
            <div className="divide-y divide-border/50">
              <ReadinessRow
                icon={Bot}
                label="Assistants"
                value={assistants.isLoading ? "Checking…" : assistantCount ? `${assistantCount} configured` : "Not configured"}
                ready={assistantCount > 0}
              />
              <ReadinessRow
                icon={BookOpen}
                label="Knowledge"
                value={knowledge.isLoading ? "Checking…" : readySources ? `${readySources} ready` : "No ready sources"}
                ready={readySources > 0}
              />
              <ReadinessRow
                icon={Settings2}
                label="Model access"
                value={settings.isLoading ? "Checking…" : allowedModelCount ? `${allowedModelCount} approved` : "All curated models"}
                ready={allowedModelCount > 0}
                neutral={allowedModelCount === 0}
              />
              <ReadinessRow
                icon={ShieldCheck}
                label="AI identity"
                value={settings.isLoading ? "Checking…" : hasPersona ? "Configured" : "Assistant defaults"}
                ready={hasPersona}
                neutral={!hasPersona}
              />
            </div>
          </section>

          <SecurityNotice title="Gateway policies remain authoritative" tone="success">
            Portal requests pass through Bastio security controls. Assistant prompts and knowledge access do not bypass threat inspection, policy enforcement, or trace logging.
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
}: {
  icon: typeof Bot;
  label: string;
  value: string;
  ready: boolean;
  neutral?: boolean;
}) {
  return (
    <div className="flex items-center gap-3 px-4 py-3">
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
          ready ? "text-success" : neutral ? "text-muted-foreground/50" : "text-muted-foreground/30",
        )}
      />
    </div>
  );
}

function RecentConversationRow({ item, onClick }: { item: ConversationListItem; onClick: () => void }) {
  return (
    <button
      type="button"
      onClick={onClick}
      className="group flex w-full items-center gap-4 px-4 py-3 text-left transition-colors hover:bg-muted/30 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring"
    >
      <div className="min-w-0 flex-1">
        <p className="truncate text-[12px] font-medium text-foreground">{item.title}</p>
        <p className="mt-0.5 truncate text-[10px] text-muted-foreground">
          {item.last_message_peek
            ? `${item.last_message_role === "assistant" ? "AI" : "User"}: ${item.last_message_peek}`
            : "No message preview"}
        </p>
      </div>
      <div className="hidden shrink-0 items-center gap-4 text-[10px] text-muted-foreground sm:flex">
        <span className="font-mono tabular-nums">{item.message_count} msgs</span>
        <span>{relativeTime(item.last_message_at)}</span>
        <ArrowRight className="h-3.5 w-3.5 opacity-0 transition-opacity group-hover:opacity-100" />
      </div>
    </button>
  );
}
