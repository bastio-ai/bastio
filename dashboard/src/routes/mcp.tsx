import { useState } from "react";
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
  FieldLabel,
} from "@/components/admin/admin-primitives";
import {
  ConnectMcpDialog,
  McpEnrollment,
} from "@/components/connect-mcp-dialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { EmptyState } from "@/components/card";

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
    onSuccess: changed,
  });

  const revoke = useMutation({
    mutationFn: () => mcpApi.revoke(server.id),
    onSuccess: changed,
  });

  if (server.revoked_at) return null;

  return (
    <>
      <div className="flex flex-wrap gap-2">
        <Button
          size="sm"
          variant="outline"
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
          size="sm"
          variant="ghost"
          disabled={revoke.isPending}
          onClick={() => {
            if (
              window.confirm(
                "Revoke this enrollment? Its firewall will reject calls immediately on the hosted gateway and after policy expiry on local wrappers.",
              )
            )
              revoke.mutate();
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
              <div className="rounded-lg border border-border bg-muted/20 p-3 space-y-3">
                <div className="flex flex-wrap items-center justify-between gap-2">
                  <FieldLabel>Gateway mode &amp; upstream credential vault</FieldLabel>
                  <div className="flex gap-1.5">
                    <Button
                      type="button"
                      size="sm"
                      variant={mode === "remote" ? "secondary" : "ghost"}
                      onClick={() => setMode("remote")}
                    >
                      <Globe />
                      Remote HTTP Gateway
                    </Button>
                    <Button
                      type="button"
                      size="sm"
                      variant={mode === "stdio" ? "secondary" : "ghost"}
                      onClick={() => setMode("stdio")}
                    >
                      <Terminal />
                      Local stdio fallback
                    </Button>
                  </div>
                </div>
                {mode === "remote" ? (
                  <div className="space-y-3">
                    <div>
                      <FieldLabel>Upstream HTTPS MCP endpoint</FieldLabel>
                      <Input
                        aria-label="Upstream HTTPS MCP endpoint"
                        value={upstreamUrl}
                        onChange={(e) => setUpstreamUrl(e.target.value)}
                        placeholder="https://mcp.yourcompany.com/mcp"
                        className="font-mono text-xs"
                      />
                    </div>
                    <div>
                      <FieldLabel>
                        {server.has_credential
                          ? "Rotate vaulted upstream Bearer credential (leave blank to keep current)"
                          : "Vault upstream Bearer credential (encrypted at rest)"}
                      </FieldLabel>
                      <Input
                        aria-label="Vaulted upstream Bearer credential"
                        type="password"
                        autoComplete="off"
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
                          <Search />
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
                          <div className="max-h-48 overflow-y-auto rounded-md border border-border bg-background p-2.5 space-y-2">
                            <p className="text-[11px] text-muted-foreground">
                              Discovered {discovered.length} upstream tools
                              (descriptions scanned by Bastio Security). Click a
                              policy action to assign:
                            </p>
                            {discovered.map((t) => (
                              <div
                                key={t.name}
                                className="flex flex-wrap items-center justify-between gap-2 border-b border-border/50 pb-1.5 last:border-0 last:pb-0"
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
                    Local stdio wrapper mode runs <code>bastio mcp-proxy</code>{" "}
                    on the developer machine as a fallback for local-only tools.
                  </p>
                )}
              </div>

              <div>
                <FieldLabel>
                  Allowed tools (optional, comma-separated)
                </FieldLabel>
                <Input
                  aria-label="Allowed tools"
                  value={allowed}
                  onChange={(e) => setAllowed(e.target.value)}
                  placeholder="Empty means all non-blocked tools are inspected"
                />
              </div>
              <div>
                <FieldLabel>
                  Human approval required tools (comma-separated)
                </FieldLabel>
                <Input
                  aria-label="Human approval required tools"
                  value={approvalRequired}
                  onChange={(e) => setApprovalRequired(e.target.value)}
                  placeholder="deploy_service, merge_pull_request, execute_mutation"
                />
              </div>
              <div>
                <FieldLabel>Blocked tools (comma-separated)</FieldLabel>
                <Input
                  aria-label="Blocked tools"
                  value={blocked}
                  onChange={(e) => setBlocked(e.target.value)}
                  placeholder="execute_sql, delete_file"
                />
              </div>

              <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
                <div className="sm:col-span-2">
                  <FieldLabel>
                    Approval notification webhook URL (optional HTTPS)
                  </FieldLabel>
                  <Input
                    aria-label="Approval notification webhook URL"
                    value={webhookUrl}
                    onChange={(e) => setWebhookUrl(e.target.value)}
                    placeholder="https://hooks.slack.com/services/..."
                    className="font-mono text-xs"
                  />
                </div>
                <div>
                  <FieldLabel>Webhook format</FieldLabel>
                  <select
                    aria-label="Webhook format"
                    className="h-9 w-full rounded-md border border-input bg-background px-3 text-sm"
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
              <div className="flex gap-2">
                <Button
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
                  variant="outline"
                  disabled={rotate.isPending}
                  onClick={() => {
                    if (
                      window.confirm(
                        "Replace this token? Update the client configuration to reconnect.",
                      )
                    )
                      rotate.mutate();
                  }}
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
  const selector = (
    <select
      aria-label="MCP server policy"
      className="h-9 rounded-md border border-input bg-background px-3 text-sm"
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
  );

  return (
    <div className="space-y-5 pb-10">
      <AdminPageHeader
        eyebrow="MCP security"
        title="MCP firewalls & gateway"
        description="Host zero-secret remote HTTP/SSE MCP gateways with vaulted credential injection, enforce human-in-the-loop approvals, or wrap local stdio servers. Telemetry excludes raw arguments, outputs, and tokens."
        actions={
          <div className="flex gap-2">
            <Button
              size="sm"
              variant="outline"
              onClick={() => {
                void qc.invalidateQueries({ queryKey: mcpKeys });
              }}
            >
              <RefreshCw />
              Refresh
            </Button>
            <Button size="sm" onClick={() => setConnect(true)}>
              <Plus />
              Connect server
            </Button>
          </div>
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
          },
          {
            label: "Enrolled",
            value: servers.isPending
              ? "—"
              : list.filter((s) => !s.revoked_at).length.toLocaleString(),
            detail: `${remoteCount} remote gateway · ${Math.max(0, list.filter((s) => !s.revoked_at).length - remoteCount)} local stdio`,
          },
          {
            label: "Pending approvals",
            value: servers.isPending
              ? "—"
              : totalPendingApprovals.toLocaleString(),
            detail: "Awaiting operator decision",
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
      <div className="flex flex-wrap gap-2 border-b border-border pb-3">
        {(
          [
            ["events", "Security events"],
            ["approvals", "Human approvals"],
            ["servers", "Servers & policies"],
            ["sandbox", "Inspect a tool call"],
          ] as const
        ).map(([id, label]) => (
          <Button
            key={id}
            size="sm"
            variant={tab === id ? "secondary" : "ghost"}
            onClick={() => setTab(id)}
          >
            {label}
            {id === "approvals" && totalPendingApprovals > 0 ? (
              <Badge variant="outline" className="ml-1.5 font-mono text-[10px]">
                {totalPendingApprovals}
              </Badge>
            ) : null}
          </Button>
        ))}
      </div>
      {error ? (
        <div
          role="alert"
          className="rounded-lg border border-border p-4 text-sm text-destructive"
        >
          Unable to load MCP data: {error.message}
        </div>
      ) : null}
      {tab === "events" ? (
        <>
          <div className="flex flex-wrap gap-3">
            <Input
              aria-label="Filter MCP events"
              className="max-w-sm"
              value={filter}
              onChange={(e) => setFilter(e.target.value)}
              placeholder="Filter tools, servers, outcomes…"
            />
            {selector}
          </div>
          <Card>
            <CardContent className="p-0">
              {events.isPending ? (
                <p className="p-5 text-sm text-muted-foreground">
                  Loading events…
                </p>
              ) : !rows.length ? (
                <EmptyState
                  icon={<Shield />}
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
                  <table className="w-full text-left text-xs">
                    <thead className="border-b border-border text-muted-foreground">
                      <tr>
                        {[
                          "Time",
                          "Tool / server",
                          "Direction",
                          "Outcome",
                          "Categories",
                          "Duration",
                        ].map((t) => (
                          <th className="p-3 font-medium" key={t}>
                            {t}
                          </th>
                        ))}
                      </tr>
                    </thead>
                    <tbody>
                      {rows.map((e) => (
                        <tr className="border-b border-border/60" key={e.id}>
                          <td className="whitespace-nowrap p-3 font-mono tabular-nums">
                            {e.occurred_at
                              ? new Date(e.occurred_at).toLocaleString()
                              : "—"}
                          </td>
                          <td className="p-3">
                            <p className="font-mono">{e.tool_name}</p>
                            <p className="mt-1 text-muted-foreground">
                              {e.server_name} · {e.client}
                            </p>
                          </td>
                          <td className="p-3">
                            {e.direction.replaceAll("_", " ")}
                          </td>
                          <td className="p-3">
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
                          <td className="p-3">
                            {e.categories.join(", ") || "—"}
                          </td>
                          <td className="p-3 font-mono tabular-nums">
                            {e.duration_ms.toFixed(2)} ms
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              )}
            </CardContent>
          </Card>
          <p className="text-xs text-muted-foreground">
            Up to 100 recent events. Request duration measures inspection;
            response duration includes the tool round trip. Reporting is
            bounded; dropped events and connection gaps appear per server.
          </p>
        </>
      ) : tab === "approvals" ? (
        <div className="space-y-3">
          <div className="flex flex-wrap gap-3">
            {selector}
            <select
              aria-label="Approval status filter"
              className="h-9 rounded-md border border-input bg-background px-3 text-sm"
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
          <Card>
            <CardContent className="p-0">
              {approvals.isPending ? (
                <p className="p-5 text-sm text-muted-foreground">
                  Loading approval requests…
                </p>
              ) : !approvalRows.length ? (
                <EmptyState
                  icon={<CheckCircle2 />}
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
                  <table className="w-full text-left text-xs">
                    <thead className="border-b border-border text-muted-foreground">
                      <tr>
                        {[
                          "Requested",
                          "Tool / server",
                          "Argument keys",
                          "Categories",
                          "Status",
                          "Decision",
                        ].map((t) => (
                          <th className="p-3 font-medium" key={t}>
                            {t}
                          </th>
                        ))}
                      </tr>
                    </thead>
                    <tbody>
                      {approvalRows.map((a) => (
                        <tr className="border-b border-border/60" key={a.id}>
                          <td className="whitespace-nowrap p-3 font-mono tabular-nums">
                            <p>
                              {new Date(a.requested_at).toLocaleTimeString()}
                            </p>
                            <p className="mt-0.5 text-[11px] text-muted-foreground">
                              Expires{" "}
                              {new Date(a.expires_at).toLocaleTimeString()}
                            </p>
                          </td>
                          <td className="p-3">
                            <p className="font-mono font-medium">
                              {a.tool_name}
                            </p>
                            <p className="mt-1 text-muted-foreground">
                              {a.server_name} · {a.client}
                            </p>
                          </td>
                          <td className="p-3 font-mono text-[11px]">
                            {a.argument_keys.length
                              ? a.argument_keys.join(", ")
                              : "—"}
                          </td>
                          <td className="p-3">
                            {a.categories.join(", ") || "human_approval"}
                          </td>
                          <td className="p-3">
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
                          <td className="p-3">
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
                                  size="sm"
                                  variant="secondary"
                                  disabled={resolveApproval.isPending}
                                  onClick={() =>
                                    resolveApproval.mutate({
                                      id: a.id,
                                      action: "approved",
                                      reason: reasonByApproval[a.id],
                                    })
                                  }
                                >
                                  <Check />
                                  Approve
                                </Button>
                                <Button
                                  size="sm"
                                  variant="destructive"
                                  disabled={resolveApproval.isPending}
                                  onClick={() =>
                                    resolveApproval.mutate({
                                      id: a.id,
                                      action: "denied",
                                      reason: reasonByApproval[a.id],
                                    })
                                  }
                                >
                                  <XCircle />
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
            </CardContent>
          </Card>
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
        <div className="space-y-3">
          {servers.isPending ? (
            <p className="text-sm text-muted-foreground">Loading servers…</p>
          ) : !list.length ? (
            <EmptyState
              icon={<Server />}
              title="Connect your first MCP server"
              description="Enroll a remote HTTP gateway with vaulted credentials or wrap a local stdio server in Claude Desktop, Cursor, Cline, or your agent."
            />
          ) : (
            list.map((s) => (
              <Card key={s.id}>
                <CardContent className="space-y-3 p-4">
                  <div className="flex flex-wrap justify-between gap-3">
                    <div>
                      <div className="flex flex-wrap items-center gap-2">
                        <h2 className="text-sm font-semibold">{s.name}</h2>
                        <Badge variant="outline" className="gap-1 text-[10px]">
                          {s.mode === "remote" ? (
                            <>
                              <Globe className="size-3" />
                              Remote HTTP Gateway
                            </>
                          ) : (
                            <>
                              <Terminal className="size-3" />
                              Local stdio fallback
                            </>
                          )}
                        </Badge>
                        {s.mode === "remote" && s.has_credential ? (
                          <Badge
                            variant="outline"
                            className="gap-1 text-[10px]"
                          >
                            <KeyRound className="size-3" />
                            Credential vaulted
                          </Badge>
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
                    <Badge variant="outline">{status(s)}</Badge>
                  </div>
                  <p className="text-xs text-muted-foreground">
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
                    · {s.calls} reported calls · {s.blocked} blocked
                    {s.pending_approvals > 0
                      ? ` · ${s.pending_approvals} pending approval`
                      : ""}
                  </p>
                  {s.dropped_events > 0 ? (
                    <p className="text-xs text-destructive">
                      {s.dropped_events} events dropped by the client reporting
                      queue.
                    </p>
                  ) : null}
                  <ServerControls server={s} />
                </CardContent>
              </Card>
            ))
          )}
        </div>
      ) : (
        <Card>
          <CardContent className="space-y-4 p-5">
            <div>
              <h2 className="text-sm font-semibold">
                Inspect without executing
              </h2>
              <p className="mt-1 text-xs text-muted-foreground">
                Uses the gateway and CLI's real inspection code. Input is
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
              {selector}
              <div>
                <FieldLabel>Tool name</FieldLabel>
                <Input
                  aria-label="MCP tool name"
                  required
                  value={tool}
                  onChange={(e) => {
                    setTool(e.target.value);
                    inspect.reset();
                  }}
                />
              </div>
              <div>
                <FieldLabel>Arguments (JSON object)</FieldLabel>
                <textarea
                  aria-label="Tool arguments JSON"
                  className="min-h-36 w-full rounded-md border border-input bg-background p-3 font-mono text-xs"
                  value={args}
                  onChange={(e) => {
                    setArgs(e.target.value);
                    inspect.reset();
                  }}
                />
              </div>
              <Button type="submit" disabled={inspect.isPending}>
                {inspect.isPending ? "Inspecting…" : "Inspect tool call"}
              </Button>
            </form>
            {formError || inspect.error ? (
              <p role="alert" className="text-sm text-destructive">
                {formError || inspect.error?.message}
              </p>
            ) : null}
            {inspect.data ? (
              <div className="space-y-2 rounded-lg border border-border p-4">
                <Badge variant="outline">
                  {inspect.data.action.replaceAll("_", " ")}
                </Badge>
                <p className="text-xs">
                  {inspect.data.categories.join(", ") || "No security findings"}{" "}
                  · {inspect.data.duration_ms.toFixed(2)} ms
                </p>
                {inspect.data.arguments ? (
                  <pre className="overflow-auto text-xs">
                    {JSON.stringify(inspect.data.arguments, null, 2)}
                  </pre>
                ) : null}
              </div>
            ) : null}
          </CardContent>
        </Card>
      )}
      <ConnectMcpDialog open={connect} onOpenChange={setConnect} />
    </div>
  );
}
