import { MCPHistorySettings } from "./mcp-history-settings";
import {
  ResourceSharingButton,
  SharingPicker,
  sharingLabel,
  everyoneSharing,
  type ResourceSharing,
} from "./sharing-controls";
import { useId, useState } from "react";
import {
  MCPSecurityError,
  mcpRuleLabel,
  mcpCategoryLabel,
} from "./mcp-security";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Plug,
  Loader2,
  Pencil,
  Trash2,
  ExternalLink,
} from "lucide-react";
import { http, unwrap } from "@/api/typed";
import type { components } from "@/api/schema";
import {
  AdminPanel,
  AdminUnderlineNav,
  FieldLabel,
  MonoValue,
} from "@/components/admin/admin-primitives";
import { cn } from "@/lib/utils";
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
  temporary = false,
  conversationId,
}: {
  onUseResult: (text: string) => void;
  disabled?: boolean;
  temporary?: boolean;
  conversationId?: string | null;
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
        <Plug className="size-4" aria-hidden="true" /> Integrations
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
              temporary={temporary}
              conversationId={conversationId}
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
  temporary = false,
  conversationId,
}: {
  onUseResult?: (text: string) => void;
  temporary?: boolean;
  conversationId?: string | null;
}) {
  const qc = useQueryClient();
  const allowPersonalId = useId();
  const connections = useQuery({
    queryKey: integrationKey,
    queryFn: () => unwrap(http.GET("/v1/workspace/mcp")),
    gcTime: 0,
    staleTime: 0,
  });
  const [activeTab, setActiveTab] = useState<"tools" | "history">("tools");
  const [editing, setEditing] = useState<Connection | null>(null);
  const [showForm, setShowForm] = useState(false);
  const [oauthConnection, setOAuthConnection] = useState<Connection | null>(
    null,
  );
  const [selected, setSelected] = useState<Connection | null>(null);
  const [historyVisited, setHistoryVisited] = useState(false);
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
      <p
        role="alert"
        className="border-l-2 border-destructive pl-3.5 text-xs text-destructive"
      >
        {errorMessage(connections.error)}
      </p>
    );

  const data = connections.data;
  const canAdd = data.allow_personal || data.can_manage;

  const tools = (
    <div className="space-y-6">
      <div className="border-l-2 border-border-subtle pl-3.5 text-xs leading-relaxed text-muted-foreground">
        <p className="font-medium text-foreground">Protected tool traffic</p>
        <p className="mt-0.5">
          Workspace policies inspect arguments before forwarding and inspect
          returned text before you see or use it. Mask and Tokenize rewrite
          detected values; Warn and Log only allow originals through. Tool
          history is controlled by workspace admins. The integration audit log
          records decisions only. Tool runs appear in Observe → Traces with
          timing and security decisions.
        </p>
      </div>

      {(policy.isError || remove.isError) && (
        <p
          role="alert"
          className="border-l-2 border-destructive pl-3.5 text-xs text-destructive"
        >
          {errorMessage(policy.error ?? remove.error)}
        </p>
      )}

      <AdminPanel
        title="Connected tools"
        description={
          !data.allow_personal
            ? "Personal connections are disabled by your workspace. Shared integrations remain available."
            : "Remote HTTPS Streamable HTTP MCP endpoints governed by Bastio policies."
        }
        action={
          canAdd ? (
            <Button
              type="button"
              size="sm"
              className="h-8 text-xs"
              onClick={() => {
                setEditing(null);
                setShowForm(true);
              }}
            >
              Add integration
            </Button>
          ) : undefined
        }
        contentClassName="py-0"
      >
        {data.can_manage && (
          <div className="border-b border-border-subtle py-3">
            <label
              htmlFor={allowPersonalId}
              className="flex cursor-pointer items-center gap-2.5 text-xs text-foreground"
            >
              <input
                id={allowPersonalId}
                type="checkbox"
                className="accent-foreground"
                checked={data.allow_personal}
                disabled={policy.isPending}
                onChange={(e) => policy.mutate(e.target.checked)}
              />
              <span>
                Allow employees to add and use personal MCP connections
              </span>
            </label>
          </div>
        )}

        {data.connections.length === 0 ? (
          <p className="border-b border-border-subtle py-8 text-xs text-muted-foreground">
            No integrations yet. Add an HTTPS Streamable HTTP MCP endpoint, such
            as a tool provider’s /mcp URL.
          </p>
        ) : (
          <div className="divide-y divide-border-subtle border-b border-border-subtle">
            {data.connections.map((c) => {
              const canEdit = c.scope === "personal" || data.can_manage;
              const disabled = c.scope === "personal" && !data.allow_personal;
              const needsOAuth = c.auth_type === "oauth" && !c.oauth_connected;
              return (
                <div
                  key={c.id}
                  className="flex flex-wrap items-center gap-3 py-3 transition-colors hover:bg-surface-1/50 sm:flex-nowrap"
                >
                  <div className="min-w-0 flex-1">
                    <div className="flex flex-wrap items-center gap-2">
                      <span
                        aria-hidden="true"
                        className={cn(
                          "size-1.5 shrink-0 rounded-full",
                          disabled
                            ? "bg-muted-foreground/50"
                            : needsOAuth
                              ? "bg-warn"
                              : "bg-success",
                        )}
                      />
                      <span className="truncate text-xs font-medium text-foreground">
                        {c.name}
                      </span>
                      {c.history_mode === "sanitized" ? (
                        <span className="text-[11px] text-muted-foreground">
                          Sanitized history enabled
                        </span>
                      ) : null}
                      <span className="text-[11px] text-muted-foreground">
                        {c.scope === "workspace"
                          ? c.sharing
                            ? sharingLabel(c.sharing)
                            : "Shared with you"
                          : "Personal · only you"}
                      </span>
                    </div>
                    <p className="mt-0.5 truncate pl-3.5 font-mono text-[11px] text-muted-foreground">
                      {c.endpoint}
                      {c.auth_type === "oauth"
                        ? ` · OAuth ${c.oauth_connected ? "connected" : "needs connecting"}`
                        : ""}
                    </p>
                  </div>
                  <div className="flex shrink-0 flex-wrap items-center gap-1.5">
                    <Button
                      type="button"
                      variant="outline"
                      size="sm"
                      className="h-7 text-xs"
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
                        type="button"
                        variant="outline"
                        size="sm"
                        className="h-7 text-xs"
                        disabled={disabled}
                        onClick={() => setOAuthConnection(c)}
                      >
                        {c.oauth_connected
                          ? "Manage account"
                          : "Connect account"}
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
                          type="button"
                          variant="ghost"
                          size="icon-xs"
                          disabled={disabled}
                          aria-label={`Edit ${c.name}`}
                          onClick={() => {
                            setEditing(c);
                            setShowForm(true);
                          }}
                        >
                          <Pencil className="size-3" aria-hidden="true" />
                        </Button>
                        <Button
                          type="button"
                          variant="ghost"
                          size="icon-xs"
                          disabled={remove.isPending}
                          aria-label={`Remove ${c.name}`}
                          onClick={() => remove.mutate(c.id)}
                        >
                          <Trash2 className="size-3" aria-hidden="true" />
                        </Button>
                      </>
                    )}
                  </div>
                </div>
              );
            })}
          </div>
        )}
      </AdminPanel>

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
          temporary={temporary}
          conversationId={conversationId}
          key={selected.id}
          connection={selected}
          canManage={data.can_manage}
          onUseResult={onUseResult}
        />
      )}
    </div>
  );

  if (!data.can_manage) return tools;

  return (
    <div>
      <AdminUnderlineNav
        ariaLabel="Integration sections"
        value={activeTab}
        onChange={(next) => {
          setActiveTab(next);
          if (next === "history") setHistoryVisited(true);
        }}
        items={[
          {
            id: "tools",
            label: "Connected tools",
            count: data.connections.length,
          },
          {
            id: "history",
            label: "Call history settings",
          },
        ]}
      />
      <div
        hidden={activeTab !== "tools"}
        className={activeTab !== "tools" ? "hidden" : undefined}
      >
        {tools}
      </div>
      {(activeTab === "history" || historyVisited) && (
        <div
          hidden={activeTab !== "history"}
          className={activeTab !== "history" ? "hidden" : undefined}
        >
          <MCPHistorySettings />
        </div>
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
  const formId = useId();
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
    <AdminPanel
      title={connection ? "Edit integration" : "Add MCP integration"}
      description="Configure endpoint access, authentication, and exposed tool names."
    >
      <form
        className="space-y-4 border-b border-border-subtle pb-4"
        onSubmit={(e) => {
          e.preventDefault();
          save.mutate();
        }}
      >
        <div>
          <FieldLabel htmlFor={`${formId}-name`}>Name</FieldLabel>
          <Input
            id={`${formId}-name`}
            value={name}
            maxLength={100}
            required
            onChange={(e) => setName(e.target.value)}
            placeholder="Company search"
          />
        </div>

        <div>
          <FieldLabel htmlFor={`${formId}-endpoint`}>MCP endpoint</FieldLabel>
          <Input
            id={`${formId}-endpoint`}
            type="url"
            value={endpoint}
            required
            autoCorrect="off"
            autoCapitalize="none"
            spellCheck={false}
            onChange={(e) => setEndpoint(e.target.value)}
            placeholder="https://tools.example.com/mcp"
            className="font-mono text-xs"
          />
        </div>

        <div>
          <FieldLabel htmlFor={`${formId}-scope`}>Access</FieldLabel>
          <select
            id={`${formId}-scope`}
            className="h-8 w-full cursor-pointer rounded-md border border-input bg-background px-2.5 text-xs text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
            value={scope}
            onChange={(e) =>
              setScope(e.target.value as "personal" | "workspace")
            }
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
        </div>

        {scope === "workspace" && (
          <div className="space-y-2.5">
            <SharingPicker
              value={sharing}
              onChange={setSharing}
              disabled={save.isPending}
            />
            <p className="border-l-2 border-border-subtle pl-3.5 text-[11px] leading-relaxed text-muted-foreground">
              Selected members can run the allowed tools using this connection’s
              credential. The credential stays encrypted on the server and is
              never shown to members.
            </p>
          </div>
        )}

        <div>
          <FieldLabel htmlFor={`${formId}-auth-type`}>
            Authentication
          </FieldLabel>
          <select
            id={`${formId}-auth-type`}
            value={authType}
            onChange={(e) => setAuthType(e.target.value as typeof authType)}
            className="h-8 w-full cursor-pointer rounded-md border border-input bg-background px-2.5 text-xs text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
          >
            <option value="none">No authentication</option>
            <option value="bearer">Bearer token</option>
            <option value="oauth">OAuth — connect an account</option>
          </select>
        </div>

        {authType === "oauth" ? (
          <div className="space-y-3 border-l-2 border-border-subtle pl-3.5 text-xs">
            <p className="leading-relaxed text-muted-foreground">
              Save this integration, then choose Connect account. Bastio
              discovers the service's OAuth settings and keeps access tokens
              encrypted on the server.
            </p>
            <details>
              <summary className="cursor-pointer font-medium text-foreground">
                Advanced: pre-registered OAuth client
              </summary>
              <div className="mt-3 space-y-3">
                <div>
                  <FieldLabel optional htmlFor={`${formId}-client-id`}>
                    Client ID
                  </FieldLabel>
                  <Input
                    id={`${formId}-client-id`}
                    value={clientID}
                    onChange={(e) => setClientID(e.target.value)}
                    placeholder="Optional; automatic registration when supported"
                    autoComplete="off"
                    autoCorrect="off"
                    autoCapitalize="none"
                    spellCheck={false}
                  />
                </div>
                <div>
                  <FieldLabel optional htmlFor={`${formId}-client-secret`}>
                    Client secret (if required)
                  </FieldLabel>
                  <Input
                    id={`${formId}-client-secret`}
                    type="password"
                    value={clientSecret}
                    onChange={(e) => setClientSecret(e.target.value)}
                    autoComplete="off"
                  />
                </div>
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
          <div>
            <FieldLabel htmlFor={`${formId}-bearer-token`}>
              Bearer token
            </FieldLabel>
            <Input
              id={`${formId}-bearer-token`}
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
          </div>
        ) : null}

        {authType === "bearer" && connection?.has_credential && (
          <label
            htmlFor={`${formId}-clear-token`}
            className="flex cursor-pointer items-center gap-2 text-xs text-foreground"
          >
            <input
              id={`${formId}-clear-token`}
              type="checkbox"
              className="accent-foreground"
              checked={clearToken}
              onChange={(e) => {
                setClearToken(e.target.checked);
                if (e.target.checked) setToken("");
              }}
            />
            <span>Clear the stored token</span>
          </label>
        )}

        <div>
          <FieldLabel optional htmlFor={`${formId}-allowed-tools`}>
            Allowed tools
          </FieldLabel>
          <Input
            id={`${formId}-allowed-tools`}
            value={allowed}
            autoCorrect="off"
            autoCapitalize="none"
            spellCheck={false}
            onChange={(e) => setAllowed(e.target.value)}
            placeholder="search, read_document"
            className="font-mono text-xs"
          />
        </div>

        <p className="border-l-2 border-border-subtle pl-3.5 text-[11px] leading-relaxed text-muted-foreground">
          Leave the allowlist empty to expose all permitted discovered tools.
          Changing the endpoint clears its stored token unless you enter a token
          again. Put credentials in the token field, never in the endpoint URL.
          Public HTTPS Streamable HTTP endpoints support OAuth, bearer tokens
          and no authentication. Changing authentication or the endpoint
          disconnects the stored account. Local stdio servers are not supported
          in the cloud workspace.
        </p>

        {save.isError && (
          <p
            role="alert"
            className="border-l-2 border-destructive pl-3.5 text-xs text-destructive"
          >
            {errorMessage(save.error)}
          </p>
        )}

        <div className="flex justify-end gap-2 pt-1">
          <Button
            type="button"
            variant="ghost"
            size="sm"
            className="h-8 text-xs"
            onClick={onCancel}
          >
            Cancel
          </Button>
          <Button
            type="submit"
            size="sm"
            className="h-8 text-xs"
            disabled={save.isPending}
          >
            {save.isPending ? "Saving…" : "Save integration"}
          </Button>
        </div>
      </form>
    </AdminPanel>
  );
}

function ToolRunner({
  connection,
  canManage,
  onUseResult,
  temporary = false,
  conversationId,
}: {
  connection: Connection;
  canManage: boolean;
  onUseResult?: (text: string) => void;
  temporary?: boolean;
  conversationId?: string | null;
}) {
  const runnerId = useId();
  const [tool, setTool] = useState<Tool | null>(null);
  const [argumentsText, setArgumentsText] = useState("{}");
  const [advanced, setAdvanced] = useState(false);
  const [inputError, setInputError] = useState("");
  const [traceID, setTraceID] = useState("");
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
      const { data, error, response } = await http.POST(
        "/v1/workspace/mcp/connections/{id}/call",
        {
          params: { path: { id: connection.id } },
          body: {
            name: tool!.name,
            arguments: argumentsObject,
            temporary,
            ...(conversationId ? { conversation_id: conversationId } : {}),
          },
        },
      );
      setTraceID(response.headers.get("X-Bastio-Trace-Id") ?? "");
      if (!response.ok) {
        const failure = MCPSecurityError.from(error);
        if (failure) throw failure;
        throw new Error(
          typeof error === "object" && error !== null && "error" in error
            ? String(error.error)
            : "Integration request failed.",
        );
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
    setTraceID("");
    call.mutate(args);
  };

  return (
    <AdminPanel
      title={`Tools · ${connection.name}`}
      description="Inspect discovered tools, review arguments, and execute a policy-checked call."
    >
      <div className="space-y-4 border-b border-border-subtle pb-4">
        {tools.isPending && (
          <p className="flex items-center gap-2 text-xs text-muted-foreground">
            <Loader2 className="size-3.5 animate-spin" aria-hidden="true" />{" "}
            Discovering protected tools…
          </p>
        )}
        {tools.isError && (
          <p
            role="alert"
            className="border-l-2 border-destructive pl-3.5 text-xs text-destructive"
          >
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
          <div>
            <FieldLabel htmlFor={`${runnerId}-tool`}>Tool</FieldLabel>
            <select
              id={`${runnerId}-tool`}
              className="h-8 w-full cursor-pointer rounded-md border border-input bg-background px-2.5 font-mono text-xs text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
              value={tool?.name ?? ""}
              onChange={(e) => {
                const next =
                  tools.data.tools.find((t) => t.name === e.target.value) ??
                  null;
                setTool(next);
                setArgumentsText(defaultArguments(next));
                setAdvanced(false);
                call.reset();
                setTraceID("");
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
          </div>
        )}
        {tool && (
          <>
            {tool.description ? (
              <p className="whitespace-pre-wrap text-xs leading-relaxed text-muted-foreground">
                {tool.description}
              </p>
            ) : null}
            {simpleFields && !advanced ? (
              <div className="space-y-3">
                {Object.entries(properties).map(([key, value]) => {
                  const field = objectValue(value);
                  const type = String(field?.type);
                  const fieldId = `${runnerId}-arg-${key}`;
                  const required =
                    Array.isArray(tool.inputSchema.required) &&
                    tool.inputSchema.required.includes(key);
                  return (
                    <div key={key}>
                      {type === "boolean" ? (
                        <label
                          htmlFor={fieldId}
                          className="flex cursor-pointer items-center gap-2 text-xs text-foreground"
                        >
                          <input
                            id={fieldId}
                            type="checkbox"
                            className="accent-foreground"
                            checked={args?.[key] === true}
                            onChange={(e) => updateField(key, e.target.checked)}
                          />
                          <span className="font-mono">
                            {key}
                            {required ? " *" : ""}
                          </span>
                        </label>
                      ) : (
                        <>
                          <FieldLabel htmlFor={fieldId}>
                            <span className="font-mono">
                              {key}
                              {required ? " *" : ""}
                            </span>
                          </FieldLabel>
                          <Input
                            id={fieldId}
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
                        </>
                      )}
                      {typeof field?.description === "string" && (
                        <p className="mt-1 text-[11px] text-muted-foreground">
                          {field.description}
                        </p>
                      )}
                    </div>
                  );
                })}
              </div>
            ) : (
              <div>
                <FieldLabel htmlFor={`${runnerId}-json-args`}>
                  JSON arguments
                </FieldLabel>
                <textarea
                  id={`${runnerId}-json-args`}
                  className="min-h-32 w-full rounded-md border border-input bg-background p-3 font-mono text-xs text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                  value={argumentsText}
                  onChange={(e) => setArgumentsText(e.target.value)}
                  autoCorrect="off"
                  autoCapitalize="none"
                  spellCheck={false}
                />
              </div>
            )}
            {simpleFields && (
              <Button
                type="button"
                size="sm"
                variant="ghost"
                className="h-7 text-xs"
                onClick={() => setAdvanced(!advanced)}
              >
                {advanced ? "Use fields" : "Edit JSON arguments"}
              </Button>
            )}
            <p className="border-l-2 border-border-subtle pl-3.5 text-[11px] leading-relaxed text-muted-foreground">
              Run sends these arguments to{" "}
              <span className="font-mono text-foreground">
                {endpointHostname(connection.endpoint)}
              </span>{" "}
              after security checks. Review them first: this tool can act on
              your connected service. Requests are not automatically retried.
            </p>
            {inputError && (
              <p
                role="alert"
                className="border-l-2 border-destructive pl-3.5 text-xs text-destructive"
              >
                {inputError}
              </p>
            )}
            <div>
              <Button
                size="sm"
                type="button"
                className="h-8 text-xs"
                disabled={call.isPending}
                onClick={run}
              >
                {call.isPending ? "Running tool…" : "Run tool"}
              </Button>
            </div>
          </>
        )}
        {traceID && !call.isPending && (
          <div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
            {canManage ? (
              <a
                href={`${import.meta.env.VITE_DASHBOARD_URL ?? ""}/traces/${encodeURIComponent(traceID)}`}
                target="_blank"
                rel="noreferrer"
                className="inline-flex cursor-pointer items-center gap-1 underline underline-offset-4 hover:text-foreground"
              >
                View trace{" "}
                <ExternalLink className="size-3" aria-hidden="true" />
              </a>
            ) : (
              <span>Trace recorded · your administrator can review this run.</span>
            )}
            <MonoValue>{traceID}</MonoValue>
          </div>
        )}
        {call.isError &&
          (call.error instanceof MCPSecurityError ? (
            <div
              role="alert"
              className="space-y-2 border-l-2 border-destructive pl-3.5 text-xs"
            >
              <p className="font-medium text-destructive">
                {call.error.stage === "result"
                  ? "Tool ran · result withheld"
                  : "Request withheld · tool did not run"}
              </p>
              <p className="leading-relaxed text-foreground">
                {call.error.reason}
              </p>
              {call.error.blockingDetectors.length > 0 && (
                <p className="font-medium text-foreground">
                  Blocked by:{" "}
                  {call.error.blockingDetectors.map(mcpCategoryLabel).join(", ")}
                </p>
              )}
              {call.error.categories.length > 0 && (
                <p className="text-foreground">
                  Security findings:{" "}
                  {call.error.categories.map(mcpCategoryLabel).join(", ")}
                </p>
              )}
              {call.error.rules.length > 0 && (
                <ul className="list-disc space-y-1 pl-4 text-muted-foreground">
                  {call.error.rules.map((rule) => (
                    <li key={rule}>{mcpRuleLabel(rule)}</li>
                  ))}
                </ul>
              )}
              <p className="leading-relaxed text-muted-foreground">
                {call.error.stage === "result"
                  ? "The remote action was not undone. Check the service before running the tool again."
                  : "These arguments were not sent to the connected service."}
                {canManage
                  ? " Review the active policy in Security Center if you suspect a false positive."
                  : " Ask your workspace administrator to review the active security policy if you suspect a false positive."}
              </p>
              {canManage && (
                <a
                  href={`${import.meta.env.VITE_DASHBOARD_URL ?? ""}/security-settings`}
                  className="inline-flex cursor-pointer underline underline-offset-4 hover:text-foreground"
                >
                  Review security policy
                </a>
              )}
            </div>
          ) : (
            <p
              role="alert"
              className="border-l-2 border-destructive pl-3.5 text-xs text-destructive"
            >
              {errorMessage(call.error)}
            </p>
          ))}
        {call.data && (
          <div className="space-y-2.5 border-t border-border-subtle pt-4">
            <div className="flex items-center gap-2">
              <span
                aria-hidden="true"
                className={cn(
                  "size-1.5 shrink-0 rounded-full",
                  call.data.is_error ? "bg-destructive" : "bg-success",
                )}
              />
              <p className="text-[13px] font-medium text-foreground">
                {call.data.is_error
                  ? "Tool returned an error"
                  : "Protected result"}
              </p>
            </div>
            <pre className="max-h-64 overflow-auto whitespace-pre-wrap break-words border-y border-border-subtle bg-surface-1/40 px-3.5 py-3 font-mono text-xs text-foreground">
              {call.data.text}
            </pre>
            {call.data.categories.length > 0 && (
              <p className="text-[11px] text-muted-foreground">
                Security findings:{" "}
                {[...new Set(call.data.categories)].join(", ")}
              </p>
            )}
            {onUseResult && !call.data.is_error && (
              <Button
                type="button"
                size="sm"
                className="h-8 text-xs"
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
      </div>
    </AdminPanel>
  );
}

function endpointHostname(endpoint: string) {
  try {
    return new URL(endpoint).hostname;
  } catch {
    return endpoint;
  }
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
            <Loader2 className="size-4 animate-spin" aria-hidden="true" />
            Discovering the authorization service…
          </p>
        ) : null}
        {discovery.data ? (
          <div className="space-y-4 text-xs">
            <div className="divide-y divide-border-subtle border-y border-border-subtle">
              <div className="py-2.5">
                <p className="text-[11px] font-medium text-muted-foreground">
                  Authorization service
                </p>
                <p className="mt-1 break-all font-mono text-xs text-foreground">
                  {discovery.data.issuer}
                </p>
              </div>
              <div className="py-2.5">
                <p className="text-[11px] font-medium text-muted-foreground">
                  MCP resource
                </p>
                <p className="mt-1 break-all font-mono text-xs text-foreground">
                  {discovery.data.resource}
                </p>
              </div>
              <div className="py-2.5">
                <p className="text-[11px] font-medium text-muted-foreground">
                  Requested scopes
                </p>
                <p className="mt-1 break-all text-xs text-foreground">
                  {discovery.data.scopes.join(", ") ||
                    "The service determines access during authorization."}
                </p>
              </div>
            </div>
            <details>
              <summary className="cursor-pointer text-muted-foreground hover:text-foreground">
                Callback URL for pre-registered clients
              </summary>
              <MonoValue className="mt-2 block break-all">
                {discovery.data.redirect_uri}
              </MonoValue>
            </details>
            {authorize.data ? (
              <div className="space-y-2.5 border-l-2 border-border-subtle pl-3.5">
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
              </div>
            ) : (
              <Button
                type="button"
                size="sm"
                className="h-8 text-xs"
                disabled={authorize.isPending}
                onClick={() => authorize.mutate()}
              >
                {authorize.isPending ? (
                  <Loader2 className="size-3 animate-spin" aria-hidden="true" />
                ) : null}
                Prepare secure connection
              </Button>
            )}
          </div>
        ) : null}
        {[discovery.error, authorize.error, disconnect.error]
          .filter(Boolean)
          .map((e, i) => (
            <p
              key={i}
              role="alert"
              className="border-l-2 border-destructive pl-3.5 text-xs text-destructive"
            >
              {errorMessage(e)}
            </p>
          ))}
        <div className="flex justify-end gap-2 border-t border-border-subtle pt-3">
          {connection.oauth_connected ? (
            <Button
              type="button"
              variant="outline"
              size="sm"
              className="h-8 text-xs"
              disabled={disconnect.isPending}
              onClick={() => disconnect.mutate()}
            >
              Disconnect account
            </Button>
          ) : null}
          <Button
            type="button"
            variant="outline"
            size="sm"
            className="h-8 text-xs"
            onClick={onClose}
          >
            Close & refresh
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  );
}
