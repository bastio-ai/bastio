import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Plug, ShieldCheck, Loader2 } from "lucide-react";
import { http, unwrap } from "@/api/typed";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
} from "@/components/ui/dialog";
import { workspaceApi, type Message, type ToolProposal } from "./types";

export function AssistantToolsPicker({
  enabled,
  onEnabled,
  selected,
  onSelected,
  attached,
  disabled,
}: {
  enabled: boolean;
  onEnabled: (v: boolean) => void;
  selected: string[];
  onSelected: (v: string[]) => void;
  attached: string[];
  disabled?: boolean;
}) {
  const [open, setOpen] = useState(false);
  const connections = useQuery({
    queryKey: ["workspace-mcp-integrations"],
    queryFn: () => unwrap(http.GET("/v1/workspace/mcp")),
    staleTime: 0,
    gcTime: 0,
  });
  const available = (connections.data?.connections ?? []).filter(
    (c) => !(c.scope === "personal" && !connections.data?.allow_personal),
  );
  const availableAttached = attached.filter((id) =>
    available.some((c) => c.id === id),
  );
  const count = new Set([...selected, ...availableAttached]).size;
  return (
    <>
      <Button
        type="button"
        variant={enabled ? "outline" : "ghost"}
        size="sm"
        disabled={disabled}
        onClick={() => setOpen(true)}
      >
        <Plug className="size-4" />
        Assistant tools{enabled ? ` · ${count}` : ""}
      </Button>
      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Tools in this conversation</DialogTitle>
            <DialogDescription>
              Your assistant can suggest a tool request. Review its protected
              arguments and choose whether to run it. Nothing runs
              automatically.
            </DialogDescription>
          </DialogHeader>
          <label className="flex cursor-pointer items-center gap-3 rounded-lg border border-border p-3 text-sm">
            <input
              type="checkbox"
              checked={enabled}
              onChange={(e) => onEnabled(e.target.checked)}
              disabled={count === 0 && !enabled}
            />
            Allow the assistant to suggest tool calls
          </label>
          <div className="space-y-2">
            {available.map((c) => {
              const fixed = availableAttached.includes(c.id);
              return (
                <label
                  key={c.id}
                  className="flex cursor-pointer items-start gap-3 rounded-md border border-border p-3 text-xs"
                >
                  <input
                    className="mt-0.5"
                    type="checkbox"
                    checked={fixed || selected.includes(c.id)}
                    disabled={fixed}
                    onChange={(e) =>
                      onSelected(
                        e.target.checked
                          ? [...selected, c.id]
                          : selected.filter((id) => id !== c.id),
                      )
                    }
                  />
                  <span>
                    <span className="font-medium">{c.name}</span>
                    <span className="mt-1 block text-muted-foreground">
                      {fixed
                        ? "Assigned to this assistant"
                        : c.scope === "workspace"
                          ? "Shared with you"
                          : "Your personal connection"}
                      {c.auth_type === "oauth" && !c.oauth_connected
                        ? " · Account needs connecting"
                        : ""}
                    </span>
                  </span>
                </label>
              );
            })}
          </div>
          {connections.isPending ? (
            <p className="text-xs text-muted-foreground">
              Loading permitted integrations…
            </p>
          ) : null}
          {connections.error ? (
            <p role="alert" className="text-xs text-destructive">
              Could not load integrations. Open Integrations to reconnect.
            </p>
          ) : null}
          {!connections.isPending && available.length === 0 ? (
            <p className="text-xs text-muted-foreground">
              No integrations are available. Open Integrations to add one, or
              ask your administrator to share a tool.
            </p>
          ) : null}
          <p className="text-xs leading-relaxed text-muted-foreground">
            Access follows both assistant and integration sharing. Tool
            descriptions, arguments and results pass through Bastio's security
            controls. Supported providers: OpenAI, Claude, Gemini, OpenRouter,
            Grok, DeepSeek and Groq; individual models must support function
            calls.
          </p>
          <Button
            onClick={() => {
              if (count === 0) onEnabled(false);
              setOpen(false);
            }}
          >
            Done
          </Button>
        </DialogContent>
      </Dialog>
    </>
  );
}

export function ToolApprovalCard({
  proposal,
  conversationID,
  onMessage,
  onBusy,
}: {
  proposal: ToolProposal;
  conversationID: string;
  onMessage: (m: Message) => void;
  onBusy: (busy: boolean) => void;
}) {
  const qc = useQueryClient();
  const [busy, setBusy] = useState(false);
  const [outcome, setOutcome] = useState<{
    status: string;
    error?: string;
    result?: string;
  } | null>(null);
  const state = useQuery({
    queryKey: ["workspace-tool-approval", conversationID, proposal.id],
    queryFn: () => workspaceApi.toolApprovalStatus(conversationID, proposal.id),
    refetchInterval: busy ? 1000 : 5000,
    gcTime: 0,
  });
  const status = outcome?.status ?? state.data?.status ?? "loading";
  const expired = Date.now() >= Date.parse(proposal.expires_at);
  const pending = status === "pending" && !expired;
  const act = async (action: "approve" | "decline") => {
    setBusy(true);
    onBusy(true);
    setOutcome(null);
    try {
      const result = await workspaceApi.toolApprovalAction(
        conversationID,
        proposal.id,
        { action, token: proposal.token },
      );
      setOutcome(result);
      if (result.message) onMessage(result.message);
      await qc.invalidateQueries({
        queryKey: ["workspace-tool-approval", conversationID, proposal.id],
      });
    } catch (err) {
      setOutcome({ status: "unavailable", error: (err as Error).message });
      await qc.invalidateQueries({
        queryKey: ["workspace-tool-approval", conversationID, proposal.id],
      });
    } finally {
      setBusy(false);
      onBusy(false);
    }
  };
  return (
    <section
      aria-label="Proposed tool request"
      className="mx-auto my-3 max-w-2xl space-y-3 rounded-xl border border-border bg-card p-4 text-xs"
    >
      <div className="flex items-center gap-2 font-medium">
        <ShieldCheck className="size-4 text-primary" />
        Review tool request
        <span className="ml-auto rounded-md bg-muted px-2 py-1 font-normal text-muted-foreground">
          {expired && status === "pending"
            ? "expired"
            : busy
              ? "working"
              : status}
        </span>
      </div>
      <p>
        <span className="font-medium">{proposal.connection_name}</span> ·{" "}
        <code>{proposal.tool_name}</code>
      </p>
      <p className="leading-relaxed text-muted-foreground">
        These are the arguments that will be sent after workspace security checks. This action may
        change data in the connected service.
      </p>
      <pre className="max-h-64 overflow-auto whitespace-pre-wrap break-all rounded-md bg-muted/40 p-3 font-mono text-[11px]">
        {JSON.stringify(proposal.arguments, null, 2)}
      </pre>
      {proposal.categories.length > 0 ? (
        <p className="text-muted-foreground">
          Security findings: {proposal.categories.join(", ")}
        </p>
      ) : null}
      {pending ? (
        <div className="flex flex-wrap items-center gap-2">
          <Button
            size="sm"
            disabled={busy || state.isError}
            onClick={() => act("approve")}
          >
            {busy ? <Loader2 className="size-3 animate-spin" /> : null}Run tool
            & continue
          </Button>
          <Button
            size="sm"
            variant="outline"
            disabled={busy}
            onClick={() => act("decline")}
          >
            Decline
          </Button>
          <span className="text-muted-foreground">
            Expires{" "}
            {new Date(proposal.expires_at).toLocaleTimeString([], {
              hour: "2-digit",
              minute: "2-digit",
            })}
          </span>
        </div>
      ) : null}
      {status === "running" ? (
        <p className="text-muted-foreground">
          The approved request is running. It cannot be submitted twice.
        </p>
      ) : null}
      {status === "completed" && !outcome?.error ? (
        <p className="text-muted-foreground">
          Tool completed. The assistant's continuation appears below.
        </p>
      ) : null}
      {outcome?.error ? (
        <p role="alert" className="leading-relaxed text-destructive">
          {outcome.error}
        </p>
      ) : null}
      {state.isError ? (
        <p role="alert" className="text-destructive">
          Approval status is unavailable. Refresh before running this request.
        </p>
      ) : null}
      {outcome?.result ? (
        <pre className="max-h-64 overflow-auto whitespace-pre-wrap break-all rounded-md bg-muted/40 p-3">
          {outcome.result}
        </pre>
      ) : null}
    </section>
  );
}
