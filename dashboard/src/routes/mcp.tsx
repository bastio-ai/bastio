import { useState } from "react";
import { Link } from "@tanstack/react-router";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Check,
  CheckCircle2,
  Globe,
  KeyRound,
  Plus,
  RefreshCw,
  Search,
  Server,
  Shield,
  Terminal,
  XCircle,
} from "lucide-react";
import {
  mcpApi,
  mcpKeys,
  type McpApproval,
  type McpPolicy,
  type McpRemoteTool,
  type McpServer,
} from "@/api/mcp";
import {
  AdminPageHeader,
  AdminSummaryStrip,
  AdminUnderlineNav,
  FieldLabel,
} from "@/components/admin/admin-primitives";
import {
  ConnectMcpDialog,
  McpEnrollment,
} from "@/components/connect-mcp-dialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { EmptyState } from "@/components/card";
import { cn } from "@/lib/utils";

const tools = (value: string) => [
  ...new Set(
    value
      .split(/[\n,]/)
      .map((s) => s.trim())
      .filter(Boolean),
  ),
];

function status(s: McpServer) {
  if (s.revoked_at) return "Revoked";
  if (!s.last_seen_at) return "Awaiting connection";
  return Date.now() - Date.parse(s.last_seen_at) < 30_000
    ? "Connected"
    : "Offline";
}

function toggleToolInCsv(current: string, toolName: string): string {
  const list = tools(current);
  const next = list.includes(toolName)
    ? list.filter((x) => x !== toolName)
    : [...list, toolName];
  return next.join(", ");
}

function ServerControls({ server }: { server: McpServer }) {
  const qc = useQueryClient();
  const [open, setOpen] = useState(false);
  const [confirmRevokeOpen, setConfirmRevokeOpen] = useState(false);
  const [confirmRotateOpen, setConfirmRotateOpen] = useState(false);
  const [allowed, setAllowed] = useState("");
  const [blocked, setBlocked] = useState("");
  const [approvalRequired, setApprovalRequired] = useState("");
  const [webhookUrl, setWebhookUrl] = useState("");
  const [webhookFormat, setWebhookFormat] = useState<
    "slack" | "teams" | "raw_json"
  >("slack");
  const [mode, setMode] = useState<"stdio" | "remote">(server.mode);
  const [upstreamUrl, setUpstreamUrl] = useState(server.upstream_url ?? "");
  const [credential, setCredential] = useState("");
  const [discovered, setDiscovered] = useState<McpRemoteTool[] | null>(null);

  const changed = () => {
    void qc.invalidateQueries({ queryKey: mcpKeys });
  };

  const save = useMutation({
    mutationFn: async (p: McpPolicy) => {
      if (
        mode !== server.mode ||
        upstreamUrl.trim() !== (server.upstream_url ?? "") ||
        credential.trim() !== ""
      ) {
        await mcpApi.connection(server.id, {
          mode,
          upstream_url: mode === "remote" ? upstreamUrl.trim() : "",
          auth_type:
            mode === "remote"
              ? credential.trim() || server.has_credential
                ? "bearer"
                : "none"
              : "none",
          credential: credential.trim() ? credential.trim() : undefined,
        });
      }
      return mcpApi.policy(server.id, p);
    },
    onSuccess: () => {
      changed();
      setOpen(false);
    },
  });

  const discover = useMutation({
    mutationFn: () => mcpApi.discoverTools(server.id),
    onSuccess: (data) => {
      setDiscovered(data);
      changed();
    },
  });

  const rotate = useMutation({
    mutationFn: () => mcpApi.rotate(server.id),
    onSuccess: () => {
      setConfirmRotateOpen(false);
      changed();
    },
  });

  const revoke = useMutation({
    mutationFn: () => mcpApi.revoke(server.id),
    onSuccess: () => {
      setConfirmRevokeOpen(false);
      changed();
    },
  });

  if (server.revoked_at) return null;

  const idPrefix = `mcp-server-${server.id}`;

  return (
    <>
      <div className="flex flex-wrap gap-2">
        <Button
          type="button"
          size="sm"
          variant="outline"
          aria-label={`Tool policy, gateway & credentials for ${server.name}`}
          onClick={() => {
            setAllowed(server.policy.allowed_tools.join(", "));
            setBlocked(server.policy.blocked_tools.join(", "));
            setApprovalRequired(
              (server.policy.approval_required_tools ?? []).join(", "),
            );
            setWebhookUrl(server.policy.webhook_url ?? "");
            setWebhookFormat(server.policy.webhook_format ?? "slack");
            setMode(server.mode);
            setUpstreamUrl(server.upstream_url ?? "");
            setCredential("");
            setDiscovered(null);
            save.reset();
            rotate.reset();
            discover.reset();
            setOpen(true);
          }}
        >
          Tool policy, gateway &amp; credentials
        </Button>
        <Button
          type="button"
          size="sm"
          variant="ghost"
          aria-label={`Revoke enrollment for ${server.name}`}
          className="text-muted-foreground hover:text-destructive"
          disabled={revoke.isPending}
          onClick={() => {
            revoke.reset();
            setConfirmRevokeOpen(true);
          }}
        >
          Revoke enrollment
        </Button>
      </div>
      {revoke.error ? (
        <p role="alert" className="text-xs text-destructive">
          {revoke.error.message}
        </p>
      ) : null}

      <Dialog open={confirmRevokeOpen} onOpenChange={setConfirmRevokeOpen}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>Revoke enrollment?</DialogTitle>
            <DialogDescription>
              <strong>{server.name}</strong> will reject calls immediately on
              the hosted gateway and after policy expiry on local wrappers.
            </DialogDescription>
          </DialogHeader>
          {revoke.error ? (
            <p role="alert" className="text-xs text-destructive">
              {revoke.error.message}
            </p>
          ) : null}
          <DialogFooter>
            <Button
              type="button"
              variant="outline"
              onClick={() => setConfirmRevokeOpen(false)}
            >
              Cancel
            </Button>
            <Button
              type="button"
              variant="destructive"
              disabled={revoke.isPending}
              onClick={() => revoke.mutate()}
            >
              {revoke.isPending ? "Revoking…" : "Revoke enrollment"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog open={confirmRotateOpen} onOpenChange={setConfirmRotateOpen}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>Replace enrollment token?</DialogTitle>
            <DialogDescription>
              Existing clients using the current token for{" "}
              <strong>{server.name}</strong> must be updated with the new token
              to reconnect.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button
              type="button"
              variant="outline"
              onClick={() => setConfirmRotateOpen(false)}
            >
              Cancel
            </Button>
            <Button
              type="button"
              disabled={rotate.isPending}
              onClick={() => rotate.mutate()}
            >
              {rotate.isPending ? "Replacing…" : "Replace token"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent className="max-h-[90vh] overflow-y-auto sm:max-w-2xl">
          <DialogHeader>
            <DialogTitle>{server.name}</DialogTitle>
          </DialogHeader>
          {rotate.data ? (
            <McpEnrollment
              token={rotate.data.enrollment_token}
              name={server.name}
              client={server.client}
              mode={mode}
            />
          ) : (
            <div className="space-y-4">
              <div className="space-y-3 border-y border-border-subtle py-3.5">
                <div className="flex flex-wrap items-center justify-between gap-2">
                  <span className="text-xs font-medium text-foreground">
                    Gateway mode &amp; upstream credential vault
                  </span>
                  <div className="flex gap-1.5">
                    <Button
                      type="button"
                      size="sm"
                      variant={mode === "remote" ? "secondary" : "ghost"}
                      onClick={() => setMode("remote")}
                    >
                      <Globe aria-hidden="true" />
                      Remote HTTP Gateway
                    </Button>
                    <Button
                      type="button"
                      size="sm"
                      variant={mode === "stdio" ? "secondary" : "ghost"}
                      onClick={() => setMode("stdio")}
                    >
                      <Terminal aria-hidden="true" />
                      Local stdio fallback
                    </Button>
                  </div>
                </div>
                {mode === "remote" ? (
                  <div className="space-y-3">
                    <div>
                      <FieldLabel htmlFor={`${idPrefix}-upstream`}>
                        Upstream HTTPS MCP endpoint
                      </FieldLabel>
                      <Input
                        id={`${idPrefix}-upstream`}
                        type="url"
                        autoCorrect="off"
                        autoCapitalize="none"
                        spellCheck={false}
                        value={upstreamUrl}
                        onChange={(e) => setUpstreamUrl(e.target.value)}
                        placeholder="https://mcp.yourcompany.com/mcp"
                        className="font-mono text-xs"
                      />
                    </div>
                    <div>
                      <FieldLabel htmlFor={`${idPrefix}-credential`}>
                        {server.has_credential
                          ? "Rotate vaulted upstream Bearer credential (leave blank to keep current)"
                          : "Vault upstream Bearer credential (encrypted at rest)"}
                      </FieldLabel>
                      <Input
                        id={`${idPrefix}-credential`}
                        type="password"
                        autoComplete="off"
                        autoCorrect="off"
                        autoCapitalize="none"
                        spellCheck={false}
                        value={credential}
                        onChange={(e) => setCredential(e.target.value)}
                        placeholder={
                          server.has_credential
                            ? "•••••••••••••••• (vaulted in Bastio)"
                            : "ghp_..., lin_api_..., or service token"
                        }
                        className="font-mono text-xs"
                      />
                    </div>
                    {server.mode === "remote" && server.upstream_url ? (
                      <div className="space-y-2">
                        <Button
                          type="button"
                          size="sm"
                          variant="outline"
                          disabled={discover.isPending}
                          onClick={() => discover.mutate()}
                        >
                          <Search aria-hidden="true" />
                          {discover.isPending
                            ? "Discovering upstream tools…"
                            : "Discover upstream tools"}
                        </Button>
                        {discover.error ? (
                          <p role="alert" className="text-xs text-destructive">
                            {discover.error.message}
                          </p>
                        ) : null}
                        {discovered ? (
                          <div className="max-h-48 space-y-2 overflow-y-auto border-y border-border-subtle py-2.5">
                            <p className="text-[11px] text-muted-foreground">
                              Discovered {discovered.length} upstream tools
                              (descriptions scanned by Bastio Security). Click a
                              policy action to assign:
                            </p>
                            {discovered.map((t) => (
                              <div
                                key={t.name}
                                className="flex flex-wrap items-center justify-between gap-2 border-b border-border-subtle pb-1.5 last:border-0 last:pb-0"
                              >
                                <div className="min-w-0 flex-1">
                                  <p className="font-mono text-xs">{t.name}</p>
                                  {t.description ? (
                                    <p className="truncate text-[11px] text-muted-foreground">
                                      {t.description}
                                    </p>
                                  ) : null}
                                </div>
                                <div className="flex gap-1">
                                  <Button
                                    type="button"
                                    size="sm"
                                    variant={
                                      tools(allowed).includes(t.name)
                                        ? "secondary"
                                        : "ghost"
                                    }
                                    onClick={() =>
                                      setAllowed(
                                        toggleToolInCsv(allowed, t.name),
                                      )
                                    }
                                  >
                                    Allow
                                  </Button>
                                  <Button
                                    type="button"
                                    size="sm"
                                    variant={
                                      tools(approvalRequired).includes(t.name)
                                        ? "secondary"
                                        : "ghost"
                                    }
                                    onClick={() =>
                                      setApprovalRequired(
                                        toggleToolInCsv(
                                          approvalRequired,
                                          t.name,
                                        ),
                                      )
                                    }
                                  >
                                    Require approval
                                  </Button>
                                  <Button
                                    type="button"
                                    size="sm"
                                    variant={
                                      tools(blocked).includes(t.name)
                                        ? "destructive"
                                        : "ghost"
                                    }
                                    onClick={() =>
                                      setBlocked(
                                        toggleToolInCsv(blocked, t.name),
                                      )
                                    }
                                  >
                                    Block
                                  </Button>
                                </div>
                              </div>
                            ))}
                          </div>
                        ) : null}
                      </div>
                    ) : null}
                  </div>
                ) : (
                  <p className="text-xs text-muted-foreground">
                    Local stdio wrapper mode runs{" "}
                    <code className="font-mono text-foreground">
                      bastio mcp-proxy
                    </code>{" "}
                    on the developer machine as a fallback for local-only tools.
                  </p>
                )}
              </div>

              <div>
                <FieldLabel htmlFor={`${idPrefix}-allowed`} optional>
                  Allowed tools (comma-separated)
                </FieldLabel>
                <Input
                  id={`${idPrefix}-allowed`}
                  autoCorrect="off"
                  autoCapitalize="none"
                  spellCheck={false}
                  value={allowed}
                  onChange={(e) => setAllowed(e.target.value)}
                  placeholder="Empty means all non-blocked tools are inspected"
                />
              </div>
              <div>
                <FieldLabel htmlFor={`${idPrefix}-approval`} optional>
                  Human approval required tools (comma-separated)
                </FieldLabel>
                <Input
                  id={`${idPrefix}-approval`}
                  autoCorrect="off"
                  autoCapitalize="none"
                  spellCheck={false}
                  value={approvalRequired}
                  onChange={(e) => setApprovalRequired(e.target.value)}
                  placeholder="deploy_service, merge_pull_request, execute_mutation"
                />
              </div>
              <div>
                <FieldLabel htmlFor={`${idPrefix}-blocked`} optional>
                  Blocked tools (comma-separated)
                </FieldLabel>
                <Input
                  id={`${idPrefix}-blocked`}
                  autoCorrect="off"
                  autoCapitalize="none"
                  spellCheck={false}
                  value={blocked}
                  onChange={(e) => setBlocked(e.target.value)}
                  placeholder="execute_sql, delete_file"
                />
              </div>

              <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
                <div className="sm:col-span-2">
                  <FieldLabel htmlFor={`${idPrefix}-webhook-url`} optional>
                    Approval notification webhook URL
                  </FieldLabel>
                  <Input
                    id={`${idPrefix}-webhook-url`}
                    type="url"
                    autoCorrect="off"
                    autoCapitalize="none"
                    spellCheck={false}
                    value={webhookUrl}
                    onChange={(e) => setWebhookUrl(e.target.value)}
                    placeholder="https://hooks.slack.com/services/..."
                    className="font-mono text-xs"
                  />
                </div>
                <div>
                  <FieldLabel htmlFor={`${idPrefix}-webhook-format`}>
                    Webhook format
                  </FieldLabel>
                  <select
                    id={`${idPrefix}-webhook-format`}
                    className="h-9 w-full rounded-md border border-input bg-background px-3 text-xs outline-none focus-visible:ring-2 focus-visible:ring-ring"
                    value={webhookFormat}
                    onChange={(e) =>
                      setWebhookFormat(
                        e.target.value as "slack" | "teams" | "raw_json",
                      )
                    }
                  >
                    <option value="slack">Slack</option>
                    <option value="teams">Microsoft Teams</option>
                    <option value="raw_json">Raw JSON</option>
                  </select>
                </div>
              </div>

              <p className="text-xs text-muted-foreground">
                Names match exactly; blocks take precedence over approvals and
                allowlists. Tools requiring human approval hold the MCP call and
                notify operators via Slack/Teams until approved or denied.
              </p>
              {save.error || rotate.error ? (
                <p role="alert" className="text-xs text-destructive">
                  {save.error?.message ?? rotate.error?.message}
                </p>
              ) : null}
              <div className="flex flex-wrap gap-2">
                <Button
                  type="button"
                  disabled={
                    save.isPending ||
                    (mode === "remote" && !upstreamUrl.trim())
                  }
                  onClick={() =>
                    save.mutate({
                      allowed_tools: tools(allowed),
                      blocked_tools: tools(blocked),
                      approval_required_tools: tools(approvalRequired),
                      webhook_url: webhookUrl.trim() || undefined,
                      webhook_format: webhookUrl.trim()
                        ? webhookFormat
                        : undefined,
                    })
                  }
                >
                  {save.isPending ? "Saving…" : "Save policy & connection"}
                </Button>
                <Button
                  type="button"
                  variant="outline"
                  disabled={rotate.isPending}
                  onClick={() => setConfirmRotateOpen(true)}
                >
                  Replace enrollment token
                </Button>
              </div>
            </div>
          )}
        </DialogContent>
      </Dialog>
    </>
  );
}

export function McpPage() {
  const [tab, setTab] = useState<
    "events" | "approvals" | "servers" | "sandbox"
  >("events");
  const [connect, setConnect] = useState(false);
  const [filter, setFilter] = useState("");
  const [selected, setSelected] = useState("");
  const [approvalStatusFilter, setApprovalStatusFilter] = useState<
    "" | "pending" | "approved" | "denied" | "expired"
  >("");
  const [reasonByApproval, setReasonByApproval] = useState<
    Record<string, string>
  >({});
  const [tool, setTool] = useState("filesystem.read_file");
  const [args, setArgs] = useState(
    '{"path":"/path/to/your/project/README.md"}',
  );
  const [formError, setFormError] = useState("");
  const qc = useQueryClient();

  const servers = useQuery({
    queryKey: [...mcpKeys, "servers"],
    queryFn: mcpApi.servers,
    meta: { dashboardLive: true },
    refetchOnWindowFocus: false,
  });

  const events = useQuery({
    queryKey: [...mcpKeys, "events", selected],
    queryFn: () => mcpApi.events(selected || undefined),
    meta: { dashboardLive: true },
    refetchOnWindowFocus: false,
  });

  const approvals = useQuery({
    queryKey: [...mcpKeys, "approvals", selected, approvalStatusFilter],
    queryFn: () =>
      mcpApi.approvals(
        selected || undefined,
        approvalStatusFilter || undefined,
      ),
    meta: { dashboardLive: true },
    refetchOnWindowFocus: false,
  });

  const resolveApproval = useMutation({
    mutationFn: ({
      id,
      action,
      reason,
    }: {
      id: string;
      action: "approved" | "denied";
      reason?: string;
    }) => mcpApi.resolveApproval(id, { action, reason }),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: mcpKeys });
    },
  });

  const inspect = useMutation({ mutationFn: mcpApi.inspect });

  const list = servers.data ?? [];
  const policy = list.find((s) => s.id === selected)?.policy ?? {
    allowed_tools: [],
    blocked_tools: [],
    approval_required_tools: [],
  };
  const rows = (events.data ?? []).filter((e) =>
    `${e.tool_name} ${e.server_name} ${e.client} ${e.action} ${e.categories.join(" ")}`
      .toLowerCase()
      .includes(filter.toLowerCase()),
  );
  const approvalRows: McpApproval[] = approvals.data ?? [];
  const totalPendingApprovals = list.reduce(
    (n, s) => n + (s.pending_approvals ?? 0),
    0,
  );
  const remoteCount = list.filter(
    (s) => !s.revoked_at && s.mode === "remote",
  ).length;

  const error = servers.error ?? events.error ?? approvals.error;
  const activeServerCount = list.filter((s) => !s.revoked_at).length;
  const selector = (id: string) => (
    <div>
      <label htmlFor={id} className="sr-only">
        MCP server policy
      </label>
      <select
        id={id}
        className="h-8 rounded-md border border-input bg-background px-2.5 text-xs outline-none focus-visible:ring-2 focus-visible:ring-ring"
        value={selected}
        onChange={(e) => {
          setSelected(e.target.value);
          inspect.reset();
        }}
      >
        <option value="">
          {tab === "sandbox" ? "Security baseline" : "All servers"}
        </option>
        {list.map((s) => (
          <option value={s.id} key={s.id}>
            {s.name}
          </option>
        ))}
      </select>
    </div>
  );

  return (
    <div className="mx-auto max-w-[1180px] pb-10 pt-2 md:pt-4">
      <AdminPageHeader
        eyebrow="Security & guardrails"
        title="Agent & Tool Firewalls"
        description="Host zero-secret remote HTTP/SSE MCP gateways with vaulted credential injection, enforce human-in-the-loop approvals, or wrap local stdio servers. Telemetry excludes raw arguments, outputs, and tokens."
        bordered={false}
        className="mb-4"
        actions={
          <>
            <Button
              nativeButton={false}
              size="sm"
              variant="outline"
              render={<Link to="/connect" search={{ guide: "mcp" }} />}
            >
              Guided setup
            </Button>
            <Button
              type="button"
              size="sm"
              variant="outline"
              onClick={() => {
                void qc.invalidateQueries({ queryKey: mcpKeys });
              }}
            >
              <RefreshCw aria-hidden="true" />
              Refresh
            </Button>
            <Button type="button" size="sm" onClick={() => setConnect(true)}>
              <Plus aria-hidden="true" />
              Connect server
            </Button>
          </>
        }
      />
      <AdminSummaryStrip
        items={[
          {
            label: "Connected",
            value: servers.isPending
              ? "—"
              : list
                  .filter((s) => status(s) === "Connected")
                  .length.toLocaleString(),
            detail: "Heartbeat within 30 seconds",
            tone: "success",
          },
          {
            label: "Enrolled",
            value: servers.isPending
              ? "—"
              : activeServerCount.toLocaleString(),
            detail: `${remoteCount} remote gateway · ${Math.max(0, activeServerCount - remoteCount)} local stdio`,
          },
          {
            label: "Pending approvals",
            value: servers.isPending
              ? "—"
              : totalPendingApprovals.toLocaleString(),
            detail: "Awaiting operator decision",
            tone: totalPendingApprovals > 0 ? "warning" : "default",
          },
          {
            label: "Calls inspected",
            value: servers.isPending
              ? "—"
              : list.reduce((n, s) => n + s.calls, 0).toLocaleString(),
            detail: `${list.reduce((n, s) => n + s.blocked, 0).toLocaleString()} blocked or denied · 30d`,
          },
        ]}
      />
      <AdminUnderlineNav
        ariaLabel="MCP firewall sections"
        value={tab}
        onChange={setTab}
        items={[
          { id: "events", label: "Security events" },
          {
            id: "approvals",
            label: "Human approvals",
            count: totalPendingApprovals > 0 ? totalPendingApprovals : undefined,
          },
          {
            id: "servers",
            label: "Servers & policies",
            count: activeServerCount > 0 ? activeServerCount : undefined,
          },
          { id: "sandbox", label: "Inspect a tool call" },
        ]}
      />
      {error ? (
        <div
          role="alert"
          className="mb-5 rounded-md border border-warn-border bg-warn-bg px-4 py-3 text-xs text-destructive"
        >
          Unable to load MCP data: {error.message}
        </div>
      ) : null}
      {tab === "events" ? (
        <div className="space-y-4">
          <search className="flex flex-wrap items-center gap-3">
            <div className="relative w-full sm:w-72">
              <label htmlFor="mcp-events-filter" className="sr-only">
                Filter MCP events
              </label>
              <Search
                aria-hidden="true"
                className="pointer-events-none absolute left-2.5 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground"
              />
              <Input
                id="mcp-events-filter"
                type="search"
                autoCorrect="off"
                autoCapitalize="none"
                spellCheck={false}
                className="h-8 pl-8 text-xs"
                value={filter}
                onChange={(e) => setFilter(e.target.value)}
                placeholder="Filter tools, servers, outcomes…"
              />
            </div>
            {selector("mcp-events-server")}
          </search>
          <div className="border-y border-border-subtle">
            {events.isPending ? (
              <p className="py-12 text-center text-xs text-muted-foreground">
                Loading events…
              </p>
            ) : !rows.length ? (
              <EmptyState
                icon={<Shield className="size-5" />}
                title={
                  filter
                    ? "No matching events"
                    : "No tool events reported yet"
                }
                description="Connect a remote HTTP gateway or local stdio wrapper and run a tool. Its real inspection result will appear here."
              />
            ) : (
              <div
                className="overflow-x-auto"
                tabIndex={0}
                role="region"
                aria-label="MCP security events"
              >
                <table className="w-full min-w-[780px] text-left text-xs">
                  <thead className="border-b border-border-subtle text-[11px] text-muted-foreground">
                    <tr>
                      {[
                        "Time",
                        "Tool / server",
                        "Direction",
                        "Outcome",
                        "Categories",
                        "Duration",
                      ].map((t) => (
                        <th
                          scope="col"
                          className="sticky top-0 bg-background px-3 py-2.5 font-normal first:pl-0 last:pr-0"
                          key={t}
                        >
                          {t}
                        </th>
                      ))}
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-border-subtle">
                    {rows.map((e) => (
                      <tr
                        className="transition-colors hover:bg-surface-1/60"
                        key={e.id}
                      >
                        <td className="whitespace-nowrap py-3 pr-3 font-mono text-[11px] tabular-nums text-muted-foreground">
                          {e.occurred_at
                            ? new Date(e.occurred_at).toLocaleString()
                            : "—"}
                        </td>
                        <td className="px-3 py-3">
                          <p className="font-mono text-[12px] text-foreground">
                            {e.tool_name}
                          </p>
                          <p className="mt-0.5 text-[11px] text-muted-foreground">
                            {e.server_name} · {e.client}
                          </p>
                        </td>
                        <td className="px-3 py-3 text-muted-foreground">
                          {e.direction.replaceAll("_", " ")}
                        </td>
                        <td className="px-3 py-3">
                          <Badge
                            variant={
                              e.action === "blocked" || e.action === "denied"
                                ? "destructive"
                                : "outline"
                            }
                          >
                            {e.action.replaceAll("_", " ")}
                          </Badge>
                        </td>
                        <td className="px-3 py-3 text-muted-foreground">
                          {e.categories.join(", ") || "—"}
                        </td>
                        <td className="py-3 pl-3 font-mono tabular-nums text-muted-foreground">
                          {e.duration_ms.toFixed(2)} ms
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </div>
          <p className="text-xs text-muted-foreground">
            Up to 100 recent events. Request duration measures inspection;
            response duration includes the tool round trip. Reporting is
            bounded; dropped events and connection gaps appear per server.
          </p>
        </div>
      ) : tab === "approvals" ? (
        <div className="space-y-4">
          <search className="flex flex-wrap items-center gap-3">
            {selector("mcp-approvals-server")}
            <div>
              <label htmlFor="mcp-approval-status" className="sr-only">
                Approval status filter
              </label>
              <select
                id="mcp-approval-status"
                className="h-8 rounded-md border border-input bg-background px-2.5 text-xs outline-none focus-visible:ring-2 focus-visible:ring-ring"
                value={approvalStatusFilter}
                onChange={(e) =>
                  setApprovalStatusFilter(
                    e.target.value as
                      | ""
                      | "pending"
                      | "approved"
                      | "denied"
                      | "expired",
                  )
                }
              >
                <option value="">All statuses</option>
                <option value="pending">Pending decision</option>
                <option value="approved">Approved</option>
                <option value="denied">Denied</option>
                <option value="expired">Expired</option>
              </select>
            </div>
          </search>
          <div className="border-y border-border-subtle">
            {approvals.isPending ? (
              <p className="py-12 text-center text-xs text-muted-foreground">
                Loading approval requests…
              </p>
            ) : !approvalRows.length ? (
              <EmptyState
                icon={<CheckCircle2 className="size-5" />}
                title="No approval requests"
                description="Add high-risk tool names to a server's 'Human approval required tools' policy. When an agent invokes those tools, execution pauses here for 1-click approval."
              />
            ) : (
              <div
                className="overflow-x-auto"
                tabIndex={0}
                role="region"
                aria-label="MCP human-in-the-loop approvals"
              >
                <table className="w-full min-w-[820px] text-left text-xs">
                  <thead className="border-b border-border-subtle text-[11px] text-muted-foreground">
                    <tr>
                      {[
                        "Requested",
                        "Tool / server",
                        "Argument keys",
                        "Categories",
                        "Status",
                        "Decision",
                      ].map((t) => (
                        <th
                          scope="col"
                          className="sticky top-0 bg-background px-3 py-2.5 font-normal first:pl-0 last:pr-0"
                          key={t}
                        >
                          {t}
                        </th>
                      ))}
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-border-subtle">
                    {approvalRows.map((a) => (
                      <tr
                        className="transition-colors hover:bg-surface-1/60"
                        key={a.id}
                      >
                        <td className="whitespace-nowrap py-3 pr-3 font-mono text-[11px] tabular-nums">
                          <p className="text-foreground">
                            {new Date(a.requested_at).toLocaleTimeString()}
                          </p>
                          <p className="mt-0.5 text-muted-foreground">
                            Expires{" "}
                            {new Date(a.expires_at).toLocaleTimeString()}
                          </p>
                        </td>
                        <td className="px-3 py-3">
                          <p className="font-mono text-[12px] font-medium text-foreground">
                            {a.tool_name}
                          </p>
                          <p className="mt-0.5 text-[11px] text-muted-foreground">
                            {a.server_name} · {a.client}
                          </p>
                        </td>
                        <td className="px-3 py-3 font-mono text-[11px] text-muted-foreground">
                          {a.argument_keys.length
                            ? a.argument_keys.join(", ")
                            : "—"}
                        </td>
                        <td className="px-3 py-3 text-muted-foreground">
                          {a.categories.join(", ") || "human_approval"}
                        </td>
                        <td className="px-3 py-3">
                          <Badge
                            variant={
                              a.status === "denied"
                                ? "destructive"
                                : a.status === "approved"
                                  ? "secondary"
                                  : "outline"
                            }
                          >
                            {a.status}
                          </Badge>
                          {a.decided_by ? (
                            <p className="mt-1 text-[11px] text-muted-foreground">
                              by {a.decided_by}
                              {a.decision_reason
                                ? ` · "${a.decision_reason}"`
                                : ""}
                            </p>
                          ) : null}
                        </td>
                        <td className="py-3 pl-3">
                          {a.status === "pending" ? (
                            <div className="flex flex-wrap items-center gap-1.5">
                              <Input
                                aria-label={`Decision reason for ${a.tool_name}`}
                                placeholder="Optional reason…"
                                className="h-7 w-36 text-xs"
                                value={reasonByApproval[a.id] ?? ""}
                                onChange={(e) =>
                                  setReasonByApproval((prev) => ({
                                    ...prev,
                                    [a.id]: e.target.value,
                                  }))
                                }
                              />
                              <Button
                                type="button"
                                size="sm"
                                variant="secondary"
                                aria-label={`Approve ${a.tool_name}`}
                                disabled={resolveApproval.isPending}
                                onClick={() =>
                                  resolveApproval.mutate({
                                    id: a.id,
                                    action: "approved",
                                    reason: reasonByApproval[a.id],
                                  })
                                }
                              >
                                <Check aria-hidden="true" />
                                Approve
                              </Button>
                              <Button
                                type="button"
                                size="sm"
                                variant="destructive"
                                aria-label={`Deny ${a.tool_name}`}
                                disabled={resolveApproval.isPending}
                                onClick={() =>
                                  resolveApproval.mutate({
                                    id: a.id,
                                    action: "denied",
                                    reason: reasonByApproval[a.id],
                                  })
                                }
                              >
                                <XCircle aria-hidden="true" />
                                Deny
                              </Button>
                            </div>
                          ) : (
                            <span className="text-muted-foreground">
                              Resolved
                            </span>
                          )}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </div>
          {resolveApproval.error ? (
            <p role="alert" className="text-xs text-destructive">
              {resolveApproval.error.message}
            </p>
          ) : null}
          <p className="text-xs text-muted-foreground">
            Approval records store only top-level argument key names and threat
            categories—never raw argument values or credentials.
          </p>
        </div>
      ) : tab === "servers" ? (
        <div className="border-y border-border-subtle">
          {servers.isPending ? (
            <p className="py-12 text-center text-xs text-muted-foreground">
              Loading servers…
            </p>
          ) : !list.length ? (
            <EmptyState
              icon={<Server className="size-5" />}
              title="Connect your first MCP server"
              description="Enroll a remote HTTP gateway with vaulted credentials or wrap a local stdio server in Claude Desktop, Cursor, Cline, or your agent."
              action={
                <Button type="button" size="sm" onClick={() => setConnect(true)}>
                  Connect server
                </Button>
              }
            />
          ) : (
            <div className="divide-y divide-border-subtle">
              {list.map((s) => {
                const state = status(s);
                return (
                  <div key={s.id} className="space-y-2.5 py-4">
                    <div className="flex flex-wrap items-start justify-between gap-3">
                      <div className="min-w-0">
                        <div className="flex flex-wrap items-center gap-2.5">
                          <span
                            aria-hidden="true"
                            className={cn(
                              "size-1.5 shrink-0 rounded-full",
                              state === "Connected"
                                ? "bg-success"
                                : state === "Awaiting connection"
                                  ? "bg-warn"
                                  : "bg-muted-foreground/40",
                            )}
                          />
                          <h2 className="text-[13px] font-medium text-foreground">
                            {s.name}
                          </h2>
                          <span className="text-[11px] text-muted-foreground">
                            {s.mode === "remote"
                              ? "Remote HTTP gateway"
                              : "Local stdio fallback"}
                          </span>
                          {s.mode === "remote" && s.has_credential ? (
                            <span className="inline-flex items-center gap-1 rounded-sm border border-border-subtle bg-surface-1/60 px-1.5 py-0.5 text-[10px] text-muted-foreground">
                              <KeyRound
                                aria-hidden="true"
                                className="size-2.5"
                              />
                              Credential vaulted
                            </span>
                          ) : null}
                        </div>
                        <p className="mt-1 text-xs text-muted-foreground">
                          {s.client}
                          {s.mode === "remote" && s.upstream_url
                            ? ` · ${s.upstream_url}`
                            : ""}{" "}
                          ·{" "}
                          {s.last_seen_at
                            ? `Last seen ${new Date(s.last_seen_at).toLocaleString()}`
                            : "No heartbeat received"}
                        </p>
                      </div>
                      <span className="text-xs text-muted-foreground">
                        {state}
                      </span>
                    </div>
                    <p className="text-xs tabular-nums text-muted-foreground">
                      {s.policy.allowed_tools.length
                        ? `Allowed: ${s.policy.allowed_tools.join(", ")}`
                        : "All tools inspected"}{" "}
                      ·{" "}
                      {(s.policy.approval_required_tools ?? []).length
                        ? `Approval required: ${(s.policy.approval_required_tools ?? []).join(", ")}`
                        : "No human-approval tools"}{" "}
                      ·{" "}
                      {s.policy.blocked_tools.length
                        ? `Blocked: ${s.policy.blocked_tools.join(", ")}`
                        : "No explicit tool blocks"}{" "}
                      · {s.calls.toLocaleString()} reported calls ·{" "}
                      {s.blocked.toLocaleString()} blocked
                      {s.pending_approvals > 0
                        ? ` · ${s.pending_approvals.toLocaleString()} pending approval`
                        : ""}
                    </p>
                    {s.dropped_events > 0 ? (
                      <p className="text-xs text-destructive">
                        {s.dropped_events} events dropped by the client
                        reporting queue.
                      </p>
                    ) : null}
                    <ServerControls server={s} />
                  </div>
                );
              })}
            </div>
          )}
        </div>
      ) : (
        <section
          aria-label="Inspect without executing"
          className="border-y border-border-subtle py-5"
        >
          <div className="max-w-2xl space-y-4">
            <div>
              <h2 className="text-[13px] font-medium text-foreground">
                Inspect without executing
              </h2>
              <p className="mt-1 text-xs leading-relaxed text-muted-foreground">
                Uses the gateway and CLI&apos;s real inspection code. Input is
                processed for this request and not added to the event store.
              </p>
            </div>
            <form
              className="space-y-4"
              onSubmit={(e) => {
                e.preventDefault();
                setFormError("");
                try {
                  const value: unknown = JSON.parse(args);
                  if (
                    !value ||
                    typeof value !== "object" ||
                    Array.isArray(value)
                  )
                    throw new Error("Tool arguments must be a JSON object.");
                  inspect.mutate({
                    tool_name: tool,
                    arguments: value as Record<string, unknown>,
                    policy,
                  });
                } catch (e) {
                  setFormError(e instanceof Error ? e.message : "Invalid JSON");
                }
              }}
            >
              <div>
                <FieldLabel htmlFor="mcp-sandbox-server">
                  Policy baseline
                </FieldLabel>
                <select
                  id="mcp-sandbox-server"
                  className="h-9 w-full rounded-md border border-input bg-background px-3 text-xs outline-none focus-visible:ring-2 focus-visible:ring-ring"
                  value={selected}
                  onChange={(e) => {
                    setSelected(e.target.value);
                    inspect.reset();
                  }}
                >
                  <option value="">Security baseline</option>
                  {list.map((s) => (
                    <option value={s.id} key={s.id}>
                      {s.name}
                    </option>
                  ))}
                </select>
              </div>
              <div>
                <FieldLabel htmlFor="mcp-sandbox-tool">Tool name</FieldLabel>
                <Input
                  id="mcp-sandbox-tool"
                  required
                  autoCorrect="off"
                  autoCapitalize="none"
                  spellCheck={false}
                  value={tool}
                  onChange={(e) => {
                    setTool(e.target.value);
                    inspect.reset();
                  }}
                  className="font-mono text-xs"
                />
              </div>
              <div>
                <FieldLabel htmlFor="mcp-sandbox-args">
                  Arguments (JSON object)
                </FieldLabel>
                <textarea
                  id="mcp-sandbox-args"
                  autoCorrect="off"
                  autoCapitalize="none"
                  spellCheck={false}
                  aria-invalid={Boolean(formError)}
                  aria-describedby={
                    formError ? "mcp-sandbox-error" : undefined
                  }
                  className="min-h-36 w-full rounded-md border border-input bg-background p-3 font-mono text-xs outline-none focus-visible:ring-2 focus-visible:ring-ring"
                  value={args}
                  onChange={(e) => {
                    setArgs(e.target.value);
                    inspect.reset();
                  }}
                />
              </div>
              <Button type="submit" size="sm" disabled={inspect.isPending}>
                {inspect.isPending ? "Inspecting…" : "Inspect tool call"}
              </Button>
            </form>
            {formError || inspect.error ? (
              <p
                id="mcp-sandbox-error"
                role="alert"
                className="text-xs text-destructive"
              >
                {formError || inspect.error?.message}
              </p>
            ) : null}
            {inspect.data ? (
              <div className="space-y-2 border-t border-border-subtle pt-4">
                <div className="flex items-center gap-2.5">
                  <Badge variant="outline">
                    {inspect.data.action.replaceAll("_", " ")}
                  </Badge>
                  <span className="text-xs tabular-nums text-muted-foreground">
                    {inspect.data.categories.join(", ") ||
                      "No security findings"}{" "}
                    · {inspect.data.duration_ms.toFixed(2)} ms
                  </span>
                </div>
                {inspect.data.arguments ? (
                  <pre className="overflow-auto rounded-md border border-border-subtle bg-surface-1/60 p-3 font-mono text-xs">
                    {JSON.stringify(inspect.data.arguments, null, 2)}
                  </pre>
                ) : null}
              </div>
            ) : null}
          </div>
        </section>
      )}
      <ConnectMcpDialog open={connect} onOpenChange={setConnect} />
    </div>
  );
}
