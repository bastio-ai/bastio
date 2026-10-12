import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Plus, RefreshCw, Server, Shield } from "lucide-react";
import { mcpApi, mcpKeys, type McpPolicy, type McpServer } from "@/api/mcp";
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
function ServerControls({ server }: { server: McpServer }) {
  const qc = useQueryClient();
  const [open, setOpen] = useState(false);
  const [allowed, setAllowed] = useState("");
  const [blocked, setBlocked] = useState("");
  const changed = () => {
    void qc.invalidateQueries({ queryKey: mcpKeys });
  };
  const save = useMutation({
    mutationFn: (p: McpPolicy) => mcpApi.policy(server.id, p),
    onSuccess: () => {
      changed();
      setOpen(false);
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
            save.reset();
            rotate.reset();
            setOpen(true);
          }}
        >
          Tool policy &amp; credentials
        </Button>
        <Button
          size="sm"
          variant="ghost"
          disabled={revoke.isPending}
          onClick={() => {
            if (
              window.confirm(
                "Revoke this enrollment? Its firewall will reject calls after receiving the revocation or when its cached policy expires.",
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
            />
          ) : (
            <div className="space-y-4">
              <div>
                <FieldLabel>
                  Allowed tools (optional, comma-separated)
                </FieldLabel>
                <Input
                  aria-label="Allowed tools"
                  value={allowed}
                  onChange={(e) => setAllowed(e.target.value)}
                  placeholder="Empty means all tools are inspected"
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
              <p className="text-xs text-muted-foreground">
                Names match exactly; blocks take precedence. Allowed tools still
                pass through security scanning. Changes refresh every five
                seconds.
              </p>
              {save.error || rotate.error ? (
                <p role="alert" className="text-xs text-destructive">
                  {save.error?.message ?? rotate.error?.message}
                </p>
              ) : null}
              <div className="flex gap-2">
                <Button
                  disabled={save.isPending}
                  onClick={() =>
                    save.mutate({
                      allowed_tools: tools(allowed),
                      blocked_tools: tools(blocked),
                    })
                  }
                >
                  Save policy
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
                  Replace token
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
  const [tab, setTab] = useState<"events" | "servers" | "sandbox">("events");
  const [connect, setConnect] = useState(false);
  const [filter, setFilter] = useState("");
  const [selected, setSelected] = useState("");
  const [tool, setTool] = useState("filesystem.read_file");
  const [args, setArgs] = useState(
    '{"path":"/path/to/your/project/README.md"}',
  );
  const [formError, setFormError] = useState("");
  const qc = useQueryClient();
  const servers = useQuery({
    queryKey: [...mcpKeys, "servers"],
    queryFn: mcpApi.servers,
    refetchInterval: 5000,
  });
  const events = useQuery({
    queryKey: [...mcpKeys, "events", selected],
    queryFn: () => mcpApi.events(selected || undefined),
    refetchInterval: 5000,
  });
  const inspect = useMutation({ mutationFn: mcpApi.inspect });
  const list = servers.data ?? [];
  const policy = list.find((s) => s.id === selected)?.policy ?? {
    allowed_tools: [],
    blocked_tools: [],
  };
  const rows = (events.data ?? []).filter((e) =>
    `${e.tool_name} ${e.server_name} ${e.client} ${e.action} ${e.categories.join(" ")}`
      .toLowerCase()
      .includes(filter.toLowerCase()),
  );
  const error = servers.error ?? events.error;
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
        title="MCP firewalls"
        description="Protect local tool servers and review real security outcomes. Events refresh every five seconds. Proxy telemetry excludes arguments and results."
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
            detail: "Current workspace",
          },
          {
            label: "Calls inspected",
            value: servers.isPending
              ? "—"
              : list.reduce((n, s) => n + s.calls, 0).toLocaleString(),
            detail: "Reported requests · last 30 days",
          },
          {
            label: "Calls blocked",
            value: servers.isPending
              ? "—"
              : list.reduce((n, s) => n + s.blocked, 0).toLocaleString(),
            detail: "Policy or security decision",
          },
        ]}
      />
      <div className="flex flex-wrap gap-2 border-b border-border pb-3">
        {(
          [
            ["events", "Security events"],
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
                  description="Connect a server and run a tool through its Bastio configuration. Its real inspection result will appear here."
                />
              ) : (
                <div className="overflow-x-auto">
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
                          <td className="whitespace-nowrap p-3">
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
                                e.action === "blocked"
                                  ? "destructive"
                                  : "outline"
                              }
                            >
                              {e.action}
                            </Badge>
                          </td>
                          <td className="p-3">
                            {e.categories.join(", ") || "—"}
                          </td>
                          <td className="p-3">{e.duration_ms.toFixed(2)} ms</td>
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
      ) : tab === "servers" ? (
        <div className="space-y-3">
          {servers.isPending ? (
            <p className="text-sm text-muted-foreground">Loading servers…</p>
          ) : !list.length ? (
            <EmptyState
              icon={<Server />}
              title="Connect your first MCP server"
              description="Enroll a server and install its configuration in Claude Desktop, Cursor, Cline, or your agent."
            />
          ) : (
            list.map((s) => (
              <Card key={s.id}>
                <CardContent className="space-y-3 p-4">
                  <div className="flex flex-wrap justify-between gap-3">
                    <div>
                      <h2 className="text-sm font-semibold">{s.name}</h2>
                      <p className="mt-1 text-xs text-muted-foreground">
                        {s.client} ·{" "}
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
                    {s.policy.blocked_tools.length
                      ? `Blocked: ${s.policy.blocked_tools.join(", ")}`
                      : "No explicit tool blocks"}{" "}
                    · {s.calls} reported calls · {s.blocked} blocked
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
                Uses the CLI's real inspection code. Input is processed for this
                request and not added to the event store.
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
                <Badge variant="outline">{inspect.data.action}</Badge>
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
