import { useMemo, useState } from "react";
import { Link } from "@tanstack/react-router";
import { ArrowRight, Check, Pencil, Plus } from "lucide-react";

import {
  AdminPageHeader,
  AdminPanel,
  AdminSummaryStrip,
  FieldLabel,
} from "@/components/admin/admin-primitives";
import {
  useHeaderExtension,
  type HeaderWorkspace,
} from "@/components/header-extension";
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
    () =>
      extension.workspaces?.length
        ? extension.workspaces
        : [
            {
              id: "local",
              name: "Local deployment",
              detail: "Open source",
              role: "owner",
              isHome: true,
            },
          ],
    [extension.workspaces],
  );
  const activeID = extension.activeWorkspaceID ?? workspaces[0]?.id;
  const activeWorkspace =
    workspaces.find((workspace) => workspace.id === activeID) ?? workspaces[0];
  const ownedCount = workspaces.filter(
    (workspace) => workspace.role === "owner",
  ).length;
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
    if (!dialog || submitting) return;
    if (nextName.length < 2) {
      setError("Enter a workspace name with at least 2 characters.");
      return;
    }
    setSubmitting(true);
    setError("");
    try {
      if (dialog.kind === "create") {
        if (!extension.onCreateWorkspace)
          throw new Error("Workspace creation is unavailable in this deployment");
        await extension.onCreateWorkspace(nextName);
      } else {
        if (!extension.onRenameWorkspace)
          throw new Error("Workspace renaming is unavailable in this deployment");
        await extension.onRenameWorkspace(dialog.workspace.id, nextName);
        setDialog(null);
      }
    } catch (cause) {
      setError(
        cause instanceof Error ? cause.message : "Unable to update workspace",
      );
    } finally {
      setSubmitting(false);
    }
  };

  const switchWorkspace = async (workspaceID: string) => {
    if (!extension.onWorkspaceChange || workspaceID === activeID || switchingID)
      return;
    setSwitchingID(workspaceID);
    try {
      await extension.onWorkspaceChange(workspaceID);
    } finally {
      setSwitchingID(undefined);
    }
  };

  return (
    <div className="mx-auto max-w-[1180px] pb-10 pt-2 md:pt-4">
      <AdminPageHeader
        eyebrow="Organization"
        title="Workspaces"
        description="Create and switch between isolated tenant boundaries without mixing credentials, telemetry, members, or billing."
        bordered={false}
        className="mb-4"
        badge={
          <span className="font-mono text-[11px] tabular-nums text-muted-foreground">
            {workspaces.length} accessible
          </span>
        }
        actions={
          extension.onCreateWorkspace ? (
            <Button
              type="button"
              size="sm"
              className="h-8 text-xs"
              onClick={openCreate}
            >
              <Plus className="size-3.5" aria-hidden="true" />
              Create workspace
            </Button>
          ) : null
        }
      />

      <AdminSummaryStrip
        items={[
          {
            label: "Accessible",
            value: workspaces.length,
            detail: "Across your account",
          },
          {
            label: "Owned",
            value: ownedCount,
            detail: "Full administration",
          },
          {
            label: "Active workspace",
            value: activeWorkspace?.name ?? "—",
            detail: "Current data scope",
            tone: "success",
          },
          {
            label: "Isolation",
            value: "Strict",
            detail: "Tenant-bound access",
            tone: "success",
          },
        ]}
      />

      <div className="mb-6 border-l-2 border-border-subtle pl-3.5 text-xs leading-relaxed text-muted-foreground">
        <span className="font-medium text-foreground">
          Workspace boundaries are security boundaries.
        </span>{" "}
        API keys, environments, policies, requests, threats, members, usage, and
        billing belong to a single workspace. Cloud workspaces and self-hosted
        OSS tenants remain strictly separate.
      </div>

      <AdminPanel
        title="Your workspaces"
        description="Home is the workspace created with your account. Joined workspaces were shared with you by another team."
        contentClassName="py-0"
      >
        <div className="divide-y divide-border-subtle border-b border-border-subtle">
          {workspaces.map((workspace) => {
            const active = workspace.id === activeID;
            const canRename =
              Boolean(extension.onRenameWorkspace) &&
              (workspace.role === "owner" || workspace.role === "admin");
            return (
              <article
                key={workspace.id}
                className={cn(
                  "flex flex-col gap-3 py-4 transition-colors md:flex-row md:items-center md:justify-between",
                  active && "bg-surface-1/50 px-3 -mx-3 rounded-sm",
                )}
              >
                <div className="min-w-0 flex-1">
                  <div className="flex flex-wrap items-center gap-2">
                    <span
                      aria-hidden="true"
                      className={cn(
                        "size-1.5 shrink-0 rounded-full",
                        active ? "bg-success" : "bg-muted-foreground/40",
                      )}
                    />
                    <h3 className="truncate text-[13px] font-medium text-foreground">
                      {workspace.name}
                    </h3>
                    {active ? (
                      <span className="font-mono text-[11px] text-success">
                        active
                      </span>
                    ) : null}
                    {workspace.isHome ? (
                      <span className="font-mono text-[11px] text-muted-foreground">
                        home
                      </span>
                    ) : null}
                    <span className="font-mono text-[11px] text-muted-foreground">
                      {workspace.role ?? "member"}
                    </span>
                  </div>
                  <p className="mt-1 pl-3.5 text-xs text-muted-foreground">
                    {workspace.isHome
                      ? "Original account workspace"
                      : workspace.role === "owner"
                        ? "Additional workspace you own"
                        : `Shared access · ${workspace.detail ?? workspace.role ?? "member"}`}
                  </p>
                  <div className="mt-1.5 flex flex-wrap gap-x-4 gap-y-1 pl-3.5 font-mono text-[11px] text-muted-foreground">
                    <span>Dedicated credentials</span>
                    <span>Independent policy</span>
                    <span>Isolated telemetry</span>
                  </div>
                </div>

                <div className="flex shrink-0 flex-wrap items-center gap-2 pl-3.5 md:pl-0">
                  {canRename ? (
                    <Button
                      type="button"
                      variant="outline"
                      size="sm"
                      className="h-8 text-xs"
                      onClick={() => openRename(workspace)}
                    >
                      <Pencil className="size-3.5" aria-hidden="true" />
                      Rename
                    </Button>
                  ) : null}
                  {active ? (
                    <Button
                      nativeButton={false}
                      variant="outline"
                      size="sm"
                      className="h-8 text-xs"
                      render={<Link to="/api-keys" />}
                    >
                      Manage API keys
                      <ArrowRight className="size-3.5" aria-hidden="true" />
                    </Button>
                  ) : (
                    <Button
                      type="button"
                      size="sm"
                      className="h-8 text-xs"
                      disabled={
                        !extension.onWorkspaceChange || Boolean(switchingID)
                      }
                      onClick={() => void switchWorkspace(workspace.id)}
                    >
                      {switchingID === workspace.id
                        ? "Switching…"
                        : "Switch workspace"}
                    </Button>
                  )}
                </div>
              </article>
            );
          })}
        </div>
      </AdminPanel>

      <div className="mt-8 grid gap-8 lg:grid-cols-2">
        <AdminPanel
          title="What switching changes"
          description="Every workspace-scoped request resolves against the active tenant."
        >
          <ul className="divide-y divide-border-subtle border-b border-border-subtle text-xs text-muted-foreground">
            {[
              "API keys and gateways",
              "Environments and telemetry",
              "Security policies and findings",
              "Members, usage, and billing",
            ].map((label) => (
              <li key={label} className="flex items-center gap-2 py-2.5">
                <Check className="size-3.5 text-success" aria-hidden="true" />
                <span className="text-foreground">{label}</span>
              </li>
            ))}
          </ul>
        </AdminPanel>

        <AdminPanel
          title="Self-hosted deployments"
          description="Local OSS tenants remain administered by their own deployment."
        >
          <p className="border-b border-border-subtle pb-4 text-xs leading-relaxed text-muted-foreground">
            Keys created under the reserved{" "}
            <code className="font-mono text-foreground">Default</code> tenant
            remain in the self-hosted OSS dashboard and database. They are never
            copied into Bastio Cloud automatically because that would cross a
            tenant and credential boundary.
          </p>
        </AdminPanel>
      </div>

      <Dialog
        open={Boolean(dialog)}
        onOpenChange={(open) => !open && setDialog(null)}
      >
        <DialogContent className="sm:max-w-md">
          <form
            noValidate
            onSubmit={(event) => {
              event.preventDefault();
              void submitDialog();
            }}
          >
            <DialogHeader>
              <DialogTitle>
                {dialog?.kind === "rename"
                  ? "Rename workspace"
                  : "Create workspace"}
              </DialogTitle>
              <DialogDescription>
                {dialog?.kind === "rename"
                  ? "The display name changes for every member. Workspace IDs and credentials remain unchanged."
                  : "Create a separate tenant with independent credentials, data, members, and billing."}
              </DialogDescription>
            </DialogHeader>
            <div className="space-y-4 py-5">
              <div>
                <FieldLabel htmlFor="workspace-dialog-name">
                  Workspace name
                </FieldLabel>
                <Input
                  id="workspace-dialog-name"
                  autoFocus
                  value={name}
                  onChange={(event) => {
                    setName(event.target.value);
                    if (error) setError("");
                  }}
                  placeholder="Acme Security"
                  minLength={2}
                  maxLength={80}
                  required
                  aria-invalid={Boolean(error)}
                  aria-describedby={error ? "workspace-dialog-error" : undefined}
                />
              </div>
              <p className="border-l-2 border-border-subtle pl-3 text-xs leading-relaxed text-muted-foreground">
                {dialog?.kind === "rename"
                  ? "Renaming does not move or rotate API keys. It is recorded in the workspace audit log."
                  : "A new production environment and 14-day trial are provisioned automatically."}
              </p>
              {error ? (
                <p
                  id="workspace-dialog-error"
                  role="alert"
                  className="text-xs text-destructive"
                >
                  {error}
                </p>
              ) : null}
            </div>
            <DialogFooter>
              <Button
                type="button"
                variant="outline"
                size="sm"
                className="h-8 text-xs"
                onClick={() => setDialog(null)}
              >
                Cancel
              </Button>
              <Button
                type="submit"
                size="sm"
                className="h-8 text-xs"
                disabled={submitting}
              >
                {submitting
                  ? "Saving…"
                  : dialog?.kind === "rename"
                    ? "Save name"
                    : "Create workspace"}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>
    </div>
  );
}
