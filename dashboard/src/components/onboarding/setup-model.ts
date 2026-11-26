import type { Proxy, Trace } from "../../api/client";
import type { McpEvent } from "../../api/mcp";
import { gatewayConnection } from "../gateway-setup.ts";

export const guideIds = [
  "documents",
  "claude",
  "codex",
  "antigravity",
  "mcp",
  "gateway",
  "security",
  "traces",
  "workspace",
] as const;
export type GuideId = (typeof guideIds)[number];
export type AppId = "claude" | "codex" | "antigravity" | "other";
export type SetupDraft = {
  guide: GuideId;
  step: number;
  app: AppId;
  os: "mac" | "windows" | "linux";
  coverage: "mcp" | "model";
  intent: "overview" | "tools" | "model";
  toolLocationChosen: boolean;
  provider: "openai" | "anthropic";
  auth: "subscription" | "api";
  mode: "remote" | "stdio";
  name: string;
  model: string;
  environment: string;
  gatewayId: string;
  keyId: string;
  serverId: string;
  verificationId: string;
  startedAt: string;
  verifiedAt: string;
  evidenceId: string;
};
export const guideLabels: Record<GuideId, string> = {
  documents: "Conversations & documents",
  claude: "Claude Code",
  codex: "Codex app",
  antigravity: "Antigravity",
  mcp: "MCP firewall",
  gateway: "Application gateway",
  security: "Security & guardrails",
  traces: "Investigate activity",
  workspace: "Workspace & team",
};
export const setupSteps = [
  "Choose your setup",
  "Create a connection",
  "Configure your app",
  "Verify traffic",
  "Next steps",
];
export function isGuide(value: unknown): value is GuideId {
  return typeof value === "string" && guideIds.includes(value as GuideId);
}
export function isFeatureGuide(guide: GuideId) {
  return ["documents", "security", "traces", "workspace"].includes(guide);
}
export function validateConnectSearch(search: Record<string, unknown>): {
  guide?: GuideId;
} {
  return isGuide(search.guide) ? { guide: search.guide } : {};
}
export function freshDraft(
  guide: GuideId,
  os: SetupDraft["os"] = "mac",
): SetupDraft {
  return {
    guide,
    step: 0,
    app: ["claude", "codex", "antigravity"].includes(guide)
      ? (guide as AppId)
      : "claude",
    os,
    coverage: guide === "gateway" ? "model" : "mcp",
    intent:
      guide === "gateway" ? "model" : guide === "mcp" ? "tools" : "overview",
    toolLocationChosen: false,
    provider: guide === "claude" ? "anthropic" : "openai",
    auth: guide === "gateway" ? "api" : "subscription",
    mode: "remote",
    name: `${guideLabels[guide]} connection`,
    model: "",
    environment: "",
    gatewayId: "",
    keyId: "",
    serverId: "",
    verificationId: `bastio-setup-${crypto.randomUUID()}`,
    startedAt: new Date().toISOString(),
    verifiedAt: "",
    evidenceId: "",
  };
}
export function progressKey(workspace: string, identity: string) {
  return `bastio:guided-setup:v1:${encodeURIComponent(workspace)}:${encodeURIComponent(identity)}`;
}
// The allowlist is deliberate: secrets, command arguments and upstream URLs
// never enter browser storage, even if extra fields are accidentally supplied.
export function safeDraft(value: unknown, guide: GuideId): SetupDraft | null {
  if (!value || typeof value !== "object") return null;
  const input = value as Record<string, unknown>;
  if (
    input.guide !== guide ||
    !Number.isInteger(input.step) ||
    Number(input.step) < 0 ||
    Number(input.step) > 4
  )
    return null;
  if (
    !["claude", "codex", "antigravity", "other"].includes(String(input.app)) ||
    !["mac", "windows", "linux"].includes(String(input.os))
  )
    return null;
  if (
    !["mcp", "model"].includes(String(input.coverage)) ||
    !["subscription", "api"].includes(String(input.auth)) ||
    !["stdio", "remote"].includes(String(input.mode))
  )
    return null;
  const provider =
    input.provider ?? (guide === "claude" ? "anthropic" : "openai");
  if (provider !== "openai" && provider !== "anthropic") return null;
  const keys = [
    "name",
    "model",
    "environment",
    "gatewayId",
    "keyId",
    "serverId",
    "verificationId",
    "startedAt",
    "verifiedAt",
    "evidenceId",
  ] as const;
  if (
    keys.some(
      (key) =>
        typeof input[key] !== "string" || String(input[key]).length > 500,
    )
  )
    return null;
  if (
    !/^bastio-setup-[a-f0-9-]+$/.test(String(input.verificationId)) ||
    !Number.isFinite(Date.parse(String(input.startedAt)))
  )
    return null;
  // Unsupported app/model combinations cannot be restored from old progress.
  const coverage =
    guide === "antigravity" || guide === "mcp"
      ? "mcp"
      : guide === "gateway"
        ? "model"
        : (input.coverage as SetupDraft["coverage"]);
  if (coverage === "model" && input.auth !== "api") return null;
  const hasConnection = Boolean(input.gatewayId || input.serverId);
  const legacyIntent =
    Number(input.step) > 0 || hasConnection
      ? coverage === "model" ? "model" : "tools"
      : guide === "gateway" ? "model" : guide === "mcp" ? "tools" : "overview";
  const intent = input.intent ?? legacyIntent;
  if (!["overview", "tools", "model"].includes(String(intent))) return null;
  if (
    input.toolLocationChosen !== undefined &&
    typeof input.toolLocationChosen !== "boolean"
  )
    return null;
  return {
    guide,
    step: Number(input.step),
    app: input.app as AppId,
    os: input.os as SetupDraft["os"],
    coverage,
    intent: intent as SetupDraft["intent"],
    toolLocationChosen:
      input.toolLocationChosen === true || hasConnection || Number(input.step) > 0,
    provider,
    auth: input.auth as SetupDraft["auth"],
    mode: input.mode as SetupDraft["mode"],
    ...Object.fromEntries(keys.map((key) => [key, input[key]])),
  } as SetupDraft;
}
export function readProgress(
  storage: Pick<Storage, "getItem">,
  key: string,
): Partial<Record<GuideId, SetupDraft>> {
  try {
    const data = JSON.parse(storage.getItem(key) || "{}");
    return Object.fromEntries(
      guideIds.flatMap((id) => {
        const draft = safeDraft(data?.[id], id);
        return draft ? [[id, draft]] : [];
      }),
    );
  } catch {
    return {};
  }
}
export function saveProgress(
  storage: Pick<Storage, "setItem">,
  key: string,
  drafts: Partial<Record<GuideId, SetupDraft>>,
) {
  try {
    storage.setItem(
      key,
      JSON.stringify(
        Object.fromEntries(
          guideIds.flatMap((id) => {
            const draft = safeDraft(drafts[id], id);
            return draft ? [[id, draft]] : [];
          }),
        ),
      ),
    );
    return true;
  } catch {
    return false;
  }
}
export function gatewayEvidence(
  traces: Trace[],
  draft: SetupDraft,
): Trace | undefined {
  return traces.find(
    (trace) =>
      trace.proxy_id === draft.gatewayId &&
      trace.session_id === draft.verificationId &&
      Date.parse(trace.started_at) >= Date.parse(draft.startedAt),
  );
}
export function mcpEvidence(
  events: McpEvent[],
  draft: SetupDraft,
): McpEvent | undefined {
  return events.find(
    (event) =>
      event.server_id === draft.serverId &&
      event.direction === "request" &&
      Date.parse(event.occurred_at) >= Date.parse(draft.startedAt),
  );
}
export function parseToolList(value: string) {
  return [
    ...new Set(
      value
        .split(/[,\n]/)
        .map((tool) => tool.trim())
        .filter(Boolean),
    ),
  ];
}
export function parseArguments(value: string): string[] | null {
  try {
    const parsed: unknown = JSON.parse(value);
    return Array.isArray(parsed) &&
      parsed.every((arg) => typeof arg === "string")
      ? parsed
      : null;
  } catch {
    return null;
  }
}
const quote = (value: string) => JSON.stringify(value);
export const SECRET_PLACEHOLDER = "PASTE_YOUR_BASTIO_KEY";
export const TOKEN_PLACEHOLDER = "PASTE_YOUR_MCP_ENROLLMENT_TOKEN";
export function modelConfiguration(
  draft: SetupDraft,
  gateway: Proxy,
  origin: string,
  secret: string,
) {
  const key = secret || SECRET_PLACEHOLDER;
  const connection = gatewayConnection(gateway, origin, draft.model);
  if (draft.guide === "gateway") {
    if (draft.os === "windows") {
      const psQuote = (value: string) =>
        "'" + value.replaceAll("'", "''") + "'";
      const anthropic = gateway.target_provider === "anthropic";
      const body = JSON.stringify({
        model: draft.model || gateway.target_model,
        ...(anthropic ? { max_tokens: 256 } : {}),
        messages: [{ role: "user", content: "Bastio connection test" }],
      });
      return {
        file: "PowerShell",
        language: "PowerShell",
        code: `$env:BASTIO_API_KEY = ${psQuote(key)}\n$headers = @{ Authorization = "Bearer $env:BASTIO_API_KEY"; "X-Bastio-Session-Id" = ${psQuote(draft.verificationId)}${anthropic ? '; "anthropic-version" = "2023-06-01"' : ""} }\nInvoke-RestMethod -Method Post -Uri ${psQuote(`${connection.baseURL}/${anthropic ? "messages" : "chat/completions"}`)} -Headers $headers -ContentType 'application/json' -Body ${psQuote(body)}`,
      };
    }
    const shQuote = (value: string) =>
      "'" + value.replaceAll("'", "'\\''") + "'";
    const command = connection.curl.replace(
      " \\\n",
      ` \\\n  -H ${shQuote(`X-Bastio-Session-Id: ${draft.verificationId}`)} \\\n`,
    );
    return {
      file: "Terminal",
      language: "Shell",
      code: `export BASTIO_API_KEY=${shQuote(key)}\n${command}`,
    };
  }
  if (draft.app === "claude")
    return {
      file:
        draft.os === "windows"
          ? "%USERPROFILE%\\.claude\\settings.json"
          : "~/.claude/settings.json",
      language: "JSON",
      code: JSON.stringify(
        {
          env: {
            ANTHROPIC_BASE_URL: connection.sdkBaseURL,
            ANTHROPIC_AUTH_TOKEN: key,
            ...(draft.model ? { ANTHROPIC_MODEL: draft.model } : {}),
            ANTHROPIC_CUSTOM_HEADERS: `X-Bastio-Session-Id: ${draft.verificationId}`,
          },
        },
        null,
        2,
      ),
    };
  return {
    file:
      draft.os === "windows"
        ? "%USERPROFILE%\\.codex\\config.toml"
        : "~/.codex/config.toml",
    language: "TOML",
    code: `model = ${quote(draft.model || gateway.target_model || "YOUR_MODEL_ID")}\nmodel_provider = "bastio"\n\n[model_providers.bastio]\nname = "Bastio"\nbase_url = ${quote(connection.baseURL)}\nwire_api = "responses"\nrequires_openai_auth = false\nhttp_headers = { "Authorization" = ${quote(`Bearer ${key}`)}, "X-Bastio-Session-Id" = ${quote(draft.verificationId)} }`,
  };
}
export function mcpConfiguration(
  draft: SetupDraft,
  controlOrigin: string,
  token: string,
  executable = "npx",
  args: string[] = [],
  bastioPath = "bastio",
) {
  const secret = token || TOKEN_PLACEHOLDER;
  const origin = controlOrigin.replace(/\/$/, "");
  const cliArgs = [
    "mcp-proxy",
    "--control-url",
    origin,
    "--token-env",
    "BASTIO_MCP_TOKEN",
    "--",
    executable,
    ...args,
  ];
  const remote = {
    ...(draft.app === "claude" ? { type: "http" } : {}),
    [draft.app === "antigravity" ? "serverUrl" : "url"]:
      `${origin}/v1/mcp/gateway`,
    headers: { Authorization: `Bearer ${secret}` },
  };
  const local = {
    command: bastioPath,
    args: cliArgs,
    env: { BASTIO_MCP_TOKEN: secret },
  };
  if (draft.app === "codex")
    return {
      file:
        draft.os === "windows"
          ? "%USERPROFILE%\\.codex\\config.toml"
          : "~/.codex/config.toml",
      language: "TOML",
      code: `[mcp_servers.bastio_tools]\n${draft.mode === "remote" ? `url = ${quote(`${origin}/v1/mcp/gateway`)}\nhttp_headers = { "Authorization" = ${quote(`Bearer ${secret}`)} }` : `command = ${quote(bastioPath)}\nargs = ${JSON.stringify(cliArgs)}\nenv = { BASTIO_MCP_TOKEN = ${quote(secret)} }`}`,
    };
  return {
    file:
      draft.app === "claude"
        ? ".mcp.json · project root"
        : draft.app === "antigravity"
          ? "MCP Servers → Manage MCP Servers → View raw config"
          : "Your application's MCP configuration",
    language: "JSON",
    code: JSON.stringify(
      {
        mcpServers: { bastio_tools: draft.mode === "remote" ? remote : local },
      },
      null,
      2,
    ),
  };
}
export const integrationDocs = {
  claude: {
    label: "Claude Code documentation",
    url: "https://code.claude.com/docs/en/mcp",
  },
  codex: {
    label: "Codex configuration reference",
    url: "https://developers.openai.com/codex/config-file/config-reference",
  },
  antigravity: {
    label: "Antigravity MCP documentation",
    url: "https://antigravity.google/docs/mcp/",
  },
  other: {
    label: "Bastio MCP documentation",
    url: "https://bastio.com/docs/mcp-proxy",
  },
};
