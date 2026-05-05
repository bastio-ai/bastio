import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Shield, Filter } from "lucide-react";

import { Card, CardContent } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { SkeletonRows } from "@/components/skeleton";

import { workspaceApi, relativeTime, type AuditEntry } from "./types";

// Friendly labels + colors per action. The backend writes machine-
// readable strings ("member.role_changed"); the UI renders the
// human label and tints the badge so high-stakes actions stand out
// at a glance.
const ACTION_LABELS: Record<string, { label: string; tone: "warn" | "info" | "neutral" | "destructive" }> = {
  "member.role_changed":      { label: "Role changed",       tone: "warn" },
  "member.removed":           { label: "Member removed",     tone: "destructive" },
  "member.budgets_changed":   { label: "Budgets updated",    tone: "info" },
  "owner.transferred":        { label: "Owner transferred",  tone: "destructive" },
  "invitation.created":       { label: "Invitation sent",    tone: "info" },
  "invitation.revoked":       { label: "Invitation revoked", tone: "warn" },
  "invitation.accepted":      { label: "Invitation accepted",tone: "info" },
  "assistant.created":        { label: "Assistant created",  tone: "info" },
  "assistant.updated":        { label: "Assistant edited",   tone: "info" },
  "assistant.archived":       { label: "Assistant archived", tone: "warn" },
  "knowledge.created":        { label: "Knowledge added",    tone: "info" },
  "knowledge.uploaded":       { label: "Knowledge uploaded", tone: "info" },
  "knowledge.archived":       { label: "Knowledge archived", tone: "warn" },
  "settings.updated":         { label: "Settings changed",   tone: "warn" },
  "slug.set":                 { label: "Workspace slug set", tone: "info" },
  "domain.created":           { label: "Domain added",       tone: "info" },
  "domain.verified":          { label: "Domain verified",    tone: "info" },
  "domain.deleted":           { label: "Domain removed",     tone: "warn" },
  // Privacy events — admin/viewer/owner read someone else's thread.
  // Tinted "warn" so a list of these jumps off the page during a
  // GDPR / SOC2 review.
  "conversation.viewed_cross_user": { label: "Viewed user's chat",     tone: "warn" },
  "messages.viewed_cross_user":     { label: "Read user's messages",   tone: "warn" },
  "conversations.scanned_all_users": { label: "Scanned all chats",     tone: "warn" },
};

// Filter options correspond to the prefix groups + a few specific
// destructive actions admins typically want to scan.
const FILTER_OPTIONS = [
  { value: "", label: "All actions" },
  { value: "member.removed", label: "Member removals" },
  { value: "owner.transferred", label: "Ownership transfers" },
  { value: "member.role_changed", label: "Role changes" },
  { value: "settings.updated", label: "Settings changes" },
  { value: "assistant.archived", label: "Assistant archives" },
  { value: "knowledge.archived", label: "Knowledge archives" },
  { value: "messages.viewed_cross_user", label: "Cross-user message reads" },
  { value: "conversation.viewed_cross_user", label: "Cross-user chat views" },
  { value: "conversations.scanned_all_users", label: "All-chats scans" },
];

export function AuditTab() {
  const [action, setAction] = useState("");
  const audit = useQuery({
    queryKey: ["workspace", "audit", action],
    queryFn: () => workspaceApi.listAudit({ limit: 100, action: action || undefined }),
  });

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2">
          <Shield className="h-4 w-4 text-muted-foreground" />
          <h3 className="text-sm font-semibold">Audit log</h3>
          <span className="text-xs text-muted-foreground">
            who did what, when — every privileged action is recorded
          </span>
        </div>
        <div className="flex items-center gap-2">
          <Filter className="h-3.5 w-3.5 text-muted-foreground" />
          <select
            value={action}
            onChange={(e) => setAction(e.target.value)}
            className="rounded-md border border-border bg-background px-2 py-1 text-xs"
          >
            {FILTER_OPTIONS.map((o) => (
              <option key={o.value} value={o.value}>
                {o.label}
              </option>
            ))}
          </select>
        </div>
      </div>

      <Card>
        <CardContent className="p-0">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-border text-xs uppercase tracking-wide text-muted-foreground">
                <th className="px-4 py-2 text-left font-medium">When</th>
                <th className="px-4 py-2 text-left font-medium">Action</th>
                <th className="px-4 py-2 text-left font-medium">Who</th>
                <th className="px-4 py-2 text-left font-medium">Target</th>
                <th className="px-4 py-2 text-left font-medium">Detail</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-border">
              {audit.isLoading && (
                <tr>
                  <td colSpan={5} className="p-4">
                    <SkeletonRows count={3} />
                  </td>
                </tr>
              )}
              {audit.data?.entries.length === 0 && (
                <tr>
                  <td colSpan={5} className="p-4 text-center text-muted-foreground">
                    No matching events.
                  </td>
                </tr>
              )}
              {audit.data?.entries.map((e) => <AuditRow key={e.id} e={e} />)}
            </tbody>
          </table>
        </CardContent>
      </Card>
    </div>
  );
}

function AuditRow({ e }: { e: AuditEntry }) {
  const meta = ACTION_LABELS[e.action] ?? { label: e.action, tone: "neutral" };
  const toneClass =
    meta.tone === "destructive"
      ? "text-destructive border-destructive/30"
      : meta.tone === "warn"
        ? "text-amber-600 border-amber-600/30 dark:text-amber-400"
        : meta.tone === "info"
          ? "text-foreground border-border"
          : "text-muted-foreground border-border";

  return (
    <tr>
      <td className="px-4 py-2 text-xs text-muted-foreground" title={e.created_at}>
        {relativeTime(e.created_at)}
      </td>
      <td className="px-4 py-2">
        <Badge variant="outline" className={toneClass}>
          {meta.label}
        </Badge>
      </td>
      <td className="px-4 py-2 text-xs">
        <div>{e.actor_email || e.actor_user_id || "—"}</div>
        <div className="text-[10px] text-muted-foreground">{e.actor_role}</div>
      </td>
      <td className="px-4 py-2 text-xs">
        {e.target_label ? (
          <div>{e.target_label}</div>
        ) : e.target_id ? (
          <div className="font-mono text-[10px]">{e.target_id.slice(0, 8)}…</div>
        ) : (
          <span className="text-muted-foreground">—</span>
        )}
        {e.target_type && (
          <div className="text-[10px] text-muted-foreground">{e.target_type}</div>
        )}
      </td>
      <td className="px-4 py-2 text-[11px] text-muted-foreground">
        {Object.keys(e.metadata || {}).length === 0 ? (
          "—"
        ) : (
          <code className="font-mono text-[10px]">
            {Object.entries(e.metadata)
              .map(([k, v]) => `${k}: ${formatMetaValue(v)}`)
              .join(" · ")}
          </code>
        )}
      </td>
    </tr>
  );
}

// formatMetaValue keeps the audit row terse. Long arrays / nested
// objects collapse to "[...]" / "{...}" — admins click into a row
// for the full detail (future: expand-on-click).
function formatMetaValue(v: unknown): string {
  if (v === null || v === undefined) return "null";
  if (Array.isArray(v)) return v.length === 0 ? "[]" : `[${v.length}]`;
  if (typeof v === "object") return "{…}";
  if (typeof v === "string") return v.length > 60 ? v.slice(0, 60) + "…" : v;
  return String(v);
}
