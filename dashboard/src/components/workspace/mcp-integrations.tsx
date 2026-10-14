import {
  ResourceSharingButton,
  SharingPicker,
  sharingLabel,
  everyoneSharing,
  type ResourceSharing,
} from "./sharing-controls";
import { useState } from "react";
import { MCPSecurityError, mcpRuleLabel, mcpCategoryLabel } from "./mcp-security";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Plug, ShieldCheck, Loader2, Pencil, Trash2 } from "lucide-react";
import { http, unwrap } from "@/api/typed";
import type { components } from "@/api/schema";
import { workspaceApi } from "./types";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
} from "@/components/ui/dialog";

type Connection = components["schemas"]["WorkspaceMCPConnection"];
type ConnectionInput = components["schemas"]["WorkspaceMCPInput"];
type Tool = components["schemas"]["WorkspaceMCPTool"];
const integrationKey = ["workspace-mcp-integrations"];

export function MCPIntegrationsButton({
  onUseResult,
  disabled,
}: {
  onUseResult: (text: string) => void;
  disabled?: boolean;
}) {
  const [open, setOpen] = useState(false);
  return (
    <>
      <Button
        type="button"
        variant="ghost"
        size="sm"
        disabled={disabled}
        onClick={() => setOpen(true)}
        aria-label="Open MCP integrations"
      >
        <Plug className="size-4" /> Integrations
      </Button>
      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent className="max-h-[90vh] max-w-3xl overflow-y-auto">
          <DialogHeader>
            <DialogTitle>Workspace integrations</DialogTitle>
            <DialogDescription>
              Connect remote MCP tools, review a request, and use protected
              results in chat.
            </DialogDescription>
          </DialogHeader>
          {open && (
            <MCPIntegrations
              onUseResult={(text) => {
                onUseResult(text);
                setOpen(false);
              }}
            />
          )}
        </DialogContent>
      </Dialog>
    </>
  );
}

export function MCPIntegrations({
  onUseResult,
}: {
  onUseResult?: (text: string) => void;
}) {
  const qc = useQueryClient();
  const connections = useQuery({
    queryKey: integrationKey,
    queryFn: () => unwrap(http.GET("/v1/workspace/mcp")),
    gcTime: 0,
    staleTime: 0,
  });
  const [editing, setEditing] = useState<Connection | null>(null);
  const [showForm, setShowForm] = useState(false);
  const [oauthConnection, setOAuthConnection] = useState<Connection | null>(
    null,
  );
  const [selected, setSelected] = useState<Connection | null>(null);
  const refresh = () => qc.invalidateQueries({ queryKey: integrationKey });
  const policy = useMutation({
    mutationFn: (allow_personal: boolean) =>
      unwrap(
        http.PUT("/v1/workspace/mcp/policy", { body: { allow_personal } }),
      ),
    onSuccess: refresh,
  });
  const remove = useMutation({
    mutationFn: (id: string) =>
      unwrap(
        http.DELETE("/v1/workspace/mcp/connections/{id}", {
          params: { path: { id } },
        }),
      ),
    onSuccess: () => {
      setSelected(null);
      refresh();
    },
  });
  if (connections.isPending)
    return (
      <p className="py-4 text-xs text-muted-foreground">
        Loading integrations…
      </p>
    );
  if (connections.isError)
    return (
      <p role="alert" className="text-xs text-destructive">
        {errorMessage(connections.error)}
      </p>
    );
  const data = connections.data;
  const canAdd = data.allow_personal || data.can_manage;
  return (
    <div className="space-y-4">
      <div className="rounded-lg border border-border bg-muted/20 p-3 text-xs leading-relaxed text-muted-foreground">
        <div className="mb-1 flex items-center gap-2 font-medium text-foreground">
          <ShieldCheck className="size-4" /> Protected tool traffic
        </div>
        Workspace policies inspect arguments before forwarding and inspect
        returned text before you see or use it. Mask and Tokenize rewrite
        detected values; Warn and Log only allow originals through. Tool
        arguments and results are not stored in the integration audit log.
      </div>
      {data.can_manage && (
        <label className="flex items-center gap-2 text-xs">
          <input
            type="checkbox"
            checked={data.allow_personal}
            disabled={policy.isPending}
            onChange={(e) => policy.mutate(e.target.checked)}
          />{" "}
          Allow employees to add and use personal MCP connections
        </label>
      )}
      {!data.allow_personal && (
        <p className="text-xs text-muted-foreground">
          Personal connections are disabled by your workspace. Shared
          integrations remain available.
        </p>
      )}
      {(policy.isError || remove.isError) && (
        <p role="alert" className="text-xs text-destructive">
          {errorMessage(policy.error ?? remove.error)}
        </p>
      )}
      <div className="flex items-center justify-between">
        <h3 className="text-sm font-medium">Connected tools</h3>
        {canAdd && (
          <Button
            size="sm"
            onClick={() => {
              setEditing(null);
              setShowForm(true);
            }}
          >
            Add integration
          </Button>
        )}
      </div>
      {data.connections.length === 0 && (
        <p className="rounded-lg border border-dashed border-border p-5 text-xs text-muted-foreground">
          No integrations yet. Add an HTTPS Streamable HTTP MCP endpoint, such
          as a tool provider’s /mcp URL.
        </p>
      )}
      <div className="space-y-2">
        {data.connections.map((c) => {
          const canEdit = c.scope === "personal" || data.can_manage;
          const disabled = c.scope === "personal" && !data.allow_personal;
          return (
            <div
              key={c.id}
              className="flex items-center gap-3 rounded-lg border border-border bg-card p-3"
            >
              <Plug className="size-4 shrink-0 text-muted-foreground" />
              <div className="min-w-0 flex-1">
                <p className="truncate text-xs font-medium">
                  {c.name}{" "}
                  <span className="ml-1 text-[10px] font-normal text-muted-foreground">
                    {c.scope === "workspace"
                      ? c.sharing
                        ? sharingLabel(c.sharing)
                        : "Shared with you"
                      : "Personal · only you"}
                  </span>
                </p>
                <p className="truncate text-[10px] text-muted-foreground">
                  {c.endpoint}
                  {c.auth_type === "oauth"
                    ? ` · OAuth ${c.oauth_connected ? "connected" : "needs connecting"}`
                    : ""}
                </p>
              </div>
              <Button
                variant="outline"
                size="sm"
                disabled={disabled}
                onClick={() => {
                  setSelected(c);
                  setShowForm(false);
                }}
              >
                Tools
              </Button>
              {canEdit && c.auth_type === "oauth" ? (
                <Button
                  variant="outline"
                  size="sm"
                  disabled={disabled}
                  onClick={() => setOAuthConnection(c)}
                >
                  {c.oauth_connected ? "Manage account" : "Connect account"}
                </Button>
              ) : null}
              {data.can_manage && c.scope === "workspace" && (
                <ResourceSharingButton
                  type="mcp"
                  id={c.id}
                  name={c.name}
                  sharing={c.sharing}
                />
              )}
              {canEdit && (
                <>
                  <Button
                    variant="ghost"
                    size="icon-xs"
                    disabled={disabled}
                    aria-label={`Edit ${c.name}`}
                    onClick={() => {
                      setEditing(c);
                      setShowForm(true);
                    }}
                  >
                    <Pencil className="size-3" />
                  </Button>
                  <Button
                    variant="ghost"
                    size="icon-xs"
                    disabled={remove.isPending}
                    aria-label={`Remove ${c.name}`}
                    onClick={() => remove.mutate(c.id)}
                  >
                    <Trash2 className="size-3" />
                  </Button>
                </>
              )}
            </div>
          );
        })}
      </div>
      {oauthConnection ? (
        <OAuthConnectionDialog
          connection={oauthConnection}
          onClose={() => {
            setOAuthConnection(null);
            refresh();
          }}
        />
      ) : null}
      {showForm && (
        <ConnectionForm
          key={editing?.id ?? "new"}
          connection={editing}
          canShare={data.can_manage}
          allowPersonal={data.allow_personal}
          onCancel={() => setShowForm(false)}
          onSaved={() => {
            setShowForm(false);
            setSelected(null);
            refresh();
          }}
        />
      )}
      {selected && !showForm && (
        <ToolRunner
          key={selected.id}
          connection={selected}
          canManage={data.can_manage}
          onUseResult={onUseResult}
        />
      )}
    </div>
  );
}

function ConnectionForm({
  connection,
  canShare,
  allowPersonal,
  onCancel,
  onSaved,
}: {
  connection: Connection | null;
  canShare: boolean;
  allowPersonal: boolean;
  onCancel: () => void;
  onSaved: () => void;
}) {
  const [name, setName] = useState(connection?.name ?? "");
  const [endpoint, setEndpoint] = useState(connection?.endpoint ?? "");
  const [scope, setScope] = useState<"personal" | "workspace">(
    connection?.scope ?? (allowPersonal ? "personal" : "workspace"),
  );
  const [sharing, setSharing] = useState<ResourceSharing>(
    connection?.sharing ?? everyoneSharing(),
  );
  const [authType, setAuthType] = useState<"none" | "bearer" | "oauth">(
    connection?.auth_type ?? (connection?.has_credential ? "bearer" : "none"),
  );
  const [clientID, setClientID] = useState("");
  const [clientSecret, setClientSecret] = useState("");
  const [token, setToken] = useState("");
  const [clearToken, setClearToken] = useState(false);
  const [allowed, setAllowed] = useState(
    connection?.allowed_tools.join(", ") ?? "",
  );
  const save = useMutation({
    mutationFn: async () => {
      const body: ConnectionInput = {
        name,
        endpoint,
        scope,
        ...(scope === "workspace" ? { sharing } : {}),
        allowed_tools: allowed
          .split(",")
          .map((s) => s.trim())
          .filter(Boolean),
        auth_type: authType,
        ...(authType === "bearer" && (token || clearToken)
          ? { credential: token }
          : {}),
        ...(authType === "oauth" && (clientID || clientSecret)
          ? { oauth_client_id: clientID, oauth_client_secret: clientSecret }
          : {}),
      };
      return connection
        ? unwrap(
            http.PUT("/v1/workspace/mcp/connections/{id}", {
              params: { path: { id: connection.id } },
              body,
            }),
          )
        : unwrap(http.POST("/v1/workspace/mcp/connections", { body }));
    },
    onSuccess: () => {
      setToken("");
      onSaved();
    },
  });
  return (
    <form
      className="space-y-3 rounded-lg border border-border bg-card p-4"
      onSubmit={(e) => {
        e.preventDefault();
        save.mutate();
      }}
    >
      <h3 className="text-xs font-medium">
        {connection ? "Edit integration" : "Add MCP integration"}
      </h3>
      <label className="block space-y-1 text-xs">
        Name
        <Input
          value={name}
          maxLength={100}
          required
          onChange={(e) => setName(e.target.value)}
          placeholder="Company search"
        />
      </label>
      <label className="block space-y-1 text-xs">
        MCP endpoint
        <Input
          type="url"
          value={endpoint}
          required
          onChange={(e) => setEndpoint(e.target.value)}
          placeholder="https://tools.example.com/mcp"
        />
      </label>
      <label className="block space-y-1 text-xs">
        Access
        <select
          className="h-8 w-full rounded-md border border-input bg-background px-2 text-xs"
          value={scope}
          onChange={(e) => setScope(e.target.value as "personal" | "workspace")}
        >
          {allowPersonal && (
            <option value="personal">Personal — only me</option>
          )}
          {canShare && (
            <option value="workspace">
              Shared — choose workspace recipients
            </option>
          )}
        </select>
      </label>
      {scope === "workspace" && (
        <SharingPicker
          value={sharing}
          onChange={setSharing}
          disabled={save.isPending}
        />
      )}
      {scope === "workspace" && (
        <p className="text-[11px] leading-relaxed text-muted-foreground">
          Selected members can run the allowed tools using this connection’s
          credential. The credential stays encrypted on the server and is never
          shown to members.
        </p>
      )}
      <label className="block space-y-1 text-xs">
        Authentication
        <select
          value={authType}
          onChange={(e) => setAuthType(e.target.value as typeof authType)}
          className="h-8 w-full cursor-pointer rounded-md border border-input bg-background px-2 text-xs"
        >
          <option value="none">No authentication</option>
          <option value="bearer">Bearer token</option>
          <option value="oauth">OAuth — connect an account</option>
        </select>
      </label>
      {authType === "oauth" ? (
        <div className="space-y-2 rounded-md border border-border p-3 text-xs">
          <p className="leading-relaxed text-muted-foreground">
            Save this integration, then choose Connect account. Bastio discovers
            the service's OAuth settings and keeps access tokens encrypted on
            the server.
          </p>
          <details>
            <summary className="cursor-pointer font-medium">
              Advanced: pre-registered OAuth client
            </summary>
            <div className="mt-2 space-y-2">
              <label className="block">
                Client ID
                <Input
                  value={clientID}
                  onChange={(e) => setClientID(e.target.value)}
                  placeholder="Optional; automatic registration when supported"
                  autoComplete="off"
                />
              </label>
              <label className="block">
                Client secret (if required)
                <Input
                  type="password"
                  value={clientSecret}
                  onChange={(e) => setClientSecret(e.target.value)}
                  autoComplete="off"
                />
              </label>
              <p className="text-muted-foreground">
                Leave both blank to keep stored settings. Replacing a client
                requires its secret again. The callback URL is shown when
                connecting.
              </p>
            </div>
          </details>
        </div>
      ) : null}
      {authType === "bearer" ? (
        <label className="block space-y-1 text-xs">
          Bearer token
          <Input
            type="password"
            disabled={clearToken}
            autoComplete="off"
            value={token}
            maxLength={8192}
            onChange={(e) => setToken(e.target.value)}
            placeholder={
              connection?.has_credential
                ? "Leave blank to keep stored token"
                : "Provided by your MCP service"
            }
          />
        </label>
      ) : null}
      {authType === "bearer" && connection?.has_credential && (
        <label className="flex items-center gap-2 text-[11px]">
          <input
            type="checkbox"
            checked={clearToken}
            onChange={(e) => {
              setClearToken(e.target.checked);
              if (e.target.checked) setToken("");
            }}
          />{" "}
          Clear the stored token
        </label>
      )}
      <label className="block space-y-1 text-xs">
        Allowed tools (optional)
        <Input
          value={allowed}
          onChange={(e) => setAllowed(e.target.value)}
          placeholder="search, read_document"
        />
      </label>
      <p className="text-[11px] text-muted-foreground">
        Leave the allowlist empty to expose all permitted discovered tools.
        Changing the endpoint clears its stored token unless you enter a token
        again. Put credentials in the token field, never in the endpoint URL.
        Public HTTPS Streamable HTTP endpoints support OAuth, bearer tokens and
        no authentication. Changing authentication or the endpoint disconnects
        the stored account. Local stdio servers are not supported in the cloud
        workspace.
      </p>
      {save.isError && (
        <p role="alert" className="text-xs text-destructive">
          {errorMessage(save.error)}
        </p>
      )}
      <div className="flex justify-end gap-2">
        <Button type="button" variant="ghost" size="sm" onClick={onCancel}>
          Cancel
        </Button>
        <Button type="submit" size="sm" disabled={save.isPending}>
          {save.isPending ? "Saving…" : "Save integration"}
        </Button>
      </div>
    </form>
  );
}

function ToolRunner({
  connection,
  canManage,
  onUseResult,
}: {
  connection: Connection;
  canManage: boolean;
  onUseResult?: (text: string) => void;
}) {
  const [tool, setTool] = useState<Tool | null>(null);
  const [argumentsText, setArgumentsText] = useState("{}");
  const [advanced, setAdvanced] = useState(false);
  const [inputError, setInputError] = useState("");
  const tools = useQuery({
    queryKey: ["workspace-mcp-tools", connection.id],
    queryFn: () =>
      unwrap(
        http.GET("/v1/workspace/mcp/connections/{id}/tools", {
          params: { path: { id: connection.id } },
        }),
      ),
    gcTime: 0,
  });
  const call = useMutation({
    mutationFn: async (argumentsObject: Record<string, unknown>) => {
      const { data, error, response } = await http.POST("/v1/workspace/mcp/connections/{id}/call", {
        params: { path: { id: connection.id } },
        body: { name: tool!.name, arguments: argumentsObject },
      });
      if (!response.ok) {
        const failure = MCPSecurityError.from(error);
        if (failure) throw failure;
        throw new Error(typeof error === "object" && error !== null && "error" in error ? String(error.error) : "Integration request failed.");
      }
      return data!;
    },
  });
  const properties = objectValue(tool?.inputSchema.properties) ?? {};
  const simpleFields = Object.values(properties).every((v) =>
    ["string", "number", "integer", "boolean"].includes(
      String(objectValue(v)?.type),
    ),
  );
  const args = (() => {
    try {
      return objectValue(JSON.parse(argumentsText));
    } catch {
      return null;
    }
  })();
  const updateField = (key: string, value: string | number | boolean) =>
    setArgumentsText(JSON.stringify({ ...args, [key]: value }, null, 2));
  const run = () => {
    setInputError("");
    if (!args) {
      setInputError("Arguments must be a JSON object.");
      return;
    }
    call.mutate(args);
  };
  return (
    <section className="space-y-3 rounded-lg border border-border bg-card p-4">
      <h3 className="text-xs font-medium">Tools · {connection.name}</h3>
      {tools.isPending && (
        <p className="flex items-center gap-2 text-xs text-muted-foreground">
          <Loader2 className="size-3 animate-spin" /> Discovering protected
          tools…
        </p>
      )}
      {tools.isError && (
        <p role="alert" className="text-xs text-destructive">
          {errorMessage(tools.error)}
        </p>
      )}
      {tools.data && tools.data.tools.length === 0 && (
        <p className="text-xs text-muted-foreground">
          No tools are available under the current connection and security
          policies.
        </p>
      )}
      {tools.data && tools.data.tools.length > 0 && (
        <label className="block space-y-1 text-xs">
          Tool
          <select
            className="h-8 w-full rounded-md border border-input bg-background px-2 text-xs"
            value={tool?.name ?? ""}
            onChange={(e) => {
              const next =
                tools.data.tools.find((t) => t.name === e.target.value) ?? null;
              setTool(next);
              setArgumentsText(defaultArguments(next));
              setAdvanced(false);
              call.reset();
              setInputError("");
            }}
          >
            <option value="" disabled>
              Select a tool
            </option>
            {tools.data.tools.map((t) => (
              <option key={t.name} value={t.name}>
                {t.name}
              </option>
            ))}
          </select>
        </label>
      )}
      {tool && (
        <>
          <p className="whitespace-pre-wrap text-[11px] leading-relaxed text-muted-foreground">
            {tool.description}
          </p>
          {simpleFields && !advanced ? (
            <div className="space-y-2">
              {Object.entries(properties).map(([key, value]) => {
                const field = objectValue(value);
                const type = String(field?.type);
                return (
                  <label key={key} className="block space-y-1 text-xs">
                    {key}
                    {Array.isArray(tool.inputSchema.required) &&
                    tool.inputSchema.required.includes(key)
                      ? " *"
                      : ""}
                    {type === "boolean" ? (
                      <input
                        type="checkbox"
                        className="ml-2"
                        checked={args?.[key] === true}
                        onChange={(e) => updateField(key, e.target.checked)}
                      />
                    ) : (
                      <Input
                        type={type === "string" ? "text" : "number"}
                        value={String(args?.[key] ?? "")}
                        onChange={(e) =>
                          updateField(
                            key,
                            type === "string"
                              ? e.target.value
                              : Number(e.target.value),
                          )
                        }
                      />
                    )}
                    {typeof field?.description === "string" && (
                      <span className="text-[10px] text-muted-foreground">
                        {field.description}
                      </span>
                    )}
                  </label>
                );
              })}
            </div>
          ) : (
            <label className="block space-y-1 text-xs">
              JSON arguments
              <textarea
                className="min-h-32 w-full rounded-md border border-input bg-background p-3 font-mono text-xs"
                value={argumentsText}
                onChange={(e) => setArgumentsText(e.target.value)}
                spellCheck={false}
              />
            </label>
          )}
          {simpleFields && (
            <Button
              type="button"
              size="sm"
              variant="ghost"
              onClick={() => setAdvanced(!advanced)}
            >
              {advanced ? "Use fields" : "Edit JSON arguments"}
            </Button>
          )}
          <p className="text-[11px] leading-relaxed text-muted-foreground">
            Run sends these arguments to {new URL(connection.endpoint).hostname}{" "}
            after security checks. Review them first: this tool can act on your
            connected service. Requests are not automatically retried.
          </p>
          {inputError && (
            <p role="alert" className="text-xs text-destructive">
              {inputError}
            </p>
          )}
          <Button
            size="sm"
            type="button"
            disabled={call.isPending}
            onClick={run}
          >
            {call.isPending ? "Running tool…" : "Run tool"}
          </Button>
        </>
      )}
      {call.isError && (
        call.error instanceof MCPSecurityError ? (
          <div role="alert" className="space-y-2 rounded-lg border border-border bg-muted/20 p-3 text-xs">
            <p className="font-medium text-destructive">
              {call.error.stage === "result" ? "Tool ran · result withheld" : "Request withheld · tool did not run"}
            </p>
            <p className="leading-relaxed">{call.error.reason}</p>
            {call.error.blockingDetectors.length > 0 && <p className="font-medium">Blocked by: {call.error.blockingDetectors.map(mcpCategoryLabel).join(", ")}</p>}
            {call.error.categories.length > 0 && (
              <p>Security findings: {call.error.categories.map(mcpCategoryLabel).join(", ")}</p>
            )}
            {call.error.rules.length > 0 && (
              <ul className="space-y-1 pl-4 list-disc text-muted-foreground">
                {call.error.rules.map((rule) => <li key={rule}>{mcpRuleLabel(rule)}</li>)}
              </ul>
            )}
            <p className="leading-relaxed text-muted-foreground">
              {call.error.stage === "result" ? "The remote action was not undone. Check the service before running the tool again." : "These arguments were not sent to the connected service."}
              {canManage ? " Review the active policy in Security Center if you suspect a false positive." : " Ask your workspace administrator to review the active security policy if you suspect a false positive."}
            </p>
            {canManage && <a href={`${import.meta.env.VITE_DASHBOARD_URL ?? ""}/security-settings`} className="inline-flex cursor-pointer underline underline-offset-4 hover:text-foreground">Review security policy</a>}
          </div>
        ) : <p role="alert" className="text-xs text-destructive">{errorMessage(call.error)}</p>
      )}
      {call.data && (
        <div className="space-y-2 border-t border-border pt-3">
          <p className="text-xs font-medium">
            {call.data.is_error ? "Tool returned an error" : "Protected result"}
          </p>
          <pre className="max-h-64 overflow-auto whitespace-pre-wrap break-words rounded-md bg-muted/30 p-3 text-xs">
            {call.data.text}
          </pre>
          {call.data.categories.length > 0 && (
            <p className="text-[10px] text-muted-foreground">
              Security findings: {[...new Set(call.data.categories)].join(", ")}
            </p>
          )}
          {onUseResult && !call.data.is_error && (
            <Button
              size="sm"
              onClick={() =>
                onUseResult(
                  `[MCP tool result: ${tool?.name ?? "tool"}]\n${call.data.text}`,
                )
              }
            >
              Insert result into chat draft
            </Button>
          )}
        </div>
      )}
    </section>
  );
}

function objectValue(value: unknown): Record<string, unknown> | null {
  return typeof value === "object" && value !== null && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : null;
}
function defaultArguments(tool: Tool | null) {
  const values: Record<string, unknown> = {};
  for (const [key, value] of Object.entries(
    objectValue(tool?.inputSchema.properties) ?? {},
  )) {
    const field = objectValue(value);
    if (
      Array.isArray(tool?.inputSchema.required) &&
      tool.inputSchema.required.includes(key)
    )
      values[key] =
        field?.default ??
        (field?.type === "boolean"
          ? false
          : field?.type === "number" || field?.type === "integer"
            ? 0
            : field?.type === "object"
              ? {}
              : field?.type === "array"
                ? []
                : "");
  }
  return JSON.stringify(values, null, 2);
}
function errorMessage(error: unknown) {
  return error instanceof Error ? error.message : "Integration request failed.";
}

function OAuthConnectionDialog({
  connection,
  onClose,
}: {
  connection: Connection;
  onClose: () => void;
}) {
  const discovery = useQuery({
    queryKey: ["workspace-mcp-oauth", connection.id],
    queryFn: () => workspaceApi.discoverMCPOAuth(connection.id),
    retry: false,
    gcTime: 0,
  });
  const authorize = useMutation({
    mutationFn: () =>
      workspaceApi.startMCPOAuth(
        connection.id,
        discovery.data!.discovery_stamp,
        window.location.pathname.startsWith("/workspace")
          ? "admin"
          : "employee",
      ),
  });
  const disconnect = useMutation({
    mutationFn: () => workspaceApi.disconnectMCPOAuth(connection.id),
    onSuccess: onClose,
  });
  return (
    <Dialog open onOpenChange={(v) => !v && onClose()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Connect {connection.name}</DialogTitle>
          <DialogDescription>
            Authorize this service to connect to Bastio.{" "}
            {connection.scope === "workspace"
              ? "Members in the integration's sharing audience can use this account's permitted tools."
              : "This account connection is available only to you."}
          </DialogDescription>
        </DialogHeader>
        {discovery.isPending ? (
          <p className="flex items-center gap-2 text-xs text-muted-foreground">
            <Loader2 className="size-4 animate-spin" />
            Discovering the authorization service…
          </p>
        ) : null}
        {discovery.data ? (
          <div className="space-y-3 text-xs">
            <div className="rounded-lg border border-border p-3">
              <p className="font-medium">Authorization service</p>
              <p className="mt-1 break-all text-muted-foreground">
                {discovery.data.issuer}
              </p>
              <p className="mt-3 font-medium">MCP resource</p>
              <p className="mt-1 break-all text-muted-foreground">
                {discovery.data.resource}
              </p>
              <p className="mt-3 font-medium">Requested scopes</p>
              <p className="mt-1 break-all text-muted-foreground">
                {discovery.data.scopes.join(", ") ||
                  "The service determines access during authorization."}
              </p>
            </div>
            <details>
              <summary className="cursor-pointer text-muted-foreground">
                Callback URL for pre-registered clients
              </summary>
              <code className="mt-2 block break-all">
                {discovery.data.redirect_uri}
              </code>
            </details>
            {authorize.data ? (
              <>
                <p className="text-muted-foreground">
                  Continue in a new tab to sign in and approve access. Return
                  here and close this dialog to refresh your connection.
                </p>
                <a
                  className="inline-flex cursor-pointer items-center rounded-md bg-primary px-3 py-2 text-xs font-medium text-primary-foreground hover:opacity-90"
                  href={authorize.data.authorization_url}
                  target="_blank"
                  rel="noopener noreferrer"
                >
                  Continue to service ↗
                </a>
              </>
            ) : (
              <Button
                disabled={authorize.isPending}
                onClick={() => authorize.mutate()}
              >
                {authorize.isPending ? (
                  <Loader2 className="size-3 animate-spin" />
                ) : null}
                Prepare secure connection
              </Button>
            )}
          </div>
        ) : null}
        {[discovery.error, authorize.error, disconnect.error]
          .filter(Boolean)
          .map((e, i) => (
            <p key={i} role="alert" className="text-xs text-destructive">
              {errorMessage(e)}
            </p>
          ))}
        <div className="flex justify-end gap-2">
          {connection.oauth_connected ? (
            <Button
              variant="outline"
              disabled={disconnect.isPending}
              onClick={() => disconnect.mutate()}
            >
              Disconnect account
            </Button>
          ) : null}
          <Button variant="outline" onClick={onClose}>
            Close & refresh
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  );
}
