import { Users, Plus, Pencil, BookOpen, Bot, Plug } from "lucide-react";
import { SkeletonRows } from "@/components/skeleton";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { useState } from "react";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { http, unwrap } from "@/api/typed";
import {
  ResourceSharingButton,
  sharingLabel,
  sharingMembers,
} from "./sharing-controls";
import { workspaceApi } from "./types";

type Group = { id: string; name: string; user_ids: string[] };
type Access = {
  groups: Group[];
  resources: {
    type: string;
    id: string;
    group_ids: string[];
    user_ids?: string[];
    restricted?: boolean;
  }[];
};
async function request<T>(path: string, body?: unknown): Promise<T> {
  const r = await fetch(`/v1/workspace/access${path}`, {
    credentials: "include",
    method: body ? "PUT" : "GET",
    headers: { "Content-Type": "application/json" },
    body: body ? JSON.stringify(body) : undefined,
  });
  if (!r.ok)
    throw new Error((await r.json()).error ?? "Could not update access");
  return r.json();
}
export function AccessControls() {
  const qc = useQueryClient();
  const [open, setOpen] = useState(false);
  const [search, setSearch] = useState("");
  const [memberSearch, setMemberSearch] = useState("");
  const [resourceType, setResourceType] = useState("all");
  const [name, setName] = useState("");
  const [users, setUsers] = useState<string[]>([]);
  const [editing, setEditing] = useState<string>();
  const access = useQuery({
    queryKey: ["workspace", "access"],
    queryFn: () => request<Access>(""),
  });
  const members = useQuery({
    queryKey: ["workspace", "members"],
    queryFn: sharingMembers,
  });
  const sources = useQuery({
    queryKey: ["workspace", "knowledge"],
    queryFn: workspaceApi.listKnowledge,
  });
  const assistants = useQuery({
    queryKey: ["workspace", "assistants"],
    queryFn: workspaceApi.listAssistants,
  });
  const save = useMutation({
    mutationFn: () =>
      request("/groups", { id: editing, name, user_ids: users }),
    onSuccess: () => {
      setOpen(false);
      setName("");
      setUsers([]);
      setEditing(undefined);
      qc.invalidateQueries({ queryKey: ["workspace", "access"] });
    },
  });
  const integrations = useQuery({
    queryKey: ["workspace-mcp-integrations"],
    queryFn: () => unwrap(http.GET("/v1/workspace/mcp")),
    gcTime: 0,
  });
  const provisioning = useQuery({
    queryKey: ["workspace", "provisioning"],
    queryFn: async () => {
      const r = await fetch("/v1/workspace/provisioning", {
        credentials: "include",
      });
      if (r.status === 404) return null;
      if (!r.ok) throw new Error("Provisioning unavailable");
      return r.json() as Promise<{
        enabled: boolean;
        directory_users: number;
        linked_members: number;
      }>;
    },
  });
  const provision = useMutation({
    mutationFn: async (enabled: boolean) => {
      const r = await fetch("/v1/workspace/provisioning", {
        credentials: "include",
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ enabled }),
      });
      if (!r.ok) throw new Error("Could not update provisioning");
    },
    onSuccess: () => qc.invalidateQueries({ queryKey: ["workspace"] }),
  });
  const resources = [
    ...(sources.data?.sources ?? []).map((s) => ({
      ...s,
      type: "knowledge" as const,
    })),
    ...(assistants.data?.assistants ?? []).map((s) => ({
      ...s,
      type: "assistant" as const,
    })),
    ...(integrations.data?.connections ?? [])
      .filter((item) => item.scope === "workspace")
      .map((item) => ({ ...item, type: "mcp" as const })),
  ];
  const startGroup = (group?: Group) => {
    setEditing(group?.id);
    setName(group?.name ?? "");
    setUsers(group?.user_ids ?? []);
    setMemberSearch("");
    save.reset();
    setOpen(true);
  };
  const directory = members.data?.members ?? [];
  const groups = access.data?.groups ?? [];
  const filteredGroups = groups.filter((group) =>
    group.name.toLowerCase().includes(search.toLowerCase()),
  );
  const resourceError = sources.error || assistants.error || integrations.error;
  return (
    <div className="space-y-5">
      <section className="rounded-xl border border-border bg-card">
        <div className="flex flex-wrap items-start justify-between gap-4 border-b border-border p-5">
          <div>
            <h2 className="text-base font-bold tracking-tight">Team groups</h2>
            <p className="mt-1 max-w-2xl text-sm leading-relaxed text-muted-foreground">
              Create groups like Legal or Marketing. Then share assistants,
              knowledge, and tool integrations with a group or with selected
              people.
            </p>
          </div>
          <Button
            size="sm"
            disabled={access.isPending || !!access.error}
            onClick={() => startGroup()}
          >
            <Plus data-icon="inline-start" />
            Create group
          </Button>
        </div>
        <div className="p-5">
          {access.isPending ? (
            <SkeletonRows count={3} />
          ) : access.error ? (
            <p role="alert" className="text-sm text-destructive">
              {access.error.message}{" "}
              <Button
                variant="outline"
                size="sm"
                onClick={() => access.refetch()}
              >
                Retry
              </Button>
            </p>
          ) : (
            <>
              {groups.length > 0 && (
                <Input
                  aria-label="Search groups"
                  className="mb-4 max-w-sm"
                  value={search}
                  onChange={(e) => setSearch(e.target.value)}
                  placeholder="Find a group…"
                />
              )}
              {!groups.length ? (
                <div className="flex flex-col items-center py-6 text-center">
                  <Users className="h-6 w-6 text-muted-foreground" />
                  <h3 className="mt-3 text-sm font-medium">
                    Your first team starts here
                  </h3>
                  <p className="mt-1 max-w-sm text-sm text-muted-foreground">
                    Add members to a group so you can share the right resources
                    with them together.
                  </p>
                  <Button
                    className="mt-4"
                    variant="outline"
                    size="sm"
                    onClick={() => startGroup()}
                  >
                    Create your first group
                  </Button>
                </div>
              ) : (
                <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
                  {filteredGroups.map((group) => (
                    <button
                      key={group.id}
                      type="button"
                      onClick={() => startGroup(group)}
                      className="rounded-xl border border-border p-4 text-left transition-colors hover:bg-muted/40 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                    >
                      <div className="flex items-center justify-between gap-3">
                        <span className="text-sm font-medium">
                          {group.name}
                        </span>
                        <Pencil className="h-3.5 w-3.5 text-muted-foreground" />
                      </div>
                      <p className="mt-2 text-xs text-muted-foreground">
                        {group.user_ids.length}{" "}
                        {group.user_ids.length === 1 ? "member" : "members"}
                      </p>
                      <p className="mt-1 truncate text-xs text-muted-foreground">
                        {group.user_ids
                          .map(
                            (id) =>
                              directory.find((member) => member.user_id === id)
                                ?.email,
                          )
                          .filter(Boolean)
                          .join(", ") || "No members added"}
                      </p>
                    </button>
                  ))}
                </div>
              )}
              {groups.length > 0 && !filteredGroups.length && (
                <p className="py-4 text-sm text-muted-foreground">
                  No groups match “{search}”.
                </p>
              )}
            </>
          )}
        </div>
      </section>
      <section className="rounded-xl border border-border bg-card">
        <div className="border-b border-border p-5">
          <h2 className="text-base font-bold tracking-tight">
            Resource sharing
          </h2>
          <p className="mt-1 text-sm text-muted-foreground">
            Choose who can use each resource. Owners and admins retain access.
            An assistant’s documents keep their own sharing rules.
          </p>
        </div>
        <div className="p-5">
          <div className="mb-4 flex flex-wrap gap-2" aria-label="Resource type">
            {[
              ["all", "All resources"],
              ["assistant", "Assistants"],
              ["knowledge", "Knowledge"],
              ["mcp", "Integrations"],
            ].map(([id, label]) => (
              <Button
                key={id}
                size="sm"
                variant={resourceType === id ? "secondary" : "outline"}
                aria-pressed={resourceType === id}
                onClick={() => setResourceType(id!)}
              >
                {label}
              </Button>
            ))}
          </div>
          {resourceError && (
            <p role="alert" className="mb-3 text-sm text-destructive">
              Some resources could not be loaded: {resourceError.message}
            </p>
          )}
          {sources.isPending ||
          assistants.isPending ||
          integrations.isPending ? (
            <SkeletonRows count={3} />
          ) : (
            <div className="divide-y divide-border">
              {resources
                .filter(
                  (resource) =>
                    resourceType === "all" || resource.type === resourceType,
                )
                .map((resource) => {
                  const policy = access.data?.resources.find(
                    (item) =>
                      item.id === resource.id && item.type === resource.type,
                  );
                  const sharing = resource.sharing ?? {
                    restricted:
                      policy?.restricted ?? Boolean(policy?.group_ids.length),
                    group_ids: policy?.group_ids ?? [],
                    user_ids: policy?.user_ids ?? [],
                  };
                  const Icon =
                    resource.type === "knowledge"
                      ? BookOpen
                      : resource.type === "assistant"
                        ? Bot
                        : Plug;
                  return (
                    <div
                      key={`${resource.type}:${resource.id}`}
                      className="flex items-center justify-between gap-3 py-4"
                    >
                      <div className="flex min-w-0 items-center gap-3">
                        <Icon className="h-4 w-4 shrink-0 text-muted-foreground" />
                        <div className="min-w-0">
                          <p className="truncate text-sm font-medium">
                            {resource.name}
                          </p>
                          <p className="mt-1 text-xs text-muted-foreground">
                            {resource.type === "mcp"
                              ? "Integration"
                              : resource.type === "knowledge"
                                ? "Knowledge source"
                                : "Assistant"}{" "}
                            · {sharingLabel(sharing)}
                          </p>
                        </div>
                      </div>
                      <ResourceSharingButton
                        type={resource.type}
                        id={resource.id}
                        name={resource.name}
                        sharing={sharing}
                      />
                    </div>
                  );
                })}
              {!resources.some(
                (resource) =>
                  resourceType === "all" || resource.type === resourceType,
              ) &&
                !resourceError && (
                  <p className="py-6 text-center text-sm text-muted-foreground">
                    No resources yet. Add an assistant, upload knowledge, or
                    connect an integration from the workspace navigation.
                  </p>
                )}
            </div>
          )}
        </div>
      </section>
      {provisioning.data && (
        <details className="rounded-xl border border-border bg-card p-5">
          <summary className="cursor-pointer text-sm font-medium">
            Directory provisioning · advanced
          </summary>
          <div className="mt-4">
            <label className="flex items-center gap-2 text-sm">
              <input
                type="checkbox"
                checked={provisioning.data.enabled}
                disabled={provision.isPending}
                onChange={(e) => provision.mutate(e.target.checked)}
              />
              Provision workspace access from SCIM
            </label>
            <p className="mt-2 text-xs leading-relaxed text-muted-foreground">
              Links verified identities to active directory users within your
              seat capacity. Directory groups can receive resource access.
              Deactivation removes managed employee memberships; owners and
              admins remain under manual control. Disabling sync retains
              existing memberships.
            </p>
            <p className="mt-2 text-xs text-muted-foreground">
              {provisioning.data.linked_members} linked members ·{" "}
              {provisioning.data.directory_users} active directory users.
              Unverified identities or full seats remain pending.
            </p>
            {provision.error && (
              <p role="alert" className="mt-2 text-sm text-destructive">
                {provision.error.message}
              </p>
            )}
          </div>
        </details>
      )}
      <Dialog
        open={open}
        onOpenChange={(next) => {
          if (!save.isPending) setOpen(next);
        }}
      >
        <DialogContent className="sm:max-w-lg">
          <DialogHeader>
            <DialogTitle>
              {editing ? "Edit team group" : "Create team group"}
            </DialogTitle>
            <DialogDescription>
              Choose a name and members. You can then share resources with this
              group.
            </DialogDescription>
          </DialogHeader>
          <label className="space-y-2 text-sm font-medium">
            Group name
            <Input
              aria-label="Group name"
              placeholder="e.g. Legal or Marketing"
              value={name}
              maxLength={120}
              onChange={(e) => setName(e.target.value)}
              disabled={save.isPending}
            />
          </label>
          <div>
            <p className="mb-2 text-sm font-medium">
              Members · {users.length} selected
            </p>
            <Input
              aria-label="Find group members"
              placeholder="Find a member by email…"
              value={memberSearch}
              onChange={(e) => setMemberSearch(e.target.value)}
            />
            {members.isPending ? (
              <p className="mt-3 text-sm text-muted-foreground">
                Loading members…
              </p>
            ) : members.error ? (
              <p role="alert" className="mt-3 text-sm text-destructive">
                {members.error.message}
              </p>
            ) : (
              <div className="mt-3 max-h-60 overflow-y-auto rounded-lg border border-border p-2">
                {directory
                  .filter((member) =>
                    member.email
                      .toLowerCase()
                      .includes(memberSearch.toLowerCase()),
                  )
                  .map((member) => (
                    <label
                      key={member.user_id}
                      className="flex min-h-10 items-center gap-3 rounded px-2 text-sm hover:bg-muted/40"
                    >
                      <input
                        type="checkbox"
                        checked={users.includes(member.user_id)}
                        disabled={save.isPending}
                        onChange={(e) =>
                          setUsers(
                            e.target.checked
                              ? [...users, member.user_id]
                              : users.filter((id) => id !== member.user_id),
                          )
                        }
                      />
                      <span className="truncate">{member.email}</span>
                    </label>
                  ))}
                {!directory.length && (
                  <p className="p-2 text-sm text-muted-foreground">
                    Invite members first from Members. You can also save an
                    empty group and add people later.
                  </p>
                )}
                {directory.length > 0 &&
                  !directory.some((member) =>
                    member.email
                      .toLowerCase()
                      .includes(memberSearch.toLowerCase()),
                  ) && (
                    <p className="p-2 text-sm text-muted-foreground">
                      No members match your search.
                    </p>
                  )}
              </div>
            )}
          </div>
          {save.error && (
            <p role="alert" className="text-sm text-destructive">
              {save.error.message}
            </p>
          )}
          <DialogFooter>
            <Button
              variant="outline"
              disabled={save.isPending}
              onClick={() => setOpen(false)}
            >
              Cancel
            </Button>
            <Button
              disabled={
                !name.trim() ||
                save.isPending ||
                members.isPending ||
                !!members.error
              }
              onClick={() => save.mutate()}
            >
              {save.isPending
                ? "Saving…"
                : editing
                  ? "Save group"
                  : "Create group"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
