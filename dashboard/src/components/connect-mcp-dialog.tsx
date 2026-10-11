import { useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Check, Copy } from "lucide-react";
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
}: {
  token: string;
  name: string;
  client: string;
}) {
  const [command, setCommand] = useState("npx");
  const [args, setArgs] = useState(
    '["-y", "@modelcontextprotocol/server-filesystem", "/path/to/your/project"]',
  );
  const [copied, setCopied] = useState(false);
  let parsed: string[] = [];
  let error = "";
  try {
    const value: unknown = JSON.parse(args);
    if (!Array.isArray(value) || !value.every((v) => typeof v === "string"))
      throw new Error();
    parsed = value as string[];
  } catch {
    error = "Enter arguments as a JSON array of strings.";
  }
  const base = window.location.origin;
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
  const config = JSON.stringify(
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
  const shellQuote = (s: string) => "'" + s.replaceAll("'", "'\\''") + "'";
  const terminal = `export BASTIO_MCP_TOKEN=${shellQuote(token)}\nbastio ${fullArgs.map(shellQuote).join(" ")}`;
  const snippet = client === "terminal" ? terminal : config;
  return (
    <div className="space-y-3">
      <p className="text-xs text-muted-foreground">
        Save this enrollment token now; it is shown only once. Install the
        current Bastio CLI on the computer running your tool server:{" "}
        <code>go install github.com/bastio-ai/bastio/cmd/bastio@main</code>.
        Check that <code>bastio mcp-proxy -h</code> lists{" "}
        <code>--control-url</code>.
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
      <p className="text-xs text-muted-foreground">
        Claude Desktop: edit <code>claude_desktop_config.json</code>. Cursor:
        edit <code>.cursor/mcp.json</code>. Cline: paste the server entry into
        its MCP configuration. The child process runs locally.
      </p>
      {error ? (
        <p role="alert" className="text-xs text-destructive">
          {error}
        </p>
      ) : (
        <pre className="max-h-72 overflow-auto rounded-lg border border-border bg-muted/30 p-3 text-[11px]">
          {snippet}
        </pre>
      )}
      <Button
        disabled={Boolean(error) || !command.trim()}
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
        Keep this configuration private. Tool arguments, tool results, and the
        child server’s connection credentials are excluded from telemetry.
        Server heartbeats and policy changes refresh every five seconds.
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
  const [client, setClient] = useState<McpServer["client"]>("claude");
  const create = useMutation({
    mutationFn: mcpApi.create,
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: mcpKeys });
    },
  });
  return (
    <Dialog
      open={open}
      onOpenChange={(value) => {
        onOpenChange(value);
        if (!value) {
          create.reset();
          setName("");
        }
      }}
    >
      <DialogContent className="max-h-[90vh] overflow-y-auto sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>Connect an MCP firewall</DialogTitle>
          <DialogDescription>
            Enroll your local tool server and connect its real security events
            to this workspace.
          </DialogDescription>
        </DialogHeader>
        {create.data ? (
          <McpEnrollment
            token={create.data.enrollment_token}
            name={create.data.server.name}
            client={create.data.server.client}
          />
        ) : (
          <form
            className="space-y-4"
            onSubmit={(e) => {
              e.preventDefault();
              create.mutate({
                name: name.trim(),
                client,
                policy: { allowed_tools: [], blocked_tools: [] },
              });
            }}
          >
            <div>
              <FieldLabel>Server name</FieldLabel>
              <Input
                aria-label="MCP server name"
                required
                maxLength={100}
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder="Engineering filesystem"
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
                <option value="claude">Claude Desktop</option>
                <option value="cursor">Cursor</option>
                <option value="cline">Cline</option>
                <option value="terminal">Terminal / custom agent</option>
                <option value="other">Other MCP client</option>
              </select>
            </div>
            <p className="text-xs text-muted-foreground">
              The firewall inspects calls and text results locally. The
              dashboard retains only tool names, scan outcomes, timing, and
              server health in a 30-day activity window.
            </p>
            {create.error ? (
              <p role="alert" className="text-xs text-destructive">
                {create.error.message}
              </p>
            ) : null}
            <Button type="submit" disabled={!name.trim() || create.isPending}>
              {create.isPending ? "Enrolling…" : "Enroll server"}
            </Button>
          </form>
        )}
      </DialogContent>
    </Dialog>
  );
}
