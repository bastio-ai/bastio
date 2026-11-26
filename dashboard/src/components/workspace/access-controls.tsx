import { Plus, Pencil, BookOpen, Bot, Plug } from "lucide-react";
import { SkeletonRows } from "@/components/skeleton";
import {
  AdminPanel,
  AdminSummaryStrip,
  FieldLabel,
} from "@/components/admin/admin-primitives";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { useId, useState } from "react";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { http, unwrap } from "@/api/typed";
import { cn } from "@/lib/utils";
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
  const uid = useId();
  const groupSearchId = `${uid}-group-search`;
  const groupNameId = `${uid}-group-name`;
  const memberSearchId = `${uid}-member-search`;
  const scimId = `${uid}-scim-provisioning`;

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

  const resolveSharing = (resource: (typeof resources)[number]) => {
    const policy = access.data?.resources.find(
      (item) => item.id === resource.id && item.type === resource.type,
    );
    return (
      resource.sharing ?? {
        restricted: policy?.restricted ?? Boolean(policy?.group_ids.length),
        group_ids: policy?.group_ids ?? [],
        user_ids: policy?.user_ids ?? [],
      }
    );
  };

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
  const resourcesLoading =
    sources.isPending || assistants.isPending || integrations.isPending;
  const restrictedCount = resources.filter(
    (resource) => resolveSharing(resource).restricted,
  ).length;
  const visibleResources = resources.filter(
    (resource) => resourceType === "all" || resource.type === resourceType,
  );

  return (
    <div>
      <AdminSummaryStrip
        items={[
          {
            label: "Team groups",
            value: access.isPending ? "—" : groups.length,
            detail: "Configured access groups",
          },
          {
            label: "Members",
            value:
              members.isPending || members.data === null
                ? "—"
                : directory.length,
            detail:
              members.data === null
                ? "Group-level access"
                : "Workspace accounts",
          },
          {
            label: "Resources",
            value: resourcesLoading ? "—" : resources.length,
            detail: "Assistants, sources, and tools",
          },
          {
            label: "Restricted",
            value: resourcesLoading ? "—" : restrictedCount,
            detail: "Scoped to groups or people",
            tone: restrictedCount > 0 ? "warning" : "default",
          },
        ]}
      />

      <div className="space-y-8">
        <AdminPanel
          title="Team groups"
          description="Create groups like Legal or Marketing. Then share assistants, knowledge, and tool integrations with a group or with selected people."
          action={
            <Button
              type="button"
              size="sm"
              className="h-8 text-xs"
              disabled={access.isPending || !!access.error}
              onClick={() => startGroup()}
            >
              <Plus className="size-3.5" aria-hidden="true" />
              Create group
            </Button>
          }
          contentClassName="py-0"
        >
          {access.isPending ? (
            <div className="border-b border-border-subtle py-4">
              <SkeletonRows count={3} />
            </div>
          ) : access.error ? (
            <div
              role="alert"
              className="flex flex-wrap items-center justify-between gap-3 border-b border-border-subtle py-4"
            >
              <p className="border-l-2 border-destructive pl-3.5 text-xs text-destructive">
                {access.error.message}
              </p>
              <Button
                type="button"
                variant="outline"
                size="xs"
                onClick={() => access.refetch()}
              >
                Retry
              </Button>
            </div>
          ) : !groups.length ? (
            <div className="border-b border-border-subtle py-10 text-center">
              <h3 className="text-[13px] font-medium text-foreground">
                Your first team starts here
              </h3>
              <p className="mx-auto mt-1 max-w-sm text-xs leading-relaxed text-muted-foreground">
                Add members to a group so you can share the right resources with
                them together.
              </p>
              <Button
                type="button"
                className="mt-4 h-8 text-xs"
                variant="outline"
                size="sm"
                onClick={() => startGroup()}
              >
                Create your first group
              </Button>
            </div>
          ) : (
            <>
              <search className="border-b border-border-subtle py-3">
                <label htmlFor={groupSearchId} className="sr-only">
                  Search groups
                </label>
                <Input
                  id={groupSearchId}
                  className="max-w-sm"
                  value={search}
                  onChange={(e) => setSearch(e.target.value)}
                  placeholder="Find a group…"
                  autoCorrect="off"
                  autoCapitalize="none"
                  spellCheck={false}
                />
              </search>
              {filteredGroups.length > 0 ? (
                <div className="divide-y divide-border-subtle border-b border-border-subtle">
                  {filteredGroups.map((group) => {
                    const memberEmails = group.user_ids
                      .map(
                        (id) =>
                          directory.find((member) => member.user_id === id)
                            ?.email,
                      )
                      .filter(Boolean)
                      .join(", ");
                    return (
                      <button
                        key={group.id}
                        type="button"
                        onClick={() => startGroup(group)}
                        className="group flex w-full cursor-pointer items-center justify-between gap-4 py-3.5 text-left transition-colors hover:bg-surface-1/50 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring"
                      >
                        <div className="min-w-0 flex-1">
                          <p className="truncate text-[13px] font-medium text-foreground">
                            {group.name}
                          </p>
                          <p className="mt-0.5 truncate text-xs text-muted-foreground">
                            {memberEmails || "No members added"}
                          </p>
                        </div>
                        <div className="flex shrink-0 items-center gap-3">
                          <span className="font-mono text-[11px] tabular-nums text-muted-foreground">
                            {group.user_ids.length}{" "}
                            {group.user_ids.length === 1 ? "member" : "members"}
                          </span>
                          <Pencil
                            className="size-3.5 text-muted-foreground transition-colors group-hover:text-foreground"
                            aria-hidden="true"
                          />
                        </div>
                      </button>
                    );
                  })}
                </div>
              ) : (
                <p className="border-b border-border-subtle py-6 text-xs text-muted-foreground">
                  No groups match “{search}”.
                </p>
              )}
            </>
          )}
        </AdminPanel>

        <AdminPanel
          title="Resource sharing"
          description="Choose who can use each resource. Owners and admins retain access. An assistant’s documents keep their own sharing rules."
          contentClassName="py-0"
        >
          <search
            aria-label="Resource type"
            className="flex flex-wrap items-center gap-1.5 border-b border-border-subtle py-3"
          >
            {[
              ["all", "All resources"],
              ["assistant", "Assistants"],
              ["knowledge", "Knowledge"],
              ["mcp", "Integrations"],
            ].map(([id, label]) => (
              <Button
                key={id}
                type="button"
                size="xs"
                variant={resourceType === id ? "secondary" : "ghost"}
                aria-pressed={resourceType === id}
                onClick={() => setResourceType(id!)}
              >
                {label}
              </Button>
            ))}
          </search>

          {resourceError ? (
            <div role="alert" className="border-b border-border-subtle py-3">
              <p className="border-l-2 border-destructive pl-3.5 text-xs text-destructive">
                Some resources could not be loaded: {resourceError.message}
              </p>
            </div>
          ) : null}

          {resourcesLoading ? (
            <div className="border-b border-border-subtle py-4">
              <SkeletonRows count={3} />
            </div>
          ) : visibleResources.length > 0 ? (
            <div className="divide-y divide-border-subtle border-b border-border-subtle">
              {visibleResources.map((resource) => {
                const sharing = resolveSharing(resource);
                const Icon =
                  resource.type === "knowledge"
                    ? BookOpen
                    : resource.type === "assistant"
                      ? Bot
                      : Plug;
                const typeLabel =
                  resource.type === "mcp"
                    ? "Integration"
                    : resource.type === "knowledge"
                      ? "Knowledge source"
                      : "Assistant";
                return (
                  <div
                    key={`${resource.type}:${resource.id}`}
                    className="flex items-center justify-between gap-4 py-3.5 transition-colors hover:bg-surface-1/50"
                  >
                    <div className="flex min-w-0 items-center gap-3">
                      <Icon
                        className="size-3.5 shrink-0 text-muted-foreground"
                        aria-hidden="true"
                      />
                      <div className="min-w-0">
                        <div className="flex items-center gap-2">
                          <p className="truncate text-[13px] font-medium text-foreground">
                            {resource.name}
                          </p>
                          <span
                            aria-hidden="true"
                            className={cn(
                              "size-1.5 shrink-0 rounded-full",
                              sharing.restricted ? "bg-warn" : "bg-success",
                            )}
                          />
                        </div>
                        <p className="mt-0.5 text-xs text-muted-foreground">
                          {typeLabel} · {sharingLabel(sharing)}
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
            </div>
          ) : !resourceError ? (
            <p className="border-b border-border-subtle py-8 text-center text-xs text-muted-foreground">
              No resources yet. Add an assistant, upload knowledge, or connect
              an integration from the workspace navigation.
            </p>
          ) : null}
        </AdminPanel>

        {provisioning.data ? (
          <details className="border-y border-border-subtle py-4">
            <summary className="cursor-pointer text-[13px] font-medium text-foreground">
              Directory provisioning · advanced
            </summary>
            <div className="mt-3 space-y-2.5">
              <label
                htmlFor={scimId}
                className="flex cursor-pointer items-center gap-2.5 text-xs font-medium text-foreground"
              >
                <input
                  id={scimId}
                  type="checkbox"
                  checked={provisioning.data.enabled}
                  disabled={provision.isPending}
                  onChange={(e) => provision.mutate(e.target.checked)}
                />
                Provision workspace access from SCIM
              </label>
              <p className="text-xs leading-relaxed text-muted-foreground">
                Links verified identities to active directory users within your
                seat capacity. Directory groups can receive resource access.
                Deactivation removes managed employee memberships; owners and
                admins remain under manual control. Disabling sync retains
                existing memberships.
              </p>
              <p className="font-mono text-[11px] text-muted-foreground">
                {provisioning.data.linked_members} linked members ·{" "}
                {provisioning.data.directory_users} active directory users.
                Unverified identities or full seats remain pending.
              </p>
              {provision.error ? (
                <p
                  role="alert"
                  className="border-l-2 border-destructive pl-3.5 text-xs text-destructive"
                >
                  {provision.error.message}
                </p>
              ) : null}
            </div>
          </details>
        ) : null}
      </div>

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
          <div>
            <FieldLabel htmlFor={groupNameId}>Group name</FieldLabel>
            <Input
              id={groupNameId}
              placeholder="e.g. Legal or Marketing"
              value={name}
              maxLength={120}
              onChange={(e) => setName(e.target.value)}
              disabled={save.isPending}
            />
          </div>
          <div>
            <div className="mb-1.5 flex items-center justify-between gap-3">
              <label
                htmlFor={memberSearchId}
                className="text-xs font-medium text-foreground"
              >
                Find group members
              </label>
              <span className="font-mono text-[11px] tabular-nums text-muted-foreground">
                Members · {users.length} selected
              </span>
            </div>
            <search>
              <Input
                id={memberSearchId}
                placeholder="Find a member by email…"
                value={memberSearch}
                onChange={(e) => setMemberSearch(e.target.value)}
                autoCorrect="off"
                autoCapitalize="none"
                spellCheck={false}
              />
            </search>
            {members.isPending ? (
              <p className="mt-3 text-xs text-muted-foreground">
                Loading members…
              </p>
            ) : members.error ? (
              <p
                role="alert"
                className="mt-3 border-l-2 border-destructive pl-3 text-xs text-destructive"
              >
                {members.error.message}
              </p>
            ) : (
              <div className="mt-3 max-h-60 divide-y divide-border-subtle overflow-y-auto border-y border-border-subtle">
                {directory
                  .filter((member) =>
                    member.email
                      .toLowerCase()
                      .includes(memberSearch.toLowerCase()),
                  )
                  .map((member) => (
                    <label
                      key={member.user_id}
                      className="flex min-h-9 cursor-pointer items-center gap-3 px-2 py-1.5 text-xs text-foreground transition-colors hover:bg-surface-1/50"
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
                {!directory.length ? (
                  <p className="py-3 text-xs text-muted-foreground">
                    Invite members first from Members. You can also save an
                    empty group and add people later.
                  </p>
                ) : null}
                {directory.length > 0 &&
                !directory.some((member) =>
                  member.email
                    .toLowerCase()
                    .includes(memberSearch.toLowerCase()),
                ) ? (
                  <p className="py-3 text-xs text-muted-foreground">
                    No members match your search.
                  </p>
                ) : null}
              </div>
            )}
          </div>
          {save.error ? (
            <p role="alert" className="text-xs text-destructive">
              {save.error.message}
            </p>
          ) : null}
          <DialogFooter>
            <Button
              type="button"
              variant="outline"
              disabled={save.isPending}
              onClick={() => setOpen(false)}
            >
              Cancel
            </Button>
            <Button
              type="button"
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
