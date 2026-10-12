import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Users } from "lucide-react";
import { http, unwrap } from "@/api/typed";
import type { components } from "@/api/schema";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import type { Member } from "./types";

export type ResourceSharing = components["schemas"]["WorkspaceResourceSharing"];
export const everyoneSharing = (): ResourceSharing => ({ restricted: false, group_ids: [], user_ids: [] });
export const adminOnlySharing = (): ResourceSharing => ({ restricted: true, group_ids: [], user_ids: [] });

export async function sharingMembers(): Promise<{ members: Member[] } | null> {
  const response = await fetch("/v1/workspace/members", { credentials: "include" });
  if (response.status === 404) return null; // OSS has no employee directory.
  if (!response.ok) throw new Error("Could not load workspace members");
  return response.json() as Promise<{ members: Member[] }>;
}

export function sharingLabel(sharing?: ResourceSharing) {
  if (!sharing?.restricted) return "Everyone in workspace";
  const groups = sharing.group_ids.length;
  const people = sharing.user_ids.length;
  if (!groups && !people) return "Owners and admins only";
  return [groups ? `${groups} ${groups === 1 ? "group" : "groups"}` : "", people ? `${people} ${people === 1 ? "person" : "people"}` : ""].filter(Boolean).join(" + ");
}

export function SharingPicker({ value, onChange, disabled = false }: { value: ResourceSharing; onChange: (value: ResourceSharing) => void; disabled?: boolean }) {
  const [search, setSearch] = useState("");
  const access = useQuery({ queryKey: ["workspace", "access"], queryFn: () => unwrap(http.GET("/v1/workspace/access")), gcTime: 0 });
  const members = useQuery({ queryKey: ["workspace", "members"], queryFn: sharingMembers, gcTime: 0 });
  const toggle = (key: "group_ids" | "user_ids", id: string, checked: boolean) => onChange({ ...value, [key]: checked ? [...value[key], id] : value[key].filter((item) => item !== id) });
  const term = search.trim().toLowerCase();
  return (
    <fieldset disabled={disabled} className="space-y-3 rounded-lg border border-border bg-muted/10 p-3">
      <legend className="px-1 text-xs font-medium">Who can use this?</legend>
      <label className="flex items-center gap-2 text-xs"><input type="radio" checked={!value.restricted} onChange={() => onChange(everyoneSharing())} /> Everyone in this workspace</label>
      <label className="flex items-center gap-2 text-xs"><input type="radio" checked={value.restricted} onChange={() => onChange(adminOnlySharing())} /> Selected groups or people</label>
      {value.restricted && <>
        <Input aria-label="Find groups or people" placeholder="Find a group or person…" value={search} onChange={(event) => setSearch(event.target.value)} />
        {(access.isPending || members.isPending) && <p className="text-xs text-muted-foreground">Loading workspace recipients…</p>}
        {(access.error || members.error) && <p role="alert" className="text-xs text-destructive">Could not load recipients. Close and reopen sharing to retry.</p>}
        <div className="grid gap-3 sm:grid-cols-2">
          <div><p className="mb-2 text-[10px] font-medium uppercase tracking-wider text-muted-foreground">Groups</p><div className="max-h-36 space-y-1 overflow-y-auto">
            {access.data?.groups?.filter((group) => group.name.toLowerCase().includes(term)).map((group) => <label key={group.id} className="flex items-center gap-2 rounded px-1 py-1 text-xs hover:bg-muted/40"><input type="checkbox" checked={value.group_ids.includes(group.id!)} onChange={(event) => toggle("group_ids", group.id!, event.target.checked)} />{group.name}<span className="ml-auto font-mono text-[10px] text-muted-foreground">{group.user_ids.length}</span></label>)}
            {!access.isPending && !access.data?.groups?.length && <p className="text-[11px] text-muted-foreground">Create groups under Workspace → Groups & sharing.</p>}
          </div></div>
          <div><p className="mb-2 text-[10px] font-medium uppercase tracking-wider text-muted-foreground">People</p><div className="max-h-36 space-y-1 overflow-y-auto">
            {members.data === null && <p className="text-[11px] text-muted-foreground">Selected people are available in Cloud workspaces with employee accounts.</p>}
            {members.data?.members.filter((member) => member.email.toLowerCase().includes(term)).map((member) => <label key={member.user_id} className="flex items-center gap-2 rounded px-1 py-1 text-xs hover:bg-muted/40"><input type="checkbox" checked={value.user_ids.includes(member.user_id)} onChange={(event) => toggle("user_ids", member.user_id, event.target.checked)} /><span className="truncate">{member.email}</span></label>)}
          </div></div>
        </div>
        <p className="text-[11px] text-muted-foreground">{value.group_ids.length} {value.group_ids.length === 1 ? "group" : "groups"} and {value.user_ids.length} {value.user_ids.length === 1 ? "person" : "people"} selected. Anyone in a selected group or selected individually can use this. No selections keeps it available only to owners and admins.</p>
      </>}
      <p className="text-[11px] text-muted-foreground">Owners and admins retain access to manage shared resources. Removing a group member removes that group’s grant; individual grants remain until removed.</p>
    </fieldset>
  );
}

export function ResourceSharingButton({ type, id, name, sharing }: { type: "knowledge" | "assistant" | "mcp"; id: string; name: string; sharing?: ResourceSharing }) {
  const qc = useQueryClient();
  const [open, setOpen] = useState(false);
  const [value, setValue] = useState<ResourceSharing>(sharing ?? everyoneSharing());
  const save = useMutation({ mutationFn: () => unwrap(http.PUT("/v1/workspace/access/resources", { body: { type, id, ...value } })), onSuccess: () => { setOpen(false); qc.invalidateQueries({ queryKey: ["workspace"] }); qc.invalidateQueries({ queryKey: ["workspace-mcp-integrations"] }); } });
  return <>
    <Button type="button" size="xs" variant="outline" aria-label={`Share ${name}`} onClick={() => { setValue(sharing ?? everyoneSharing()); save.reset(); setOpen(true); }}><Users className="h-3 w-3" /> Share</Button>
    <Dialog open={open} onOpenChange={setOpen}><DialogContent className="sm:max-w-xl"><DialogHeader><DialogTitle>Share {name}</DialogTitle><DialogDescription>Choose who can use this {type === "mcp" ? "integration" : type === "knowledge" ? "knowledge source" : "assistant"}.</DialogDescription></DialogHeader><SharingPicker value={value} onChange={setValue} disabled={save.isPending} />{type === "assistant" && <p className="text-xs text-muted-foreground">Knowledge keeps its own sharing rules. Sharing this assistant does not grant access to its attached documents.</p>}{save.error && <p role="alert" className="text-xs text-destructive">{save.error.message}</p>}<DialogFooter><Button type="button" variant="outline" onClick={() => setOpen(false)}>Cancel</Button><Button type="button" disabled={save.isPending} onClick={() => save.mutate()}>{save.isPending ? "Saving…" : "Save sharing"}</Button></DialogFooter></DialogContent></Dialog>
  </>;
}
