import { useState, useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  Check,
  Copy,
  Database,
  FileCode,
  FolderTree,
  Key,
  Laptop,
  Shield,
  ShieldCheck,
  Terminal,
} from "lucide-react";

import { api, type APIKey } from "@/api/client";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { cn } from "@/lib/utils";

interface ConnectMcpDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}

type McpPreset = "postgres" | "bash" | "filesystem" | "github" | "custom";
type ClientTarget = "claude" | "cursor" | "cline" | "terminal";

const MCP_PRESETS = [
  {
    id: "postgres" as McpPreset,
    name: "PostgreSQL Database",
    package: "@modelcontextprotocol/server-postgres",
    icon: Database,
    description: "Blocks destructive SQL (DROP, TRUNCATE) & masks PII in table queries",
    defaultParam: "postgresql://user:pass@localhost:5432/production_db",
    paramLabel: "Postgres Connection URI",
    paramPlaceholder: "postgresql://user:password@host:port/database",
  },
  {
    id: "bash" as McpPreset,
    name: "Bash / Shell Execution",
    package: "@modelcontextprotocol/server-bash",
    icon: Terminal,
    description: "Blocks rm -rf, curl | bash, fork bombs, and remote code execution exploits",
    defaultParam: "",
    paramLabel: "Command Flags / Env (Optional)",
    paramPlaceholder: "--timeout 30s",
  },
  {
    id: "filesystem" as McpPreset,
    name: "Filesystem Access",
    package: "@modelcontextprotocol/server-filesystem",
    icon: FolderTree,
    description: "Restricts tool access to safe workspace paths and prevents root traversal",
    defaultParam: "/Users/workspace/project",
    paramLabel: "Allowed Directory Root",
    paramPlaceholder: "/path/to/allowed/directory",
  },
  {
    id: "github" as McpPreset,
    name: "GitHub Tool Server",
    package: "@modelcontextprotocol/server-github",
    icon: FileCode,
    description: "Prevents unauthorized force pushes and masks exposed tokens in PR bodies",
    defaultParam: "my-org/core-repo",
    paramLabel: "Default Repository / Scope",
    paramPlaceholder: "owner/repository",
  },
  {
    id: "custom" as McpPreset,
    name: "Custom stdio Server",
    package: "custom-binary",
    icon: Laptop,
    description: "Firewall any custom executable or stdio-based MCP process",
    defaultParam: "python3 ./mcp_tools.py",
    paramLabel: "Custom Command",
    paramPlaceholder: "executable-name --flag arg",
  },
];

export function ConnectMcpDialog({ open, onOpenChange }: ConnectMcpDialogProps) {
  const [selectedPreset, setSelectedPreset] = useState<McpPreset>("postgres");
  const [targetClient, setTargetClient] = useState<ClientTarget>("claude");
  const [customParam, setCustomParam] = useState("");
  const [profile, setProfile] = useState<"strict" | "standard">("strict");
  const [copiedKey, setCopiedKey] = useState<string | null>(null);

  const { data: apiKeys = [] } = useQuery({
    queryKey: ["api-keys"],
    queryFn: api.apiKeys.list,
    enabled: open,
  });

  const firstKey = apiKeys.find((k: APIKey) => k.is_active);
  const activeKey = firstKey ? `${firstKey.key_prefix}sk_live_agent` : "sk_bastio_mcp_live_token";
  const apiBaseUrl = typeof window !== "undefined" ? window.location.origin : "http://localhost:4000";

  const preset = MCP_PRESETS.find((p) => p.id === selectedPreset)!;
  const activeParam = customParam || preset.defaultParam;

  const copyToClipboard = async (text: string, id: string) => {
    await navigator.clipboard.writeText(text);
    setCopiedKey(id);
    window.setTimeout(() => setCopiedKey(null), 2000);
  };

  // Build command args array
  const commandArgs = useMemo(() => {
    const args = ["mcp-proxy", "--api-key", activeKey, "--upstream", apiBaseUrl];
    if (profile === "strict") {
      args.push("--profile", "strict");
    }
    args.push("--");

    if (selectedPreset === "postgres") {
      args.push("npx", "-y", "@modelcontextprotocol/server-postgres", activeParam || "postgresql://localhost:5432/mydb");
    } else if (selectedPreset === "bash") {
      args.push("npx", "-y", "@modelcontextprotocol/server-bash");
    } else if (selectedPreset === "filesystem") {
      args.push("npx", "-y", "@modelcontextprotocol/server-filesystem", activeParam || "/Users/workspace/project");
    } else if (selectedPreset === "github") {
      args.push("npx", "-y", "@modelcontextprotocol/server-github");
    } else {
      const parts = (activeParam || "python3 server.py").split(" ");
      args.push(...parts);
    }
    return args;
  }, [activeKey, apiBaseUrl, profile, selectedPreset, activeParam]);

  // Generate Claude Desktop Config
  const claudeDesktopConfig = useMemo(() => {
    const serverName = `bastio-${selectedPreset}-firewall`;
    return JSON.stringify(
      {
        mcpServers: {
          [serverName]: {
            command: "bastio",
            args: commandArgs,
          },
        },
      },
      null,
      2
    );
  }, [selectedPreset, commandArgs]);

  // Generate Cursor Config
  const cursorConfig = useMemo(() => {
    const serverName = `bastio-${selectedPreset}`;
    return JSON.stringify(
      {
        mcp: {
          servers: {
            [serverName]: {
              command: "bastio",
              args: commandArgs,
            },
          },
        },
      },
      null,
      2
    );
  }, [selectedPreset, commandArgs]);

  // Generate Terminal command
  const terminalCommand = useMemo(() => {
    return `bastio ${commandArgs.map((a) => (a.includes(" ") ? `"${a}"` : a)).join(" ")}`;
  }, [commandArgs]);

  const displayedSnippet =
    targetClient === "claude"
      ? claudeDesktopConfig
      : targetClient === "cursor" || targetClient === "cline"
      ? cursorConfig
      : terminalCommand;

  const configPath =
    targetClient === "claude"
      ? "~/Library/Application Support/Claude/claude_desktop_config.json"
      : targetClient === "cursor"
      ? ".cursor/mcp.json"
      : targetClient === "cline"
      ? "~/.vscode/extensions/cline_mcp_settings.json"
      : "Terminal";

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-4xl max-h-[90vh] overflow-y-auto border-border bg-card p-0">
        {/* Header */}
        <div className="p-6 border-b border-border bg-muted/20">
          <DialogHeader>
            <div className="flex items-center justify-between">
              <div className="flex items-center gap-2.5">
                <span className="flex size-9 items-center justify-center rounded-lg bg-accent/10 text-accent border border-accent/20">
                  <ShieldCheck className="size-5" />
                </span>
                <div>
                  <DialogTitle className="text-lg font-semibold text-foreground">
                    Connect &amp; Protect MCP Tool Server
                  </DialogTitle>
                  <DialogDescription className="text-xs text-muted-foreground mt-0.5">
                    Wrap any Model Context Protocol tool with <code className="text-foreground font-mono">bastio mcp-proxy</code> to intercept destructive actions and mask PII in real time.
                  </DialogDescription>
                </div>
              </div>
              <Badge variant="outline" className="text-accent border-accent/30 bg-accent/10 font-mono text-[10px] hidden sm:flex">
                stdio JSON-RPC 2.0
              </Badge>
            </div>
          </DialogHeader>
        </div>

        {/* 2-Column Wizard Layout */}
        <div className="p-6 grid grid-cols-1 lg:grid-cols-12 gap-6">
          {/* Left Column: Configurator (7 Cols) */}
          <div className="lg:col-span-6 space-y-5">
            {/* Step 1: Select Server Type */}
            <div>
              <label className="text-xs font-semibold uppercase tracking-wider text-muted-foreground block mb-2">
                1. Select Tool Server Template
              </label>
              <div className="grid grid-cols-1 sm:grid-cols-2 gap-2">
                {MCP_PRESETS.map((p) => {
                  const Icon = p.icon;
                  const isSelected = selectedPreset === p.id;
                  return (
                    <button
                      key={p.id}
                      type="button"
                      onClick={() => {
                        setSelectedPreset(p.id);
                        setCustomParam("");
                      }}
                      className={cn(
                        "p-3 rounded-lg border text-left transition-all flex flex-col justify-between gap-2",
                        isSelected
                          ? "border-accent bg-accent/5 ring-1 ring-accent"
                          : "border-border bg-card hover:bg-muted/30 hover:border-border-default"
                      )}
                    >
                      <div className="flex items-center justify-between w-full">
                        <div className="flex items-center gap-2">
                          <Icon className={cn("size-4", isSelected ? "text-accent" : "text-muted-foreground")} />
                          <span className="text-xs font-semibold text-foreground">{p.name}</span>
                        </div>
                        {isSelected && <span className="size-2 rounded-full bg-accent" />}
                      </div>
                      <p className="text-[10px] text-muted-foreground leading-tight line-clamp-2">
                        {p.description}
                      </p>
                    </button>
                  );
                })}
              </div>
            </div>

            {/* Step 2: Server Parameters */}
            <div>
              <label className="text-xs font-semibold uppercase tracking-wider text-muted-foreground block mb-1.5">
                2. Server Target / URI
              </label>
              <Input
                value={customParam}
                onChange={(e) => setCustomParam(e.target.value)}
                placeholder={preset.paramPlaceholder}
                className="font-mono text-xs h-9 bg-muted/20"
              />
              <p className="text-[10px] text-muted-foreground mt-1">
                Default: <code className="font-mono text-foreground">{preset.defaultParam || "none"}</code>
              </p>
            </div>

            {/* Step 3: Security Profile */}
            <div>
              <label className="text-xs font-semibold uppercase tracking-wider text-muted-foreground block mb-1.5">
                3. Firewall Security Policy
              </label>
              <div className="grid grid-cols-2 gap-2">
                <button
                  type="button"
                  onClick={() => setProfile("strict")}
                  className={cn(
                    "p-2.5 rounded-lg border text-left transition-all",
                    profile === "strict"
                      ? "border-success bg-success/5 ring-1 ring-success"
                      : "border-border bg-card hover:bg-muted/20"
                  )}
                >
                  <div className="flex items-center gap-1.5 text-xs font-semibold text-foreground mb-0.5">
                    <ShieldCheck className="size-3.5 text-success" />
                    Strict Guardrails
                  </div>
                  <p className="text-[10px] text-muted-foreground">
                    Block all destructive SQL/shell commands &amp; tokenize PII parameters.
                  </p>
                </button>

                <button
                  type="button"
                  onClick={() => setProfile("standard")}
                  className={cn(
                    "p-2.5 rounded-lg border text-left transition-all",
                    profile === "standard"
                      ? "border-accent bg-accent/5 ring-1 ring-accent"
                      : "border-border bg-card hover:bg-muted/20"
                  )}
                >
                  <div className="flex items-center gap-1.5 text-xs font-semibold text-foreground mb-0.5">
                    <Shield className="size-3.5 text-accent" />
                    Standard Audit
                  </div>
                  <p className="text-[10px] text-muted-foreground">
                    Audit and log all executions; block known critical exploit vectors.
                  </p>
                </button>
              </div>
            </div>
          </div>

          {/* Right Column: Code Generator & Export (6 Cols) */}
          <div className="lg:col-span-6 flex flex-col justify-between space-y-4">
            <div className="space-y-3">
              {/* Target Client Switcher */}
              <div>
                <label className="text-xs font-semibold uppercase tracking-wider text-muted-foreground block mb-2">
                  4. Select Host Client / IDE
                </label>
                <div className="flex p-1 bg-muted/40 border border-border rounded-lg gap-1">
                  {[
                    { id: "claude" as ClientTarget, label: "Claude Desktop" },
                    { id: "cursor" as ClientTarget, label: "Cursor AI" },
                    { id: "cline" as ClientTarget, label: "Cline / Windsurf" },
                    { id: "terminal" as ClientTarget, label: "CLI Command" },
                  ].map((t) => (
                    <button
                      key={t.id}
                      type="button"
                      onClick={() => setTargetClient(t.id)}
                      className={cn(
                        "flex-1 py-1 px-2 text-[11px] font-medium rounded-md transition-all text-center",
                        targetClient === t.id
                          ? "bg-card text-foreground font-semibold shadow-sm border border-border"
                          : "text-muted-foreground hover:text-foreground"
                      )}
                    >
                      {t.label}
                    </button>
                  ))}
                </div>
              </div>

              {/* Path Bar */}
              {targetClient !== "terminal" && (
                <div className="flex items-center justify-between gap-2 p-2 rounded bg-muted/30 border border-border text-[11px]">
                  <span className="text-muted-foreground truncate font-mono">{configPath}</span>
                  <Button
                    variant="ghost"
                    size="sm"
                    className="h-6 text-[10px] gap-1 px-2 shrink-0"
                    onClick={() => copyToClipboard(configPath, "path")}
                  >
                    {copiedKey === "path" ? <Check className="size-3 text-success" /> : <Copy className="size-3" />}
                    {copiedKey === "path" ? "Copied" : "Copy Path"}
                  </Button>
                </div>
              )}

              {/* Code Snippet Box */}
              <div className="relative group">
                <pre className="p-4 rounded-xl bg-muted/60 border border-border font-mono text-[11px] text-foreground overflow-x-auto max-h-[260px] leading-relaxed">
                  {displayedSnippet}
                </pre>
                <Button
                  variant="outline"
                  size="sm"
                  className="absolute top-2.5 right-2.5 h-7 text-xs gap-1.5 bg-background shadow-sm border-border"
                  onClick={() => copyToClipboard(displayedSnippet, "snippet")}
                >
                  {copiedKey === "snippet" ? <Check className="size-3 text-success" /> : <Copy className="size-3" />}
                  {copiedKey === "snippet" ? "Copied Config" : "Copy Config"}
                </Button>
              </div>
            </div>

            {/* Quick Helper Note */}
            <div className="p-3 rounded-lg bg-accent/5 border border-accent/20 text-xs flex items-start gap-2.5 text-muted-foreground">
              <Terminal className="size-4 text-accent shrink-0 mt-0.5" />
              <div className="leading-normal">
                <strong className="text-foreground">Zero Gateway Overhead:</strong> All JSON-RPC messages are inspected locally in &lt;2ms. If an agent emits a dangerous command, <code className="text-accent font-mono">-32600</code> is returned before touching the child process.
              </div>
            </div>
          </div>
        </div>

        {/* Footer */}
        <div className="p-4 bg-muted/30 border-t border-border flex items-center justify-between text-xs">
          <div className="flex items-center gap-1.5 text-muted-foreground">
            <Key className="size-3.5 text-accent" />
            <span>Bound to active API key:</span>
            <code className="font-mono text-foreground font-semibold">
              {activeKey.slice(0, 16)}...
            </code>
          </div>
          <Button
            onClick={() => {
              copyToClipboard(displayedSnippet, "footer-copy");
              setTimeout(() => onOpenChange(false), 600);
            }}
            className="gap-1.5 text-xs font-semibold"
          >
            <Check className="size-3.5" />
            Copy &amp; Done
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  );
}
