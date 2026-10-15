import { useId, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { History, LockKeyhole, Trash2 } from "lucide-react";
import type { components } from "@/api/schema";
import { http, unwrap } from "@/api/typed";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { SharingPicker } from "./sharing-controls";

type Policy = components["schemas"]["WorkspaceMCPHistoryPolicy"];
const policyKey = ["workspace", "mcp-history-policy"];
export function MCPHistorySettings() {
  const qc = useQueryClient();
  const modeName = useId();
  const settings = useQuery({
    queryKey: policyKey,
    queryFn: () => unwrap(http.GET("/v1/workspace/mcp/history/policy")),
    gcTime: 0,
    staleTime: 0,
  });
  const connections = useQuery({
    queryKey: ["workspace-mcp-integrations"],
    queryFn: () => unwrap(http.GET("/v1/workspace/mcp")),
    gcTime: 0,
  });
  const [draft, setDraft] = useState<Policy | null>(null);
  const [confirm, setConfirm] = useState<"save" | "delete" | null>(null);
  const save = useMutation({
    mutationFn: (body: Policy) =>
      unwrap(http.PUT("/v1/workspace/mcp/history/policy", { body })),
    onSuccess: (fresh) => {
      qc.setQueryData(policyKey, fresh);
      setDraft(null);
      setConfirm(null);
      qc.invalidateQueries({ queryKey: ["workspace-mcp-integrations"] });
      qc.removeQueries({ queryKey: ["mcp-history-content"] });
    },
  });
  const clear = useMutation({
    mutationFn: () => unwrap(http.DELETE("/v1/workspace/mcp/history")),
    onSuccess: () => {
      setConfirm(null);
      setDraft(null);
      qc.invalidateQueries({ queryKey: policyKey });
      qc.removeQueries({ queryKey: ["mcp-history-content"] });
    },
  });
  if (settings.isPending)
    return (
      <p className="text-xs text-muted-foreground">
        Loading MCP history settings…
      </p>
    );
  if (settings.error || !settings.data)
    return (
      <div role="alert" className="rounded-lg border border-border p-4 text-xs">
        MCP history settings are available to workspace owners and admins.{" "}
        <Button
          type="button"
          size="sm"
          variant="ghost"
          onClick={() => void settings.refetch()}
        >
          Retry
        </Button>
      </div>
    );
  const stored = settings.data.policy;
  const value = draft ?? stored;
  const enabled = value.mode === "sanitized";
  const busy = save.isPending || clear.isPending;
  const capability =
    settings.data.storage_allowed &&
    settings.data.encryption_available &&
    settings.data.tracing_available;
  const dirty = JSON.stringify(value) !== JSON.stringify(stored);
  const change = (patch: Partial<Policy>) => {
    setDraft({ ...value, ...patch });
    save.reset();
  };
  const invalid =
    enabled &&
    (!capability || (!value.capture_all && !value.connection_ids.length));
  const removesHistory =
    stored.mode === "sanitized" &&
    (value.mode === "metadata_only" ||
      value.retention_days < stored.retention_days ||
      (stored.capture_all && !value.capture_all) ||
      (!value.capture_all &&
        stored.connection_ids.some(
          (id) => !value.connection_ids.includes(id),
        )));
  return (
    <section
      id="mcp-history"
      aria-labelledby="mcp-history-title"
      className="space-y-4 rounded-xl border border-border bg-card p-5"
    >
      <div className="flex items-start gap-3">
        <History className="mt-0.5 size-5 shrink-0 text-muted-foreground" />
        <div>
          <h2
            id="mcp-history-title"
            className="text-sm font-bold tracking-tight"
          >
            MCP tool call history
          </h2>
          <p className="mt-1 text-xs leading-relaxed text-muted-foreground">
            Choose whether authorized reviewers can inspect sanitized tool
            arguments and responses in traces.
          </p>
        </div>
      </div>
      <fieldset disabled={busy} className="space-y-3">
        <legend className="sr-only">Capture mode</legend>
        <label className="flex cursor-pointer items-start gap-2 rounded-lg border border-border p-3 text-xs">
          <input
            type="radio"
            name={modeName}
            checked={!enabled}
            onChange={() => change({ mode: "metadata_only" })}
          />
          <span>
            <strong className="block font-medium">Metadata only</strong>
            <span className="mt-1 block text-muted-foreground">
              Record timing, tool outcomes and security decisions. Retain no
              payload content.
            </span>
          </span>
        </label>
        <label className="flex cursor-pointer items-start gap-2 rounded-lg border border-border p-3 text-xs">
          <input
            type="radio"
            name={modeName}
            checked={enabled}
            disabled={!capability}
            onChange={() => change({ mode: "sanitized" })}
          />
          <span>
            <strong className="block font-medium">Sanitized history</strong>
            <span className="mt-1 block text-muted-foreground">
              Store encrypted, redacted snapshots of inspected arguments and
              text responses. Raw capture is not available.
            </span>
          </span>
        </label>
        {!settings.data.storage_allowed && (
          <p className="text-xs text-muted-foreground">
            Workspace “Do not store conversation content” takes precedence.{" "}
            <Link
              to="/workspace"
              search={{ view: "settings", section: "privacy" }}
              className="underline underline-offset-4"
            >
              Review Privacy & data
            </Link>{" "}
            before enabling history.
          </p>
        )}
        {(!settings.data.encryption_available ||
          !settings.data.tracing_available) && (
          <p className="text-xs text-muted-foreground">
            Sanitized history needs encrypted storage and trace recording
            configured on this deployment.
          </p>
        )}
        {enabled && (
          <>
            <label className="flex flex-wrap items-center gap-3 text-xs">
              Delete captured content after{" "}
              <select
                aria-label="MCP history retention"
                className="cursor-pointer rounded-md border border-input bg-background px-3 py-2"
                value={value.retention_days}
                onChange={(e) =>
                  change({ retention_days: Number(e.target.value) })
                }
              >
                {[...new Set([1, 7, 14, 30, 60, 90, value.retention_days])]
                  .sort((a, b) => a - b)
                  .map((days) => (
                    <option key={days} value={days}>
                      {days} {days === 1 ? "day" : "days"}
                    </option>
                  ))}
              </select>
            </label>
            <p className="text-[11px] leading-relaxed text-muted-foreground">
              The workspace retention limit can shorten this period. Expired
              content becomes unreadable immediately and is deleted by the
              background sweep.
            </p>
            <fieldset className="space-y-2 rounded-lg border border-border p-3">
              <legend className="px-1 text-xs font-medium">
                Which integrations?
              </legend>
              <label className="flex items-start gap-2 text-xs">
                <input
                  type="checkbox"
                  checked={value.capture_all}
                  onChange={(e) => change({ capture_all: e.target.checked })}
                />
                <span>
                  All integrations, including employees’ personal connections
                  and future integrations
                </span>
              </label>
              {!value.capture_all && (
                <div className="max-h-48 space-y-2 overflow-y-auto pt-2">
                  {connections.isPending ? (
                    <p className="text-xs text-muted-foreground">
                      Loading integrations…
                    </p>
                  ) : connections.error ? (
                    <p role="alert" className="text-xs text-destructive">
                      Integrations could not be loaded.
                    </p>
                  ) : (
                    connections.data?.connections.map((c) => (
                      <label
                        key={c.id}
                        className="flex items-center gap-2 text-xs"
                      >
                        <input
                          type="checkbox"
                          checked={value.connection_ids.includes(c.id)}
                          onChange={(e) =>
                            change({
                              connection_ids: e.target.checked
                                ? [...value.connection_ids, c.id]
                                : value.connection_ids.filter(
                                    (id) => id !== c.id,
                                  ),
                            })
                          }
                        />
                        {c.name}
                        <span className="text-muted-foreground">
                          {c.scope === "workspace" ? "Shared" : "Personal"}
                        </span>
                      </label>
                    ))
                  )}
                </div>
              )}
              {!value.capture_all && !value.connection_ids.length && (
                <p className="text-[11px] text-muted-foreground">
                  Select at least one integration to enable capture.
                </p>
              )}
            </fieldset>
            <SharingPicker
              purpose="history"
              disabled={busy}
              value={{
                restricted: true,
                user_ids: value.viewer_user_ids,
                group_ids: value.viewer_group_ids,
              }}
              onChange={(sharing) =>
                change({
                  viewer_user_ids: sharing.user_ids,
                  viewer_group_ids: sharing.group_ids,
                })
              }
            />
            <div className="flex items-start gap-2 text-[11px] leading-relaxed text-muted-foreground">
              <LockKeyhole className="mt-0.5 size-3.5 shrink-0" />
              <p>
                Temporary chats and workspace no-storage rules always disable
                capture. Blocked payloads, binary resources and oversized
                content are omitted. Sanitization removes recognized PII and
                credentials; it is not a guarantee of anonymity.
              </p>
            </div>
          </>
        )}
      </fieldset>
      <p className="text-[11px] leading-relaxed text-muted-foreground">
        Capture applies to future calls. Switching to metadata only deletes
        retained payloads. Removing integrations purges their captured content.
        Shorter limits shorten existing expiry dates; longer limits do not
        extend old captures.
      </p>
      {(save.error || clear.error) && (
        <p role="alert" className="text-xs text-destructive">
          {(save.error ?? clear.error)?.message}
        </p>
      )}
      <div className="flex flex-wrap items-center gap-2">
        <Button
          type="button"
          size="sm"
          disabled={!dirty || busy || invalid}
          onClick={() =>
            removesHistory ? setConfirm("save") : save.mutate(value)
          }
        >
          {save.isPending ? "Saving…" : "Save history settings"}
        </Button>
        <Button
          type="button"
          size="sm"
          variant="ghost"
          disabled={busy}
          onClick={() => {
            setDraft(null);
            void settings.refetch();
          }}
        >
          Reload settings
        </Button>
        <Button
          type="button"
          size="sm"
          variant="outline"
          className="sm:ml-auto"
          disabled={busy}
          onClick={() => setConfirm("delete")}
        >
          <Trash2 className="size-3.5" />
          Delete retained content
        </Button>
      </div>
      {save.isSuccess && !dirty && (
        <p role="status" className="text-xs text-success">
          History settings saved.
        </p>
      )}
      <Dialog
        open={confirm !== null}
        onOpenChange={(open) => {
          if (!busy && !open) setConfirm(null);
        }}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>
              {confirm === "delete"
                ? "Delete retained MCP content?"
                : "Apply history changes?"}
            </DialogTitle>
            <DialogDescription>
              {confirm === "delete"
                ? "This permanently deletes all retained MCP payloads in this workspace. Trace timing and security metadata remain. In-flight calls cannot recreate this history; later calls follow your capture policy."
                : "This change can delete captured payloads for disabled integrations or shorten their expiry. Deleted content cannot be recovered. Trace timing and security metadata remain."}
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button
              type="button"
              variant="outline"
              disabled={busy}
              onClick={() => setConfirm(null)}
            >
              Cancel
            </Button>
            <Button
              type="button"
              disabled={busy}
              onClick={() =>
                confirm === "delete" ? clear.mutate() : save.mutate(value)
              }
            >
              {busy
                ? "Applying…"
                : confirm === "delete"
                  ? "Delete retained content"
                  : "Apply changes"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </section>
  );
}
