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
  // Match the full-height workbench viewport budget inside <Layout>
  // without nested padding so ChatTab's sidebar and composer sit flush
  // inside a crisp hairline frame.
  return (
    <div className="flex h-[calc(100vh-6.5rem)] min-h-0 flex-col overflow-hidden border border-border-subtle bg-background">
      <ChatTab manageUrl={false} />
    </div>
  );
}
