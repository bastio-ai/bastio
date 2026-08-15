import { useState } from "react";
import {
  Database,
  FileCode,
  FolderTree,
  Play,
  Plus,
  Search,
  Server,
  Shield,
  ShieldAlert,
  Terminal,
} from "lucide-react";

import { AdminPageHeader, AdminSummaryStrip } from "@/components/admin/admin-primitives";
import { ConnectMcpDialog } from "@/components/connect-mcp-dialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { cn } from "@/lib/utils";

interface ToolCallEvent {
  id: string;
  tool_name: string;
  server: string;
  action: "BLOCKED" | "MASKED" | "ALLOWED";
  client: string;
  arguments_preview: string;
  threat_type?: string;
  duration_ms: number;
  timestamp: string;
}

const mockToolCalls: ToolCallEvent[] = [
  {
    id: "call-001",
    tool_name: "bash.run_command",
    server: "local-bash",
    action: "BLOCKED",
    client: "Claude Desktop",
    arguments_preview: '{"command": "rm -rf /var/data && cat /etc/shadow"}',
    threat_type: "destructive_command / prompt_injection",
    duration_ms: 1.4,
    timestamp: "2 mins ago",
  },
  {
    id: "call-002",
    tool_name: "postgres.execute_query",
    server: "prod-postgres-readonly",
    action: "BLOCKED",
    client: "Cursor AI Agent",
    arguments_preview: '{"query": "DROP TABLE customers CASCADE;"}',
    threat_type: "destructive_sql",
    duration_ms: 1.8,
    timestamp: "14 mins ago",
  },
  {
    id: "call-003",
    tool_name: "crm.lookup_customer",
    server: "salesforce-mcp",
    action: "MASKED",
    client: "Claude Desktop",
    arguments_preview: '{"ssn": "[SSN-MASKED-8472]", "email": "john.doe@example.com"}',
    threat_type: "pii_sanitized",
    duration_ms: 2.1,
    timestamp: "32 mins ago",
  },
  {
    id: "call-004",
    tool_name: "filesystem.read_file",
    server: "workspace-fs",
    action: "ALLOWED",
    client: "Windsurf",
    arguments_preview: '{"path": "src/components/button.tsx"}',
    duration_ms: 0.9,
    timestamp: "1 hour ago",
  },
  {
    id: "call-005",
    tool_name: "github.create_pull_request",
    server: "github-mcp",
    action: "ALLOWED",
    client: "Claude Desktop",
    arguments_preview: '{"title": "feat: add semantic caching", "base": "main"}',
    duration_ms: 1.2,
    timestamp: "2 hours ago",
  },
];

const mockServers = [
  {
    id: "srv-1",
    name: "postgres-mcp",
    command: "npx @modelcontextprotocol/server-postgres",
    type: "Database",
    status: "active",
    interceptions_today: 142,
    blocked: 3,
    icon: Database,
  },
  {
    id: "srv-2",
    name: "bash-mcp",
    command: "bastio mcp-proxy -- npx @modelcontextprotocol/server-bash",
    type: "Terminal / Execution",
    status: "active",
    interceptions_today: 89,
    blocked: 12,
    icon: Terminal,
  },
  {
    id: "srv-3",
    name: "filesystem-mcp",
    command: "npx @modelcontextprotocol/server-filesystem /workspace",
    type: "Filesystem",
    status: "active",
    interceptions_today: 412,
    blocked: 0,
    icon: FolderTree,
  },
  {
    id: "srv-4",
    name: "github-tools",
    command: "npx @modelcontextprotocol/server-github",
    type: "API Tool",
    status: "active",
    interceptions_today: 64,
    blocked: 1,
    icon: FileCode,
  },
];

export function McpPage() {
  const [activeTab, setActiveTab] = useState<"live" | "servers" | "sandbox">("live");
  const [connectOpen, setConnectOpen] = useState(false);
  const [searchQuery, setSearchQuery] = useState("");

  // Sandbox state
  const [sandboxTool, setSandboxTool] = useState("bash.run_command");
  const [sandboxPayload, setSandboxPayload] = useState('{\n  "command": "rm -rf / --no-preserve-root"\n}');
  const [simulating, setSimulating] = useState(false);
  const [simulationResult, setSimulationResult] = useState<{
    action: "BLOCKED" | "MASKED" | "ALLOWED";
    reason: string;
    jsonrpc_response: string;
  } | null>(null);

  const runSimulation = () => {
    setSimulating(true);
    setTimeout(() => {
      setSimulating(false);
      const isDestructive =
        sandboxPayload.includes("rm -rf") ||
        sandboxPayload.includes("DROP TABLE") ||
        sandboxPayload.includes("DELETE FROM");
      const hasPII =
        sandboxPayload.includes("ssn") ||
        sandboxPayload.includes("credit_card") ||
        sandboxPayload.includes("password");

      if (isDestructive) {
        setSimulationResult({
          action: "BLOCKED",
          reason: "Destructive shell command or unconstrained mutation detected by Security Engine",
          jsonrpc_response: JSON.stringify(
            {
              jsonrpc: "2.0",
              id: 1,
              error: {
                code: -32600,
                message: "Blocked by Bastio Security Firewall: destructive action violation",
              },
            },
            null,
            2
          ),
        });
      } else if (hasPII) {
        setSimulationResult({
          action: "MASKED",
          reason: "Reversible PII surrogate generated before forwarding to tool process",
          jsonrpc_response: JSON.stringify(
            {
              jsonrpc: "2.0",
              id: 1,
              result: {
                content: [{ type: "text", text: "Successfully sanitized and executed with tokenized parameters" }],
              },
            },
            null,
            2
          ),
        });
      } else {
        setSimulationResult({
          action: "ALLOWED",
          reason: "No threat heuristics triggered. Tool call forwarded directly to child process stdin.",
          jsonrpc_response: JSON.stringify(
            {
              jsonrpc: "2.0",
              id: 1,
              result: { content: [{ type: "text", text: "Command executed cleanly" }] },
            },
            null,
            2
          ),
        });
      }
    }, 400);
  };

  const filteredCalls = mockToolCalls.filter(
    (c) =>
      c.tool_name.toLowerCase().includes(searchQuery.toLowerCase()) ||
      c.client.toLowerCase().includes(searchQuery.toLowerCase()) ||
      c.arguments_preview.toLowerCase().includes(searchQuery.toLowerCase())
  );

  return (
    <div className="space-y-6 pb-12">
      <AdminPageHeader
        eyebrow="Agentic security"
        title="Agent & MCP Tool Firewalls"
        description="Inspect, sanitize, and firewall Model Context Protocol (MCP) tool execution streams for Claude Desktop, Cursor, and autonomous agents."
        actions={
          <Button onClick={() => setConnectOpen(true)} className="gap-1.5 text-xs font-medium">
            <Plus className="size-3.5" />
            Connect Tool Server
          </Button>
        }
      />

      {/* Summary KPI Cards */}
      <AdminSummaryStrip
        items={[
          {
            label: "Protected Tool Servers",
            value: "4 Active",
            detail: "Postgres, Bash, FS, GitHub",
          },
          {
            label: "Tool Calls Inspected",
            value: "1,842",
            detail: "Last 24 hours",
          },
          {
            label: "Destructive Calls Blocked",
            value: "16 Blocked",
            detail: "RCE, SQL drops, file overwrites",
          },
          {
            label: "PII Sanitized",
            value: "94 Masked",
            detail: "Reversible surrogate vault",
          },
        ]}
      />

      {/* Navigation Tabs */}
      <div className="flex gap-2 border-b border-border pb-3">
        <button
          type="button"
          onClick={() => setActiveTab("live")}
          className={cn(
            "flex items-center gap-2 px-3 py-1.5 text-xs font-medium rounded-md transition-colors",
            activeTab === "live"
              ? "bg-accent/10 text-accent border border-accent/20 font-semibold"
              : "text-muted-foreground hover:text-foreground"
          )}
        >
          <ShieldAlert className="size-3.5" />
          Tool Interception Log ({mockToolCalls.length})
        </button>
        <button
          type="button"
          onClick={() => setActiveTab("servers")}
          className={cn(
            "flex items-center gap-2 px-3 py-1.5 text-xs font-medium rounded-md transition-colors",
            activeTab === "servers"
              ? "bg-accent/10 text-accent border border-accent/20 font-semibold"
              : "text-muted-foreground hover:text-foreground"
          )}
        >
          <Server className="size-3.5" />
          Configured MCP Servers ({mockServers.length})
        </button>
        <button
          type="button"
          onClick={() => setActiveTab("sandbox")}
          className={cn(
            "flex items-center gap-2 px-3 py-1.5 text-xs font-medium rounded-md transition-colors",
            activeTab === "sandbox"
              ? "bg-accent/10 text-accent border border-accent/20 font-semibold"
              : "text-muted-foreground hover:text-foreground"
          )}
        >
          <Play className="size-3.5" />
          Tool Call Simulator Sandbox
        </button>
      </div>

      {/* TAB 1: Live Interception Feed */}
      {activeTab === "live" && (
        <div className="space-y-4">
          <div className="flex items-center justify-between gap-4">
            <div className="relative flex-1 max-w-sm">
              <Search className="absolute left-2.5 top-2.5 size-3.5 text-muted-foreground" />
              <Input
                placeholder="Filter tool calls by name, arguments, or client..."
                value={searchQuery}
                onChange={(e) => setSearchQuery(e.target.value)}
                className="pl-8 h-8 text-xs bg-muted/20"
              />
            </div>
            <span className="text-xs text-muted-foreground">
              Inline latency: <strong className="text-foreground font-mono">&lt;2ms</strong> average
            </span>
          </div>

          <div className="border border-border rounded-lg overflow-hidden bg-card">
            <div className="overflow-x-auto">
              <table className="w-full text-left text-xs border-collapse">
                <thead>
                  <tr className="border-b border-border bg-muted/40 text-muted-foreground font-medium">
                    <th className="py-2.5 px-3">Verdict</th>
                    <th className="py-2.5 px-3">Tool Name</th>
                    <th className="py-2.5 px-3">Client</th>
                    <th className="py-2.5 px-3">Arguments Preview</th>
                    <th className="py-2.5 px-3">Latency</th>
                    <th className="py-2.5 px-3">Time</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-border/60">
                  {filteredCalls.map((call) => (
                    <tr key={call.id} className="hover:bg-muted/20 transition-colors">
                      <td className="py-2.5 px-3 whitespace-nowrap">
                        <Badge
                          variant={
                            call.action === "BLOCKED"
                              ? "destructive"
                              : call.action === "MASKED"
                              ? "outline"
                              : "secondary"
                          }
                          className={cn(
                            "h-5 text-[10px] uppercase font-mono tracking-wider font-semibold",
                            call.action === "MASKED" && "text-accent border-accent/30 bg-accent/10"
                          )}
                        >
                          {call.action}
                        </Badge>
                      </td>
                      <td className="py-2.5 px-3 font-mono font-medium text-foreground">
                        {call.tool_name}
                      </td>
                      <td className="py-2.5 px-3 text-muted-foreground">{call.client}</td>
                      <td className="py-2.5 px-3 font-mono text-[11px] text-muted-foreground max-w-md truncate">
                        {call.arguments_preview}
                      </td>
                      <td className="py-2.5 px-3 font-mono text-muted-foreground">{call.duration_ms}ms</td>
                      <td className="py-2.5 px-3 text-muted-foreground whitespace-nowrap">
                        {call.timestamp}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </div>
        </div>
      )}

      {/* TAB 2: Protected MCP Servers */}
      {activeTab === "servers" && (
        <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
          {mockServers.map((srv) => {
            const Icon = srv.icon;
            return (
              <Card key={srv.id} className="border-border bg-card">
                <CardContent className="p-4 space-y-3">
                  <div className="flex items-center justify-between">
                    <div className="flex items-center gap-2.5">
                      <div className="size-8 rounded-lg bg-muted/60 border border-border flex items-center justify-center text-foreground">
                        <Icon className="size-4" />
                      </div>
                      <div>
                        <h4 className="text-xs font-semibold text-foreground">{srv.name}</h4>
                        <span className="text-[11px] text-muted-foreground">{srv.type}</span>
                      </div>
                    </div>
                    <Badge variant="outline" className="text-success border-success/30 bg-success/10 text-[10px]">
                      Protected (bastio mcp-proxy)
                    </Badge>
                  </div>

                  <div className="p-2.5 rounded bg-muted/40 border border-border font-mono text-[11px] text-muted-foreground truncate">
                    {srv.command}
                  </div>

                  <div className="flex items-center justify-between pt-2 border-t border-border/60 text-xs text-muted-foreground">
                    <span>
                      Interceptions today: <strong className="text-foreground">{srv.interceptions_today}</strong>
                    </span>
                    <span>
                      Blocked exploits: <strong className="text-danger">{srv.blocked}</strong>
                    </span>
                  </div>
                </CardContent>
              </Card>
            );
          })}
        </div>
      )}

      {/* TAB 3: Interactive Sandbox */}
      {activeTab === "sandbox" && (
        <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
          <Card className="border-border bg-card">
            <CardContent className="p-5 space-y-4">
              <div className="flex items-center justify-between">
                <h4 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">
                  Simulate Inbound Tool Call (JSON-RPC)
                </h4>
                <Badge variant="secondary" className="text-[10px]">
                  Local Security Engine
                </Badge>
              </div>

              <div>
                <label className="text-xs font-medium text-foreground block mb-1">Tool Identifier</label>
                <Input
                  value={sandboxTool}
                  onChange={(e) => setSandboxTool(e.target.value)}
                  className="font-mono text-xs h-8 bg-muted/20"
                />
              </div>

              <div>
                <label className="text-xs font-medium text-foreground block mb-1">
                  Tool Arguments (JSON Payload)
                </label>
                <textarea
                  value={sandboxPayload}
                  onChange={(e) => setSandboxPayload(e.target.value)}
                  rows={6}
                  className="w-full p-2.5 rounded-md bg-muted/30 border border-border font-mono text-xs text-foreground focus:outline-none focus:ring-1 focus:ring-accent"
                />
              </div>

              <div className="flex gap-2">
                <Button
                  onClick={runSimulation}
                  disabled={simulating}
                  size="sm"
                  className="gap-1.5 text-xs"
                >
                  <Play className="size-3" />
                  {simulating ? "Scanning..." : "Simulate Firewall Verdict"}
                </Button>
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => setSandboxPayload('{\n  "query": "SELECT * FROM customers WHERE id = 123;"\n}')}
                  className="text-xs"
                >
                  Load Clean SQL
                </Button>
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => setSandboxPayload('{\n  "command": "rm -rf /"\n}')}
                  className="text-xs text-danger"
                >
                  Load Destructive Attack
                </Button>
              </div>
            </CardContent>
          </Card>

          <Card className="border-border bg-card">
            <CardContent className="p-5 space-y-4">
              <div className="flex items-center justify-between">
                <h4 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">
                  Firewall Inspection Result
                </h4>
                {simulationResult && (
                  <Badge
                    variant={
                      simulationResult.action === "BLOCKED"
                        ? "destructive"
                        : simulationResult.action === "MASKED"
                        ? "outline"
                        : "secondary"
                    }
                    className="text-[10px] font-mono"
                  >
                    {simulationResult.action}
                  </Badge>
                )}
              </div>

              {simulationResult ? (
                <div className="space-y-3">
                  <div className="p-3 rounded-lg bg-muted/40 border border-border text-xs">
                    <span className="text-muted-foreground font-medium">Policy Rationale: </span>
                    <span className="text-foreground">{simulationResult.reason}</span>
                  </div>

                  <div>
                    <span className="text-[11px] font-mono text-muted-foreground uppercase block mb-1">
                      JSON-RPC Client Output:
                    </span>
                    <pre className="p-3 rounded-lg bg-muted/60 border border-border font-mono text-xs text-foreground overflow-x-auto">
                      {simulationResult.jsonrpc_response}
                    </pre>
                  </div>
                </div>
              ) : (
                <div className="flex flex-col items-center justify-center py-12 text-center text-muted-foreground">
                  <Shield className="size-8 mb-2 opacity-40" />
                  <p className="text-xs">Click "Simulate Firewall Verdict" to test payload inspection.</p>
                </div>
              )}
            </CardContent>
          </Card>
        </div>
      )}

      {/* Connect Dialog */}
      <ConnectMcpDialog open={connectOpen} onOpenChange={setConnectOpen} />
    </div>
  );
}
