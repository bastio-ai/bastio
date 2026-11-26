import test from "node:test";
import assert from "node:assert/strict";
import {
  freshDraft,
  safeDraft,
  readProgress,
  saveProgress,
  progressKey,
  validateConnectSearch,
  gatewayEvidence,
  mcpEvidence,
  modelConfiguration,
  mcpConfiguration,
  parseArguments,
  parseToolList,
} from "./setup-model.ts";
const gateway = {
  id: "gateway-1",
  name: "Test gateway",
  target_provider: "anthropic",
  target_model: "test-model",
  is_active: true,
};
const draft = () => ({
  ...freshDraft("claude"),
  gatewayId: gateway.id,
  model: "test-model",
});

test("API model routing requires API authentication", () => {
  assert.equal(freshDraft("gateway").auth, "api");
  assert.equal(
    safeDraft(
      { ...draft(), coverage: "model", auth: "subscription" },
      "claude",
    ),
    null,
  );
});
test("app guides start with a coverage explanation and never assume a tool location", () => {
  for (const app of ["claude", "codex", "antigravity"]) {
    const initial = freshDraft(app);
    assert.equal(initial.intent, "overview");
    assert.equal(initial.toolLocationChosen, false);
  }
  assert.equal(freshDraft("mcp").toolLocationChosen, false);
});
test("old unstarted app drafts do not restore the implicit remote-server choice", () => {
  const old = { ...freshDraft("claude"), mode: "remote" };
  delete old.intent;
  delete old.toolLocationChosen;
  const restored = safeDraft(old, "claude");
  assert.equal(restored.intent, "overview");
  assert.equal(restored.toolLocationChosen, false);

  const connected = safeDraft({ ...old, step: 2, serverId: "existing-tool" }, "claude");
  assert.equal(connected.intent, "tools");
  assert.equal(connected.toolLocationChosen, true);
  assert.equal(connected.serverId, "existing-tool");
  assert.equal(connected.mode, "remote");
});
test("explicit protection and tool-location choices survive resume", () => {
  const selected = safeDraft({
    ...freshDraft("claude"), intent: "tools", mode: "stdio", toolLocationChosen: true,
  }, "claude");
  assert.equal(selected.intent, "tools");
  assert.equal(selected.mode, "stdio");
  assert.equal(selected.toolLocationChosen, true);
  assert.equal(safeDraft({ ...selected, intent: "unknown" }, "claude"), null);
});
test("unsupported Antigravity model routing cannot be restored", () => {
  const restored = safeDraft(
    { ...freshDraft("antigravity"), coverage: "model" },
    "antigravity",
  );
  assert.equal(restored.coverage, "mcp");
});
test("unknown guide searches are discarded", () => {
  assert.deepEqual(validateConnectSearch({ guide: "antigravity" }), {
    guide: "antigravity",
  });
  assert.deepEqual(
    validateConnectSearch({ guide: "arbitrary", token: "secret" }),
    {},
  );
});
test("progress is scoped to both workspace and identity", () => {
  assert.notEqual(progressKey("one", "user"), progressKey("two", "user"));
  assert.notEqual(progressKey("one", "user"), progressKey("one", "other"));
});
test("only allowlisted progress reaches storage; credentials and arguments are excluded", () => {
  let stored = "";
  const data = {
    ...draft(),
    secret: "private-key",
    token: "private-token",
    credential: "upstream-secret",
    args: ["another-secret"],
  };
  assert.equal(
    saveProgress({ setItem: (_key, value) => (stored = value) }, "test", {
      claude: data,
    }),
    true,
  );
  for (const secret of [
    "private-key",
    "private-token",
    "upstream-secret",
    "another-secret",
  ])
    assert.equal(stored.includes(secret), false);
  assert.equal(
    readProgress({ getItem: () => stored }, "test").claude.gatewayId,
    gateway.id,
  );
});
test("corrupt or blocked storage fails safely", () => {
  assert.deepEqual(readProgress({ getItem: () => "{invalid" }, "test"), {});
  assert.deepEqual(
    readProgress(
      {
        getItem: () => {
          throw new Error("blocked");
        },
      },
      "test",
    ),
    {},
  );
  assert.equal(
    saveProgress(
      {
        setItem: () => {
          throw new Error("blocked");
        },
      },
      "test",
      { claude: draft() },
    ),
    false,
  );
  assert.equal(safeDraft({ ...draft(), step: -1 }, "claude"), null);
});
test("header identifiers containing shell injection cannot be restored", () => {
  assert.equal(
    safeDraft(
      { ...draft(), verificationId: "bastio-setup-'\n$(command)" },
      "claude",
    ),
    null,
  );
});
test("gateway verification needs the selected gateway, setup marker, and fresh timestamp", () => {
  const d = draft();
  const valid = {
    id: "trace-1",
    proxy_id: d.gatewayId,
    session_id: d.verificationId,
    started_at: new Date(Date.parse(d.startedAt) + 1).toISOString(),
  };
  assert.equal(gatewayEvidence([valid], d).id, "trace-1");
  for (const invalid of [
    { ...valid, proxy_id: "other" },
    { ...valid, session_id: "other" },
    { ...valid, started_at: "2000-01-01" },
  ])
    assert.equal(gatewayEvidence([invalid], d), undefined);
});
test("MCP verification ignores other servers, old events, and tool discovery", () => {
  const d = { ...draft(), serverId: "server-1" };
  const valid = {
    id: "event-1",
    server_id: d.serverId,
    direction: "request",
    occurred_at: new Date(Date.parse(d.startedAt) + 1).toISOString(),
  };
  assert.equal(mcpEvidence([valid], d).id, "event-1");
  for (const invalid of [
    { ...valid, server_id: "other" },
    { ...valid, direction: "tool_description" },
    { ...valid, occurred_at: "2000-01-01" },
  ])
    assert.equal(mcpEvidence([invalid], d), undefined);
});
test("Claude Code uses its settings file, native gateway base, bearer auth, and verification header", () => {
  const d = draft();
  const config = modelConfiguration(
    d,
    gateway,
    "https://api.example.test",
    "real-test-key",
  );
  const env = JSON.parse(config.code).env;
  assert.equal(config.file, "~/.claude/settings.json");
  assert.equal(
    env.ANTHROPIC_BASE_URL,
    "https://api.example.test/v1/guard/gateway-1",
  );
  assert.equal(env.ANTHROPIC_AUTH_TOKEN, "real-test-key");
  assert.equal(
    env.ANTHROPIC_CUSTOM_HEADERS,
    `X-Bastio-Session-Id: ${d.verificationId}`,
  );
});
test("Codex provider configuration uses Responses and keeps custom provider settings at user scope", () => {
  const d = { ...draft(), app: "codex", guide: "codex" };
  const config = modelConfiguration(
    d,
    { ...gateway, target_provider: "openai" },
    "https://api.example.test",
    'key"with\ncharacters',
  );
  assert.equal(config.file, "~/.codex/config.toml");
  assert.match(config.code, /wire_api = "responses"/);
  assert.match(config.code, /requires_openai_auth = false/);
  assert.ok(
    config.code.includes(JSON.stringify('Bearer key"with\ncharacters')),
  );
  assert.ok(config.code.includes(d.verificationId));
});
test("API gateway command exports a real key and includes the setup header", () => {
  const d = { ...draft(), guide: "gateway" };
  const config = modelConfiguration(
    d,
    { ...gateway, target_provider: "openai" },
    "https://api.example.test",
    "key'quoted",
  );
  assert.ok(config.code.includes("export BASTIO_API_KEY="));
  assert.ok(config.code.includes("'key'\\''quoted'"));
  assert.ok(config.code.includes(`X-Bastio-Session-Id: ${d.verificationId}`));
});
test("Windows API guide generates PowerShell rather than a POSIX shell export", () => {
  const config = modelConfiguration(
    { ...draft(), guide: "gateway", os: "windows" },
    { ...gateway, target_provider: "openai" },
    "https://api.example.test",
    "test-key",
  );
  assert.equal(config.language, "PowerShell");
  assert.match(config.code, /Invoke-RestMethod/);
  assert.ok(!config.code.includes("export "));
});
test("Claude Code remote MCP requires the HTTP type and project MCP file", () => {
  const config = mcpConfiguration(
    draft(),
    "https://control.example.test/",
    "test-token",
  );
  const entry = JSON.parse(config.code).mcpServers.bastio_tools;
  assert.equal(entry.type, "http");
  assert.equal(entry.url, "https://control.example.test/v1/mcp/gateway");
  assert.equal(entry.headers.Authorization, "Bearer test-token");
  assert.match(config.file, /\.mcp\.json/);
});
test("Antigravity uses serverUrl; Codex uses its TOML MCP server section", () => {
  const ag = JSON.parse(
    mcpConfiguration(
      { ...draft(), app: "antigravity" },
      "https://control.example.test",
      "token",
    ).code,
  ).mcpServers.bastio_tools;
  assert.equal(ag.serverUrl, "https://control.example.test/v1/mcp/gateway");
  assert.equal(ag.url, undefined);
  const codex = mcpConfiguration(
    { ...draft(), app: "codex" },
    "https://control.example.test",
    "token",
  );
  assert.match(codex.code, /\[mcp_servers.bastio_tools\]/);
  assert.ok(codex.code.includes("http_headers"));
});
test("local wrapper keeps exact argument boundaries and uses the control plane token env", () => {
  const config = mcpConfiguration(
    { ...draft(), mode: "stdio" },
    "https://control.example.test",
    "token",
    "python3",
    ["/path with spaces/tool.py", "--flag"],
    "/absolute/path/bastio",
  );
  const entry = JSON.parse(config.code).mcpServers.bastio_tools;
  assert.equal(entry.command, "/absolute/path/bastio");
  assert.deepEqual(entry.args, [
    "mcp-proxy",
    "--control-url",
    "https://control.example.test",
    "--token-env",
    "BASTIO_MCP_TOKEN",
    "--",
    "python3",
    "/path with spaces/tool.py",
    "--flag",
  ]);
  assert.equal(entry.env.BASTIO_MCP_TOKEN, "token");
});
test("invalid arguments do not become commands; policy tool names stay exact", () => {
  assert.equal(parseArguments("not json"), null);
  assert.equal(parseArguments("[true]"), null);
  assert.deepEqual(parseArguments('["path with spaces"]'), [
    "path with spaces",
  ]);
  assert.deepEqual(parseToolList("read_file, write_file\nread_file"), [
    "read_file",
    "write_file",
  ]);
});

test("API guide provider choice survives progress storage and older drafts get a default", () => {
  const selected = { ...freshDraft("gateway"), provider: "anthropic" };
  assert.equal(safeDraft(selected, "gateway").provider, "anthropic");
  const { provider: _provider, ...older } = freshDraft("gateway");
  assert.equal(safeDraft(older, "gateway").provider, "openai");
});

test("Anthropic PowerShell guide uses Messages, token limits, and version header", () => {
  const config = modelConfiguration(
    {
      ...freshDraft("gateway"),
      provider: "anthropic",
      os: "windows",
      model: "test-model",
    },
    gateway,
    "https://api.example.test",
    "test-key",
  );
  assert.ok(config.code.includes("/messages"));
  assert.ok(config.code.includes("anthropic-version"));
  assert.ok(config.code.includes("max_tokens"));
  assert.ok(!config.code.includes("/chat/completions"));
});
