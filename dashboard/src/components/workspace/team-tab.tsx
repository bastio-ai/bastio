import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Plus, Trash2 } from "lucide-react";

import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { SkeletonRows } from "@/components/skeleton";

import { workspaceApi, relativeTime, type Member } from "./types";

// inviteAcceptOrigin returns the origin to use when displaying an
// invitation accept link in the dashboard's "copy link" panel.
// Prefers VITE_WORKSPACE_URL (the chat host — workspace.bastio.com
// in cloud, http://workspace.localhost:3000 in dev) so invitees
// land directly on the chat surface, with no cross-subdomain
// session-cookie hand-off. Falls back to the dashboard's own
// origin for single-host OSS deployments where the dashboard
// itself answers /accept-invite.
function inviteAcceptOrigin(): string {
  const env = (
    import.meta as ImportMeta & { env: Record<string, string | undefined> }
  ).env;
  const u = env?.VITE_WORKSPACE_URL?.replace(/\/$/, "");
  if (u) return u;
  return window.location.origin;
}

export function TeamTab() {
  const qc = useQueryClient();
  // whoami drives every UI gate on this page — Invite button, role
  // editor on rows, transfer-ownership control. The server is the
  // real authority (RequireRole 403s); this just keeps the UI from
  // dangling clickable affordances that would always 403.
  const me = useQuery({
    queryKey: ["workspace", "whoami"],
    queryFn: workspaceApi.whoami,
    staleTime: 60_000,
  });
  const members = useQuery({
    queryKey: ["workspace", "members"],
    queryFn: workspaceApi.listMembers,
  });
  const invitations = useQuery({
    queryKey: ["workspace", "invitations"],
    queryFn: workspaceApi.listInvitations,
  });
  const [showInvite, setShowInvite] = useState(false);
  const [issuedToken, setIssuedToken] = useState<string | null>(null);

  const remove = useMutation({
    mutationFn: workspaceApi.removeMember,
    onSuccess: () => qc.invalidateQueries({ queryKey: ["workspace", "members"] }),
  });
  const revoke = useMutation({
    mutationFn: workspaceApi.revokeInvitation,
    onSuccess: () => qc.invalidateQueries({ queryKey: ["workspace", "invitations"] }),
  });
  const changeRole = useMutation({
    mutationFn: ({ userID, role }: { userID: string; role: "admin" | "member" | "viewer" }) =>
      workspaceApi.changeMemberRole(userID, role),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["workspace", "members"] }),
  });
  const transfer = useMutation({
    mutationFn: workspaceApi.transferOwnership,
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["workspace", "members"] });
      qc.invalidateQueries({ queryKey: ["workspace", "whoami"] });
    },
  });

  const canAdmin = me.data?.can_admin === true;
  const isOwner = me.data?.is_owner === true;

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h3 className="text-sm font-semibold">Team</h3>
        {canAdmin && (
          <Button size="sm" onClick={() => setShowInvite(true)}>
            <Plus className="mr-2 h-4 w-4" /> Invite member
          </Button>
        )}
      </div>

      <Card>
        <CardContent className="p-0">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-border text-xs uppercase tracking-wide text-muted-foreground">
                <th className="px-4 py-2 text-left font-medium">Email</th>
                <th className="px-4 py-2 text-left font-medium">Role</th>
                <th className="px-4 py-2 text-left font-medium">Joined</th>
                <th className="px-4 py-2 text-left font-medium">Last seen</th>
                <th className="px-4 py-2 text-left font-medium" title="Monthly token cap. Empty = no limit.">Monthly tokens</th>
                <th className="px-4 py-2 text-left font-medium" title="Daily message rate cap. Empty = no limit.">Daily msgs</th>
                <th className="px-4 py-2"></th>
              </tr>
            </thead>
            <tbody className="divide-y divide-border">
              {members.isLoading && (
                <tr>
                  <td colSpan={7} className="p-4">
                    <SkeletonRows count={2} />
                  </td>
                </tr>
              )}
              {members.data?.members.length === 0 && (
                <tr>
                  <td colSpan={7} className="p-4 text-center text-muted-foreground">
                    No members yet — invite someone to get started.
                  </td>
                </tr>
              )}
              {members.data?.members.map((m) => (
                <tr key={m.user_id}>
                  <td className="px-4 py-2">
                    {m.email}
                    {m.user_id === me.data?.user_id && (
                      <span className="ml-1.5 text-[10px] text-muted-foreground">(you)</span>
                    )}
                  </td>
                  <td className="px-4 py-2">
                    {/* Owner row is read-only — owner change goes
                        through the transfer flow. Non-owner rows
                        are editable for admins. */}
                    {canAdmin && m.role !== "owner" ? (
                      <select
                        value={m.role}
                        onChange={(e) =>
                          changeRole.mutate({
                            userID: m.user_id,
                            role: e.target.value as "admin" | "member" | "viewer",
                          })
                        }
                        disabled={changeRole.isPending}
                        className="rounded-md border border-border bg-background px-1.5 py-0.5 text-xs"
                      >
                        <option value="admin">admin</option>
                        <option value="member">member</option>
                        <option value="viewer">viewer</option>
                      </select>
                    ) : (
                      <Badge variant="outline">{m.role}</Badge>
                    )}
                  </td>
                  <td className="px-4 py-2 text-xs text-muted-foreground">
                    {relativeTime(m.joined_at)}
                  </td>
                  <td className="px-4 py-2 text-xs text-muted-foreground">
                    {m.last_seen_at ? relativeTime(m.last_seen_at) : "—"}
                  </td>
                  <td className="px-4 py-2">
                    <BudgetCell
                      member={m}
                      field="monthly_token_limit"
                      placeholder="∞"
                    />
                  </td>
                  <td className="px-4 py-2">
                    <BudgetCell
                      member={m}
                      field="daily_rate_limit"
                      placeholder="∞"
                    />
                  </td>
                  <td className="px-4 py-2 text-right">
                    <div className="flex items-center justify-end gap-1">
                      {/* Owner row gets a "Transfer to…" — only the
                          current owner sees it, only on OTHER members'
                          rows (you transfer TO someone else). */}
                      {isOwner && m.role !== "owner" && (
                        <Button
                          size="sm"
                          variant="ghost"
                          className="text-xs"
                          onClick={() => {
                            if (
                              confirm(
                                `Transfer ownership of this workspace to ${m.email}?\n\n` +
                                  `You will become an admin. The workspace can only have one owner.`,
                              )
                            ) {
                              transfer.mutate(m.user_id);
                            }
                          }}
                          disabled={transfer.isPending}
                        >
                          Make owner
                        </Button>
                      )}
                      {canAdmin && m.role !== "owner" && (
                        <Button
                          size="sm"
                          variant="ghost"
                          onClick={() => {
                            if (confirm(`Remove ${m.email}?`)) remove.mutate(m.user_id);
                          }}
                        >
                          <Trash2 className="h-3.5 w-3.5" />
                        </Button>
                      )}
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </CardContent>
      </Card>

      {invitations.data && invitations.data.invitations.length > 0 && (
        <Card>
          <CardContent className="p-4">
            <h4 className="mb-3 text-sm font-semibold">Pending invitations</h4>
            <ul className="divide-y divide-border">
              {invitations.data.invitations.map((inv) => (
                <li
                  key={inv.id}
                  className="flex items-center justify-between gap-4 py-2 text-sm"
                >
                  <div>
                    <p>{inv.email}</p>
                    <p className="text-xs text-muted-foreground">
                      {inv.role} · expires {new Date(inv.expires_at).toLocaleDateString()}
                    </p>
                  </div>
                  <Button
                    size="sm"
                    variant="ghost"
                    onClick={() => {
                      if (confirm(`Revoke invite to ${inv.email}?`)) revoke.mutate(inv.id);
                    }}
                  >
                    Revoke
                  </Button>
                </li>
              ))}
            </ul>
          </CardContent>
        </Card>
      )}

      {showInvite && (
        <InviteForm
          onClose={() => {
            setShowInvite(false);
            qc.invalidateQueries({ queryKey: ["workspace", "invitations"] });
          }}
          onIssued={setIssuedToken}
        />
      )}

      {issuedToken && (
        <Card className="border-cyan-500/50">
          <CardContent className="space-y-2 p-4">
            <p className="text-sm font-semibold">Invitation link issued</p>
            <p className="text-xs text-muted-foreground">
              Copy and send this link now — the token cannot be retrieved again.
            </p>
            <code className="block break-all rounded-md bg-muted p-2 text-xs">
              {inviteAcceptOrigin()}/accept-invite?token={issuedToken}
            </code>
            <Button size="sm" variant="ghost" onClick={() => setIssuedToken(null)}>
              Got it
            </Button>
          </CardContent>
        </Card>
      )}
    </div>
  );
}

// BudgetCell is the inline editor for one budget field on one member.
// Click → input; blur or Enter → PATCH. Empty input clears the cap
// (sends null). Optimistic — invalidates the members list on save.
function BudgetCell({
  member,
  field,
  placeholder,
}: {
  member: Member;
  field: "monthly_token_limit" | "daily_rate_limit";
  placeholder: string;
}) {
  const qc = useQueryClient();
  const current = member[field] ?? null;
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState<string>(current == null ? "" : String(current));

  const save = useMutation({
    mutationFn: async (v: string) => {
      const trimmed = v.trim();
      const value = trimmed === "" ? null : Number(trimmed);
      if (value !== null && (!Number.isFinite(value) || value < 0)) {
        throw new Error("Must be a non-negative integer or empty");
      }
      await workspaceApi.setMemberBudgets(member.user_id, { [field]: value });
    },
    onSuccess: () => {
      setEditing(false);
      qc.invalidateQueries({ queryKey: ["workspace", "members"] });
    },
  });

  if (!editing) {
    return (
      <button
        type="button"
        onClick={() => {
          setDraft(current == null ? "" : String(current));
          setEditing(true);
        }}
        className="rounded-md px-2 py-0.5 text-xs hover:bg-muted"
      >
        {current == null ? (
          <span className="text-muted-foreground">{placeholder}</span>
        ) : (
          <span className="font-mono">{current.toLocaleString()}</span>
        )}
      </button>
    );
  }

  return (
    <div className="flex items-center gap-1">
      <input
        autoFocus
        type="number"
        min={0}
        value={draft}
        onChange={(e) => setDraft(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === "Enter") save.mutate(draft);
          if (e.key === "Escape") setEditing(false);
        }}
        onBlur={() => save.mutate(draft)}
        placeholder={placeholder}
        className="w-24 rounded-md border border-border bg-background px-2 py-0.5 text-xs"
      />
      {save.error && (
        <span className="text-xs text-destructive">{(save.error as Error).message}</span>
      )}
    </div>
  );
}

// ROLE_OPTIONS describes the *intended* semantics of each invite-time
// role. Note: at the time of writing, the workspace handlers do not
// yet enforce role-based authorization — every authenticated member
// can hit every endpoint. Setting a role today persists the
// designation for forward compatibility; enforcement lands in a
// follow-up sprint with per-route role middleware.
//
// "owner" is intentionally absent — it's reserved for the customer-
// creating signup, not an invite-time selection.
const ROLE_OPTIONS: { value: "admin" | "member" | "viewer"; label: string; hint: string }[] = [
  {
    value: "admin",
    label: "Admin",
    hint:
      "Full workspace control — invite or remove members, manage assistants, knowledge bases, " +
      "security policies, and budgets. Can change other members' roles. Cannot delete the workspace " +
      "or change billing.",
  },
  {
    value: "member",
    label: "Member",
    hint:
      "Day-to-day user — chat with the workspace's assistants, manage their own conversations, " +
      "and see shared assistants and knowledge. Cannot invite others or change settings.",
  },
  {
    value: "viewer",
    label: "Viewer",
    hint:
      "Read-only — see conversations, analytics, and traces but cannot send messages or change " +
      "anything. For auditors, compliance reviewers, or managers who need visibility without " +
      "using the chat.",
  },
];

function InviteForm({
  onClose,
  onIssued,
}: {
  onClose: () => void;
  onIssued: (token: string) => void;
}) {
  const [email, setEmail] = useState("");
  const [role, setRole] = useState<"admin" | "member" | "viewer">("member");

  const issue = useMutation({
    mutationFn: () =>
      workspaceApi.createInvitation({
        email,
        role,
      }),
    onSuccess: (resp) => {
      onIssued(resp.token);
      onClose();
    },
  });

  const activeRole = ROLE_OPTIONS.find((o) => o.value === role)!;

  return (
    <Card>
      <CardContent className="space-y-3 p-4">
        <h4 className="text-sm font-semibold">Invite member</h4>
        <label className="flex flex-col gap-1">
          <span className="text-xs font-medium text-muted-foreground">Email</span>
          <input
            type="email"
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            className="w-full rounded-md border border-border bg-background px-2 py-1 text-sm"
          />
        </label>
        <div className="flex flex-col gap-1">
          <label className="flex flex-col gap-1">
            <span className="text-xs font-medium text-muted-foreground">Role</span>
            <select
              value={role}
              onChange={(e) => setRole(e.target.value as typeof role)}
              className="w-full rounded-md border border-border bg-background px-2 py-1 text-sm"
            >
              {ROLE_OPTIONS.map((o) => (
                <option key={o.value} value={o.value}>
                  {o.label}
                </option>
              ))}
            </select>
          </label>
          <p className="text-[11px] leading-relaxed text-muted-foreground">{activeRole.hint}</p>
        </div>
        {issue.error && (
          <p className="text-sm text-destructive">{(issue.error as Error).message}</p>
        )}
        <div className="flex justify-end gap-2">
          <Button size="sm" variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button
            size="sm"
            onClick={() => issue.mutate()}
            disabled={!email.includes("@") || issue.isPending}
          >
            {issue.isPending ? "Sending…" : "Send invitation"}
          </Button>
        </div>
      </CardContent>
    </Card>
  );
}
