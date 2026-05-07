// Full-screen chat-only workspace route, mounted at /workspace/chat.
//
// In OSS there's no separate employee SPA at workspace.bastio.com —
// that surface lives in bastio-cloud. The "Open Workspace" link in
// the admin Workspace page repoints here so single-tenant self-
// hosters get a chat-only view (no admin chrome, no tab strip)
// without leaving the dashboard.
//
// Cloud-dashboard's main.tsx supplies WorkspaceExtension.openWorkspaceURL
// to point that link at workspace.bastio.com instead, so cloud users
// keep their existing employee SPA. This route still exists in cloud
// but is not the primary chat surface.

import { ChatTab } from "@/components/workspace/chat-tab";

export function WorkspaceChatPage() {
  // h-screen + overflow-hidden so the chat-tab's internal flex column
  // (sidebar + composer) takes the full viewport. The dashboard's
  // <Layout> wraps this route, but the layout's sidebar collapses
  // visually because we don't render any nested admin chrome.
  return (
    <div className="flex h-[calc(100vh-3rem)] min-h-0 flex-col overflow-hidden p-3">
      <ChatTab manageUrl={false} />
    </div>
  );
}
