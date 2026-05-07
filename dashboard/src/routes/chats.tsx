// All-chats list. Linked from chat-tab.tsx's "View all chats" footer
// when the recent-only sidebar runs out of room. Click a row to open
// the chat — the workspace-chat page reads /c/<uuid> from the URL on
// mount and seeds the conversation, so navigating to /c/<id> from
// here lands the user inside that thread.

import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { MessagesSquare, Pin, Search } from "lucide-react";

import { workspaceApi } from "@/components/workspace/types";
import type { ConversationListItem } from "@/components/workspace/types";
import { Card, CardContent } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Badge } from "@/components/ui/badge";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { EmptyState, PageHeader } from "@/components/card";
import { SkeletonRows } from "@/components/skeleton";
import { formatNumber } from "@/lib/utils";

export function ChatsPage() {
  const navigate = useNavigate();
  const [search, setSearch] = useState("");

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

  return (
    <>
      <PageHeader
        title="All chats"
        description={
          conversations.length
            ? `${formatNumber(conversations.length)} conversation${conversations.length === 1 ? "" : "s"} in this workspace.`
            : "Your workspace conversations land here."
        }
      />

      <div className="mt-4 max-w-md">
        <div className="relative">
          <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground/60" />
          <Input
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            placeholder="Search by title or message…"
            className="pl-9"
          />
        </div>
      </div>

      <Card className="mt-4">
        <CardContent className="p-0">
          {isLoading ? (
            <div className="p-4">
              <SkeletonRows count={6} />
            </div>
          ) : filtered.length === 0 ? (
            <div className="p-8">
              <EmptyState
                icon={<MessagesSquare className="h-6 w-6" />}
                title={search ? "No matches" : "No conversations yet"}
                description={
                  search
                    ? "Try a different search term, or clear the box to see everything."
                    : "Start a new chat from the workspace and it'll show up here."
                }
              />
            </div>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Title</TableHead>
                  <TableHead className="hidden lg:table-cell">Last message</TableHead>
                  <TableHead className="text-right">Messages</TableHead>
                  <TableHead className="text-right">Last activity</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {filtered.map((c) => (
                  <TableRow
                    key={c.id}
                    className="cursor-pointer"
                    onClick={() => navigate({ to: "/c/$id" as never, params: { id: c.id } as never })}
                  >
                    <TableCell className="font-medium">
                      <div className="flex items-center gap-2">
                        {c.pinned && (
                          <Pin className="h-3.5 w-3.5 text-muted-foreground/70" aria-hidden />
                        )}
                        <span className="truncate">{c.title || "Untitled"}</span>
                      </div>
                    </TableCell>
                    <TableCell className="hidden text-muted-foreground lg:table-cell">
                      <span className="block max-w-[480px] truncate">
                        {c.last_message_peek || "—"}
                      </span>
                    </TableCell>
                    <TableCell className="text-right tabular-nums">
                      <Badge variant="outline" className="font-normal">
                        {formatNumber(c.message_count)}
                      </Badge>
                    </TableCell>
                    <TableCell className="text-right text-muted-foreground tabular-nums">
                      {formatRelativeTime(c.last_message_at)}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>
    </>
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
