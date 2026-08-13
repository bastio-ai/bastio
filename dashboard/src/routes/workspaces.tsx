import { useMemo, useState } from "react";
import { Link } from "@tanstack/react-router";
import {
  ArrowRight,
  Building2,
  Check,
  KeyRound,
  LockKeyhole,
  Pencil,
  Plus,
  ShieldCheck,
} from "lucide-react";

import {
  AdminPageHeader,
  AdminPanel,
  AdminSummaryStrip,
  SecurityNotice,
} from "@/components/admin/admin-primitives";
import { useHeaderExtension, type HeaderWorkspace } from "@/components/header-extension";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { cn } from "@/lib/utils";

type WorkspaceDialog =
  | { kind: "create" }
  | { kind: "rename"; workspace: HeaderWorkspace }
  | null;

export function WorkspacesPage() {
  const extension = useHeaderExtension();
  const workspaces = useMemo<HeaderWorkspace[]>(
    () => extension.workspaces?.length
      ? extension.workspaces
      : [{ id: "local", name: "Local deployment", detail: "Open source", role: "owner", isHome: true }],
    [extension.workspaces],
  );
  const activeID = extension.activeWorkspaceID ?? workspaces[0]?.id;
  const activeWorkspace = workspaces.find((workspace) => workspace.id === activeID) ?? workspaces[0];
  const ownedCount = workspaces.filter((workspace) => workspace.role === "owner").length;
  const [dialog, setDialog] = useState<WorkspaceDialog>(null);
  const [name, setName] = useState("");
  const [error, setError] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [switchingID, setSwitchingID] = useState<string>();

  const openCreate = () => {
    setName("");
    setError("");
    setDialog({ kind: "create" });
  };
  const openRename = (workspace: HeaderWorkspace) => {
    setName(workspace.name);
    setError("");
    setDialog({ kind: "rename", workspace });
  };

  const submitDialog = async () => {
    const nextName = name.trim();
    if (!dialog || nextName.length < 2 || submitting) return;
    setSubmitting(true);
    setError("");
    try {
      if (dialog.kind === "create") {
        if (!extension.onCreateWorkspace) throw new Error("Workspace creation is unavailable in this deployment");
        await extension.onCreateWorkspace(nextName);
      } else {
        if (!extension.onRenameWorkspace) throw new Error("Workspace renaming is unavailable in this deployment");
        await extension.onRenameWorkspace(dialog.workspace.id, nextName);
        setDialog(null);
      }
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "Unable to update workspace");
    } finally {
      setSubmitting(false);
    }
  };

  const switchWorkspace = async (workspaceID: string) => {
    if (!extension.onWorkspaceChange || workspaceID === activeID || switchingID) return;
    setSwitchingID(workspaceID);
    try {
      await extension.onWorkspaceChange(workspaceID);
    } finally {
      setSwitchingID(undefined);
    }
  };

  return (
    <>
      <AdminPageHeader
        eyebrow="Organization"
        title="Workspaces"
        description="Create and switch between isolated tenant boundaries without mixing credentials, telemetry, members, or billing."
        badge={<Badge variant="outline">{workspaces.length} accessible</Badge>}
        actions={extension.onCreateWorkspace ? (
          <Button size="sm" onClick={openCreate}>
            <Plus className="size-3.5" /> Create workspace
          </Button>
        ) : null}
      />

      <AdminSummaryStrip
        items={[
          { label: "Accessible", value: workspaces.length, detail: "Across your account" },
          { label: "Owned", value: ownedCount, detail: "Full administration" },
          { label: "Active workspace", value: activeWorkspace?.name ?? "—", detail: "Current data scope", tone: "success" },
          { label: "Isolation", value: "Strict", detail: "Tenant-bound access", tone: "success" },
        ]}
      />

      <SecurityNotice title="Workspace boundaries are security boundaries" className="mb-4">
        API keys, environments, policies, requests, threats, members, usage, and billing belong to exactly one workspace.
        Cloud workspaces and self-hosted OSS tenants are intentionally separate; an OSS tenant such as <span className="font-mono text-foreground">Default</span> remains in that deployment and does not appear in this cloud list.
      </SecurityNotice>

      <AdminPanel
        title="Your workspaces"
        description="Home is the workspace created with your account. Joined workspaces were shared with you by another team."
        contentClassName="p-0"
      >
        <div className="divide-y divide-border/60">
          {workspaces.map((workspace) => {
            const active = workspace.id === activeID;
            const canRename = Boolean(extension.onRenameWorkspace) && (workspace.role === "owner" || workspace.role === "admin");
            return (
              <article key={workspace.id} className={cn("group px-4 py-4 transition-colors hover:bg-muted/20", active && "bg-success-bg/40")}>
                <div className="flex flex-col gap-4 md:flex-row md:items-center">
                  <div className={cn("flex size-10 shrink-0 items-center justify-center rounded-lg border", active ? "border-success-border bg-success-bg text-success" : "border-border/70 bg-muted/30 text-muted-foreground")}>
                    <Building2 className="size-4" />
                  </div>
                  <div className="min-w-0 flex-1">
                    <div className="flex flex-wrap items-center gap-2">
                      <h2 className="truncate text-[13px] font-bold tracking-tight text-foreground">{workspace.name}</h2>
                      {active ? <Badge variant="success"><Check className="size-3" /> Active</Badge> : null}
                      {workspace.isHome ? <Badge variant="outline">Home</Badge> : null}
                      <Badge variant="secondary" className="capitalize">{workspace.role ?? "member"}</Badge>
                    </div>
                    <p className="mt-1 text-[11px] text-muted-foreground">
                      {workspace.isHome
                        ? "Original account workspace"
                        : workspace.role === "owner"
                          ? "Additional workspace you own"
                          : `Shared access · ${workspace.detail ?? workspace.role ?? "member"}`}
                    </p>
                    <div className="mt-2 flex flex-wrap gap-x-4 gap-y-1 text-[10px] text-muted-foreground">
                      <span className="inline-flex items-center gap-1.5"><KeyRound className="size-3" /> Dedicated credentials</span>
                      <span className="inline-flex items-center gap-1.5"><ShieldCheck className="size-3" /> Independent policy</span>
                      <span className="inline-flex items-center gap-1.5"><LockKeyhole className="size-3" /> Isolated telemetry</span>
                    </div>
                  </div>
                  <div className="flex shrink-0 flex-wrap items-center gap-2">
                    {canRename ? (
                      <Button variant="outline" size="sm" onClick={() => openRename(workspace)}>
                        <Pencil className="size-3.5" /> Rename
                      </Button>
                    ) : null}
                    {active ? (
                      <Button variant="outline" size="sm" render={<Link to="/api-keys" />}>
                        Manage API keys <ArrowRight className="size-3.5" />
                      </Button>
                    ) : (
                      <Button size="sm" disabled={!extension.onWorkspaceChange || Boolean(switchingID)} onClick={() => void switchWorkspace(workspace.id)}>
                        {switchingID === workspace.id ? "Switching…" : "Switch workspace"}
                      </Button>
                    )}
                  </div>
                </div>
              </article>
            );
          })}
        </div>
      </AdminPanel>

      <div className="mt-4 grid gap-4 lg:grid-cols-2">
        <AdminPanel title="What switching changes" description="Every workspace-scoped request is resolved against the active tenant.">
          <div className="grid gap-2 text-[11px] text-muted-foreground sm:grid-cols-2">
            {["API keys and gateways", "Environments and telemetry", "Security policies and findings", "Members, usage, and billing"].map((label) => (
              <div key={label} className="flex items-center gap-2 rounded-lg border border-border/60 bg-muted/20 px-3 py-2.5">
                <Check className="size-3.5 text-success" /> {label}
              </div>
            ))}
          </div>
        </AdminPanel>
        <AdminPanel title="Self-hosted deployments" description="Local OSS tenants remain administered by their own deployment.">
          <p className="text-[11px] leading-relaxed text-muted-foreground">
            Keys created under the reserved <span className="font-mono text-foreground">Default</span> tenant are still available in the OSS dashboard and database. They are not copied into Bastio Cloud automatically because that would cross a tenant and credential boundary.
          </p>
        </AdminPanel>
      </div>

      <Dialog open={Boolean(dialog)} onOpenChange={(open) => !open && setDialog(null)}>
        <DialogContent className="sm:max-w-md">
          <form onSubmit={(event) => { event.preventDefault(); void submitDialog(); }}>
            <DialogHeader>
              <DialogTitle>{dialog?.kind === "rename" ? "Rename workspace" : "Create workspace"}</DialogTitle>
              <DialogDescription>
                {dialog?.kind === "rename"
                  ? "The display name changes for every member. Workspace IDs and credentials remain unchanged."
                  : "Create a separate tenant with independent credentials, data, members, and billing."}
              </DialogDescription>
            </DialogHeader>
            <div className="space-y-4 py-5">
              <label className="block space-y-2 text-xs font-medium">
                Workspace name
                <Input autoFocus value={name} onChange={(event) => setName(event.target.value)} placeholder="Acme Security" minLength={2} maxLength={80} required />
              </label>
              <div className="rounded-lg border border-border/60 bg-muted/20 p-3 text-[11px] leading-relaxed text-muted-foreground">
                {dialog?.kind === "rename"
                  ? "Renaming does not move or rotate API keys. It is recorded in the workspace audit log."
                  : "A new production environment and 14-day trial are provisioned automatically."}
              </div>
              {error ? <p className="text-xs text-destructive">{error}</p> : null}
            </div>
            <DialogFooter>
              <Button type="button" variant="outline" onClick={() => setDialog(null)}>Cancel</Button>
              <Button type="submit" disabled={submitting || name.trim().length < 2}>
                {submitting ? "Saving…" : dialog?.kind === "rename" ? "Save name" : "Create workspace"}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>
    </>
  );
}
