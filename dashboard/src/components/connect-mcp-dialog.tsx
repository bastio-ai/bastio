import { useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Check, Copy, Globe, Terminal } from "lucide-react";
import { mcpApi, mcpKeys, type McpServer } from "@/api/mcp";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { FieldLabel } from "@/components/admin/admin-primitives";

export function McpEnrollment({
  token,
  name,
  client,
  mode = "stdio",
}: {
  token: string;
  name: string;
  client: string;
  mode?: "stdio" | "remote";
}) {
  const [viewMode, setViewMode] = useState<"remote" | "stdio">(
    mode === "remote" ? "remote" : "stdio",
  );
  const [command, setCommand] = useState("npx");
  const [args, setArgs] = useState(
    '["-y", "@modelcontextprotocol/server-filesystem", "/path/to/your/project"]',
  );
  const [copied, setCopied] = useState(false);

  let parsed: string[] = [];
  let error = "";
  if (viewMode === "stdio") {
    try {
      const value: unknown = JSON.parse(args);
      if (!Array.isArray(value) || !value.every((v) => typeof v === "string"))
        throw new Error();
      parsed = value as string[];
    } catch {
      error = "Enter arguments as a JSON array of strings.";
    }
  }

  const base = window.location.origin;
  const gatewayUrl = `${base}/v1/mcp/gateway`;

  const remoteConfig = JSON.stringify(
    {
      mcpServers: {
        [name]: {
          url: gatewayUrl,
          headers: {
            Authorization: `Bearer ${token}`,
          },
        },
      },
    },
    null,
    2,
  );

  const shellQuote = (s: string) => "'" + s.replaceAll("'", "'\\''") + "'";
  const remoteTerminal = `curl -X POST ${shellQuote(gatewayUrl)} \\\n  -H ${shellQuote(`Authorization: Bearer ${token}`)} \\\n  -H 'Content-Type: application/json' \\\n  -d '{"jsonrpc":"2.0","id":1,"method":"tools/list"}'`;

  const fullArgs = [
    "mcp-proxy",
    "--control-url",
    base,
    "--token-env",
    "BASTIO_MCP_TOKEN",
    "--",
    command,
    ...parsed,
  ];
  const stdioConfig = JSON.stringify(
    {
      mcpServers: {
        [name]: {
          command: "bastio",
          args: fullArgs,
          env: { BASTIO_MCP_TOKEN: token },
        },
      },
    },
    null,
    2,
  );
  const stdioTerminal = `export BASTIO_MCP_TOKEN=${shellQuote(token)}\nbastio ${fullArgs.map(shellQuote).join(" ")}`;

  const snippet =
    viewMode === "remote"
      ? client === "terminal"
        ? remoteTerminal
        : remoteConfig
      : client === "terminal"
        ? stdioTerminal
        : stdioConfig;

  return (
    <div className="space-y-3">
      <div className="flex flex-wrap gap-2 rounded-lg border border-border bg-muted/20 p-1.5">
        <Button
          type="button"
          size="sm"
          variant={viewMode === "remote" ? "secondary" : "ghost"}
          onClick={() => {
            setViewMode("remote");
            setCopied(false);
          }}
        >
          <Globe />
          Remote HTTP Gateway
        </Button>
        <Button
          type="button"
          size="sm"
          variant={viewMode === "stdio" ? "secondary" : "ghost"}
          onClick={() => {
            setViewMode("stdio");
            setCopied(false);
          }}
        >
          <Terminal />
          Local stdio wrapper (fallback)
        </Button>
      </div>

      {viewMode === "remote" ? (
        <p className="text-xs text-muted-foreground">
          Save this enrollment token now; it is shown only once. Point Cursor (
          <code>.cursor/mcp.json</code>), Claude Desktop, Claude Code, or Cline
          directly at Bastio’s hosted Streamable HTTP gateway. Upstream
          credentials stay encrypted in Bastio’s vault—no secrets or CLI binary
          required on developer laptops.
        </p>
      ) : (
        <>
          <p className="text-xs text-muted-foreground">
            Save this enrollment token now; it is shown only once. For local
            tool servers (filesystem, local databases), install the Bastio CLI:{" "}
            <code>go install github.com/bastio-ai/bastio/cmd/bastio@main</code>.
          </p>
          <div>
            <FieldLabel>Local server executable</FieldLabel>
            <Input
              aria-label="Local MCP server executable"
              value={command}
              onChange={(e) => setCommand(e.target.value)}
              placeholder="npx, python3, or a local executable"
            />
          </div>
          <div>
            <FieldLabel>Local arguments (JSON array)</FieldLabel>
            <Input
              aria-label="Local MCP server arguments"
              value={args}
              onChange={(e) => setArgs(e.target.value)}
              className="font-mono text-xs"
            />
          </div>
        </>
      )}

      {error ? (
        <p role="alert" className="text-xs text-destructive">
          {error}
        </p>
      ) : (
        <pre className="max-h-72 overflow-auto rounded-lg border border-border bg-muted/30 p-3 font-mono text-[11px]">
          {snippet}
        </pre>
      )}
      <Button
        disabled={
          viewMode === "stdio" && (Boolean(error) || !command.trim())
        }
        onClick={async () => {
          await navigator.clipboard.writeText(snippet);
          setCopied(true);
        }}
        variant="outline"
        size="sm"
      >
        {copied ? <Check /> : <Copy />}
        {copied ? "Copied configuration" : "Copy configuration"}
      </Button>
      <p className="text-xs text-muted-foreground">
        Keep this configuration private. Tool arguments, tool results, and
        upstream credentials are never written to telemetry or approval logs.
      </p>
    </div>
  );
}

export function ConnectMcpDialog({
  open,
  onOpenChange,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const qc = useQueryClient();
  const [name, setName] = useState("");
  const [client, setClient] = useState<McpServer["client"]>("cursor");
  const [mode, setMode] = useState<"remote" | "stdio">("remote");
  const [upstreamUrl, setUpstreamUrl] = useState("");
  const [credential, setCredential] = useState("");
  const [approvalTools, setApprovalTools] = useState("");

  const create = useMutation({
    mutationFn: mcpApi.create,
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: mcpKeys });
    },
  });

  const parseTools = (val: string) => [
    ...new Set(
      val
        .split(/[\n,]/)
        .map((s) => s.trim())
        .filter(Boolean),
    ),
  ];

  return (
    <Dialog
      open={open}
      onOpenChange={(value) => {
        onOpenChange(value);
        if (!value) {
          create.reset();
          setName("");
          setUpstreamUrl("");
          setCredential("");
          setApprovalTools("");
        }
      }}
    >
      <DialogContent className="max-h-[90vh] overflow-y-auto sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>Connect an MCP firewall</DialogTitle>
          <DialogDescription>
            Enroll a hosted remote HTTP/SSE MCP gateway with credential
            vaulting, or wrap a local stdio server with the Bastio CLI fallback.
          </DialogDescription>
        </DialogHeader>
        {create.data ? (
          <McpEnrollment
            token={create.data.enrollment_token}
            name={create.data.server.name}
            client={create.data.server.client}
            mode={create.data.server.mode}
          />
        ) : (
          <form
            className="space-y-4"
            onSubmit={(e) => {
              e.preventDefault();
              create.mutate({
                name: name.trim(),
                client,
                mode,
                upstream_url: mode === "remote" ? upstreamUrl.trim() : undefined,
                auth_type:
                  mode === "remote"
                    ? credential.trim()
                      ? "bearer"
                      : "none"
                    : "none",
                credential:
                  mode === "remote" && credential.trim()
                    ? credential.trim()
                    : undefined,
                policy: {
                  allowed_tools: [],
                  blocked_tools: [],
                  approval_required_tools: parseTools(approvalTools),
                },
              });
            }}
          >
            <div>
              <FieldLabel>Deployment mode</FieldLabel>
              <div className="grid grid-cols-1 gap-2 sm:grid-cols-2">
                <button
                  type="button"
                  onClick={() => setMode("remote")}
                  className={`rounded-lg border p-3 text-left transition-colors ${
                    mode === "remote"
                      ? "border-foreground bg-muted/40"
                      : "border-border hover:bg-muted/20"
                  }`}
                >
                  <div className="flex items-center gap-2 text-xs font-semibold">
                    <Globe className="size-3.5" />
                    Remote HTTP Gateway (Recommended)
                  </div>
                  <p className="mt-1 text-[11px] text-muted-foreground">
                    Zero-install URL for Cursor &amp; Claude. Bastio vaults
                    upstream API tokens and injects them on egress.
                  </p>
                </button>
                <button
                  type="button"
                  onClick={() => setMode("stdio")}
                  className={`rounded-lg border p-3 text-left transition-colors ${
                    mode === "stdio"
                      ? "border-foreground bg-muted/40"
                      : "border-border hover:bg-muted/20"
                  }`}
                >
                  <div className="flex items-center gap-2 text-xs font-semibold">
                    <Terminal className="size-3.5" />
                    Local stdio wrapper (Fallback)
                  </div>
                  <p className="mt-1 text-[11px] text-muted-foreground">
                    Wraps local-only child processes via{" "}
                    <code>bastio mcp-proxy</code> on the developer machine.
                  </p>
                </button>
              </div>
            </div>
            <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
              <div>
                <FieldLabel>Server name</FieldLabel>
                <Input
                  aria-label="MCP server name"
                  required
                  maxLength={100}
                  value={name}
                  onChange={(e) => setName(e.target.value)}
                  placeholder={
                    mode === "remote"
                      ? "GitHub / Linear / Postgres MCP"
                      : "Engineering filesystem"
                  }
                />
              </div>
              <div>
                <FieldLabel>Client</FieldLabel>
                <select
                  aria-label="MCP client"
                  className="h-9 w-full rounded-md border border-input bg-background px-3 text-sm"
                  value={client}
                  onChange={(e) =>
                    setClient(e.target.value as McpServer["client"])
                  }
                >
                  <option value="cursor">Cursor</option>
                  <option value="claude">Claude Desktop / Code</option>
                  <option value="cline">Cline</option>
                  <option value="terminal">Terminal / custom agent</option>
                  <option value="other">Other MCP client</option>
                </select>
              </div>
            </div>

            {mode === "remote" ? (
              <>
                <div>
                  <FieldLabel>Upstream HTTPS MCP endpoint</FieldLabel>
                  <Input
                    aria-label="Upstream HTTPS MCP endpoint"
                    required
                    type="url"
                    value={upstreamUrl}
                    onChange={(e) => setUpstreamUrl(e.target.value)}
                    placeholder="https://mcp.yourcompany.com/mcp"
                    className="font-mono text-xs"
                  />
                </div>
                <div>
                  <FieldLabel>
                    Upstream Bearer token (optional, encrypted at rest)
                  </FieldLabel>
                  <Input
                    aria-label="Upstream Bearer credential"
                    type="password"
                    autoComplete="off"
                    value={credential}
                    onChange={(e) => setCredential(e.target.value)}
                    placeholder="ghp_..., lin_api_..., or service bearer token"
                    className="font-mono text-xs"
                  />
                </div>
              </>
            ) : null}

            <div>
              <FieldLabel>
                Tools requiring human approval (optional, comma-separated)
              </FieldLabel>
              <Input
                aria-label="Tools requiring human approval"
                value={approvalTools}
                onChange={(e) => setApprovalTools(e.target.value)}
                placeholder="deploy_service, execute_sql, merge_pull_request"
              />
            </div>

            <p className="text-xs text-muted-foreground">
              The firewall inspects tool descriptions, arguments, and responses
              inline. High-risk tools hold execution until approved in the
              dashboard or via Slack/Teams webhook notification.
            </p>
            {create.error ? (
              <p role="alert" className="text-xs text-destructive">
                {create.error.message}
              </p>
            ) : null}
            <Button
              type="submit"
              disabled={
                !name.trim() ||
                (mode === "remote" && !upstreamUrl.trim()) ||
                create.isPending
              }
            >
              {create.isPending ? "Enrolling…" : "Enroll server"}
            </Button>
          </form>
        )}
      </DialogContent>
    </Dialog>
  );
}
