import { useQuery } from "@tanstack/react-query";

import { Card, CardContent } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { KpiCard } from "@/components/observe/kpi-card";
import { SkeletonRows } from "@/components/skeleton";
import { formatNumber } from "@/lib/utils";

import {
  workspaceApi,
  formatCents,
  formatTokens,
  relativeTime,
  type ConversationListItem,
} from "./types";

type Props = {
  onOpenChat: (conversationID?: string) => void;
};

export function DashboardTab({ onOpenChat }: Props) {
  const summary = useQuery({
    queryKey: ["workspace", "analytics", "summary"],
    queryFn: workspaceApi.analyticsSummary,
  });
  const recent = useQuery({
    queryKey: ["workspace", "conversations", "recent"],
    queryFn: () => workspaceApi.listConversations(10),
  });

  return (
    <div className="space-y-6">
      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-4">
        <KpiCard
          label="Messages this month"
          value={summary.data ? formatNumber(summary.data.messages_this_month) : "—"}
        />
        <KpiCard
          label="Tokens used"
          value={summary.data ? formatTokens(summary.data.tokens_this_month) : "—"}
        />
        <KpiCard
          label="Estimated cost"
          value={summary.data ? formatCents(summary.data.cost_cents_this_month) : "—"}
        />
        <KpiCard
          label="Active users"
          value={
            summary.data
              ? `${summary.data.active_users}/${summary.data.seat_limit}`
              : "—"
          }
        />
      </div>

      <div className="grid grid-cols-1 gap-6 lg:grid-cols-3">
        <Card className="lg:col-span-2">
          <CardContent className="p-6">
            <div className="mb-4 flex items-center justify-between">
              <h3 className="text-sm font-semibold">Recent conversations</h3>
              <Button size="sm" variant="ghost" onClick={() => onOpenChat()}>
                Start new chat
              </Button>
            </div>
            {recent.isLoading ? (
              <SkeletonRows count={4} />
            ) : recent.data?.conversations.length === 0 ? (
              <p className="text-sm text-muted-foreground">
                No conversations yet — start your first chat.
              </p>
            ) : (
              <ul className="divide-y divide-border">
                {recent.data?.conversations.map((c) => (
                  <RecentConversationRow
                    key={c.id}
                    item={c}
                    onClick={() => onOpenChat(c.id)}
                  />
                ))}
              </ul>
            )}
          </CardContent>
        </Card>

        <Card>
          <CardContent className="p-6">
            <h3 className="mb-4 text-sm font-semibold">Quick actions</h3>
            <div className="flex flex-col gap-2">
              <Button size="sm" onClick={() => onOpenChat()}>
                Start new chat
              </Button>
              <Button size="sm" variant="outline" disabled>
                View analytics
              </Button>
              <Button size="sm" variant="outline" disabled>
                Invite members
              </Button>
              <Button size="sm" variant="outline" disabled>
                Team settings
              </Button>
            </div>
          </CardContent>
        </Card>
      </div>
    </div>
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
    <li
      onClick={onClick}
      className="flex cursor-pointer items-center justify-between gap-4 py-3 hover:bg-muted/50"
    >
      <div className="min-w-0 flex-1">
        <p className="truncate text-sm font-medium">{item.title}</p>
        {item.last_message_peek && (
          <p className="truncate text-xs text-muted-foreground">
            {item.last_message_role === "assistant" ? "AI: " : "You: "}
            {item.last_message_peek}
          </p>
        )}
      </div>
      <div className="flex items-center gap-3 text-xs text-muted-foreground">
        <span>{item.message_count} msgs</span>
        <span>{relativeTime(item.last_message_at)}</span>
      </div>
    </li>
  );
}
