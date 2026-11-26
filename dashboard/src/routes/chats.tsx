// All-chats list. Linked from chat-tab.tsx's "View all chats" footer
// when the recent-only sidebar runs out of room. Click a row to open
// the chat — the workspace-chat page reads /c/<uuid> from the URL on
// mount and seeds the conversation, so navigating to /c/<id> from
// here lands the user inside that thread.

import { useId, useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { MessagesSquare, Pin, Search } from "lucide-react";

import { workspaceApi } from "@/components/workspace/types";
import type { ConversationListItem } from "@/components/workspace/types";
import { Input } from "@/components/ui/input";
import {
  AdminPageHeader,
  AdminSummaryStrip,
} from "@/components/admin/admin-primitives";
import { SkeletonRows } from "@/components/skeleton";
import { formatNumber } from "@/lib/utils";

export function ChatsPage() {
  const navigate = useNavigate();
  const [search, setSearch] = useState("");
  const searchInputId = useId();

  const { data, isLoading } = useQuery({
    queryKey: ["workspace", "conversations", "all"],
    // 200 covers most active workspaces in one round trip; pagination
    // lands when a customer actually crosses it. The chat-tab sidebar
    // requests 50; the all-chats page requests more so users get the
    // long-tail without an extra fetch.
    queryFn: () => workspaceApi.listConversations(200),
  });

  const conversations: ConversationListItem[] = data?.conversations ?? [];

  const filtered = useMemo(() => {
    const q = search.trim().toLowerCase();
    if (!q) return conversations;
    return conversations.filter((c) => {
      if (c.title?.toLowerCase().includes(q)) return true;
      if (c.last_message_peek?.toLowerCase().includes(q)) return true;
      return false;
    });
  }, [conversations, search]);

  const pinnedCount = useMemo(
    () => conversations.filter((c) => c.pinned).length,
    [conversations],
  );
  const totalMessages = useMemo(
    () => conversations.reduce((sum, c) => sum + (c.message_count ?? 0), 0),
    [conversations],
  );

  const openConversation = (conversationId: string) => {
    navigate({ to: "/c/$id" as never, params: { id: conversationId } as never });
  };

  return (
    <div className="mx-auto max-w-[1180px] pb-10 pt-2 md:pt-4">
      <AdminPageHeader
        eyebrow="Workspace"
        title="All chats"
        description={
          conversations.length
            ? `${formatNumber(conversations.length)} conversation${conversations.length === 1 ? "" : "s"} in this workspace.`
            : "Your workspace conversations land here."
        }
        bordered={false}
        className="mb-4"
        badge={
          <span className="inline-flex items-center gap-1.5 text-[11px] text-muted-foreground">
            <span
              aria-hidden="true"
              className={
                conversations.length
                  ? "size-1.5 rounded-full bg-success"
                  : "size-1.5 rounded-full bg-muted-foreground/50"
              }
            />
            {conversations.length ? "Active history" : "Empty"}
          </span>
        }
      />

      <AdminSummaryStrip
        items={[
          {
            label: "Conversations",
            value: formatNumber(conversations.length),
            detail: "Recent workspace threads",
          },
          {
            label: "Pinned",
            value: formatNumber(pinnedCount),
            detail: pinnedCount ? "Saved to sidebar top" : "None pinned",
          },
          {
            label: "Total messages",
            value: formatNumber(totalMessages),
            detail: "Across loaded threads",
          },
          {
            label: "Showing",
            value: formatNumber(filtered.length),
            detail: search.trim() ? "Filtered matches" : "All conversations",
          },
        ]}
      />

      <search className="mb-4 max-w-md">
        <label htmlFor={searchInputId} className="sr-only">
          Search conversations by title or message
        </label>
        <div className="relative">
          <Search
            aria-hidden="true"
            className="pointer-events-none absolute left-3 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground"
          />
          <Input
            id={searchInputId}
            type="search"
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            placeholder="Search by title or message…"
            autoCorrect="off"
            autoCapitalize="none"
            spellCheck={false}
            className="h-8 pl-8 text-xs"
          />
        </div>
      </search>

      {isLoading ? (
        <div className="border-y border-border-subtle">
          <SkeletonRows count={6} />
        </div>
      ) : filtered.length === 0 ? (
        <div className="border-y border-border-subtle py-12 text-center">
          <MessagesSquare
            aria-hidden="true"
            className="mx-auto mb-2.5 h-5 w-5 text-muted-foreground"
          />
          <p className="text-[13px] font-medium text-foreground">
            {search ? "No matches" : "No conversations yet"}
          </p>
          <p className="mt-1 text-xs text-muted-foreground">
            {search
              ? "Try a different search term, or clear the box to see everything."
              : "Start a new chat from the workspace and it'll show up here."}
          </p>
        </div>
      ) : (
        <div
          className="overflow-x-auto border-y border-border-subtle"
          tabIndex={0}
          role="region"
          aria-label="Workspace conversations"
        >
          <table className="w-full min-w-[680px] text-left text-xs">
            <thead className="border-b border-border-subtle text-[11px] text-muted-foreground">
              <tr>
                <th
                  scope="col"
                  className="sticky top-0 bg-background py-2.5 pr-4 font-normal"
                >
                  Title
                </th>
                <th
                  scope="col"
                  className="sticky top-0 hidden bg-background px-4 py-2.5 font-normal lg:table-cell"
                >
                  Last message
                </th>
                <th
                  scope="col"
                  className="sticky top-0 bg-background px-4 py-2.5 text-right font-normal"
                >
                  Messages
                </th>
                <th
                  scope="col"
                  className="sticky top-0 bg-background py-2.5 pl-4 text-right font-normal"
                >
                  Last activity
                </th>
              </tr>
            </thead>
            <tbody className="divide-y divide-border-subtle">
              {filtered.map((c) => (
                <tr
                  key={c.id}
                  className="cursor-pointer transition-colors hover:bg-surface-1/60"
                  onClick={() => openConversation(c.id)}
                >
                  <td className="py-3 pr-4 text-foreground">
                    <button
                      type="button"
                      onClick={(e) => {
                        e.stopPropagation();
                        openConversation(c.id);
                      }}
                      className="inline-flex max-w-full cursor-pointer items-center gap-2 text-left underline decoration-border-default underline-offset-4 hover:decoration-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                    >
                      {c.pinned ? (
                        <Pin
                          aria-hidden="true"
                          className="h-3.5 w-3.5 shrink-0 text-muted-foreground"
                        />
                      ) : null}
                      <span className="truncate">{c.title || "Untitled"}</span>
                    </button>
                  </td>
                  <td className="hidden px-4 py-3 text-muted-foreground lg:table-cell">
                    <span className="block max-w-[460px] truncate">
                      {c.last_message_peek || "—"}
                    </span>
                  </td>
                  <td className="px-4 py-3 text-right font-mono tabular-nums text-muted-foreground">
                    {formatNumber(c.message_count)}
                  </td>
                  <td
                    className="py-3 pl-4 text-right font-mono tabular-nums text-muted-foreground"
                    title={new Date(c.last_message_at).toLocaleString()}
                  >
                    {formatRelativeTime(c.last_message_at)}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}

// formatRelativeTime renders timestamps as a compact distance: "2m",
// "3h", "5d", or absolute date when older than a week. Matches the
// chat-tab sidebar's existing style.
function formatRelativeTime(iso: string): string {
  const now = Date.now();
  const then = new Date(iso).getTime();
  const diffSec = Math.max(0, Math.floor((now - then) / 1000));
  if (diffSec < 60) return "just now";
  const diffMin = Math.floor(diffSec / 60);
  if (diffMin < 60) return `${diffMin}m`;
  const diffHr = Math.floor(diffMin / 60);
  if (diffHr < 24) return `${diffHr}h`;
  const diffDay = Math.floor(diffHr / 24);
  if (diffDay < 7) return `${diffDay}d`;
  return new Date(iso).toLocaleDateString(undefined, {
    year: "numeric",
    month: "short",
    day: "numeric",
  });
}
