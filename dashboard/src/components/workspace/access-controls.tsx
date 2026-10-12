import { useState } from "react";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { http, unwrap } from "@/api/typed";
import { ResourceSharingButton, sharingLabel, sharingMembers } from "./sharing-controls";
import { workspaceApi } from "./types";

type Group = { id: string; name: string; user_ids: string[] };
type Access = {
  groups: Group[];
  resources: { type: string; id: string; group_ids: string[]; user_ids?: string[]; restricted?: boolean }[];
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
      setName("");
      setUsers([]);
      setEditing(undefined);
      qc.invalidateQueries({ queryKey: ["workspace", "access"] });
    },
  });
  const integrations = useQuery({ queryKey: ["workspace-mcp-integrations"], queryFn: () => unwrap(http.GET("/v1/workspace/mcp")), gcTime: 0 });
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
    ...(sources.data?.sources ?? []).map((s) => ({ ...s, type: "knowledge" as const })),
    ...(assistants.data?.assistants ?? []).map((s) => ({
      ...s,
      type: "assistant" as const,
    })),
    ...(integrations.data?.connections ?? []).filter((item) => item.scope === "workspace").map((item) => ({ ...item, type: "mcp" as const })),
  ];
  return (
    <section className="mt-5 rounded-xl border border-border bg-card p-4">
      {provisioning.data && (
        <div className="mb-4 border-b border-border pb-4">
          <label className="flex items-center gap-2 text-xs font-medium">
            <input
              type="checkbox"
              checked={provisioning.data.enabled}
              disabled={provision.isPending}
              onChange={(e) => provision.mutate(e.target.checked)}
            />
            Provision Workspace access from SCIM
          </label>
          <p className="mt-2 text-xs text-muted-foreground">
            Links verified Bastio identities to active directory users within
            the seat limit. SCIM groups can receive access to knowledge, assistants and integrations. Deactivation
            removes managed employee memberships; owners and admins remain under
            manual control. Disabling sync retains existing memberships.
          </p>
          <p className="mt-2 text-xs">
            {provisioning.data.linked_members} linked members ·{" "}
            {provisioning.data.directory_users} active directory users.
            Unverified identities or full seats remain pending.
          </p>
          {provision.error && (
            <p role="alert" className="mt-2 text-xs text-destructive">
              {provision.error.message}
            </p>
          )}
        </div>
      )}
      <h2 className="text-[13px] font-bold tracking-tight">
        Teams and sharing
      </h2>
      <p className="mt-2 text-xs text-muted-foreground">
        Share knowledge, assistants and integrations with groups, selected people, or everyone. Restricted resources with no recipients remain available only to owners and admins.
      </p>
      <div className="mt-3 flex flex-wrap gap-2">
        {access.data?.groups.map((g) => (
          <Button
            key={g.id}
            variant="outline"
            size="sm"
            onClick={() => {
              setEditing(g.id);
              setName(g.name);
              setUsers(g.user_ids);
            }}
          >
            {g.name} · {g.user_ids.length}
          </Button>
        ))}
      </div>
      <Input
        className="mt-3 max-w-xs"
        placeholder="Group name, e.g. Finance"
        aria-label="Access group name"
        value={name}
        onChange={(e) => setName(e.target.value)}
      />
      <div className="my-3 flex flex-wrap gap-3">
        {members.data?.members.map((m) => (
          <label key={m.user_id} className="flex items-center gap-1 text-xs">
            <input
              type="checkbox"
              checked={users.includes(m.user_id)}
              onChange={(e) =>
                setUsers(
                  e.target.checked
                    ? [...users, m.user_id]
                    : users.filter((id) => id !== m.user_id),
                )
              }
            />
            {m.email}
          </label>
        ))}
      </div>
      <Button
        size="sm"
        disabled={!name.trim() || save.isPending}
        onClick={() => save.mutate()}
      >
        {editing ? "Update group" : "Create group"}
      </Button>
      {editing && (
        <Button
          className="ml-2"
          variant="ghost"
          size="sm"
          onClick={() => {
            setEditing(undefined);
            setName("");
            setUsers([]);
          }}
        >
          Cancel
        </Button>
      )}
      {(save.error || access.error || members.error) && (
        <p role="alert" className="mt-2 text-xs text-destructive">
          {(save.error ?? access.error ?? members.error)?.message}
        </p>
      )}
      <div className="mt-4 divide-y divide-border">
        {resources.map((resource) => {
          const policy = access.data?.resources.find((item) => item.id === resource.id && item.type === resource.type);
          const sharing = resource.sharing ?? { restricted: policy?.restricted ?? Boolean(policy?.group_ids.length), group_ids: policy?.group_ids ?? [], user_ids: policy?.user_ids ?? [] };
          return <div key={`${resource.type}:${resource.id}`} className="flex items-center justify-between gap-3 py-3"><div><p className="text-xs font-medium">{resource.name} <span className="text-muted-foreground">· {resource.type}</span></p><p className="mt-1 text-[11px] text-muted-foreground">{sharingLabel(sharing)}</p></div><ResourceSharingButton type={resource.type} id={resource.id} name={resource.name} sharing={sharing} /></div>;

        })}
      </div>
    </section>
  );
}
