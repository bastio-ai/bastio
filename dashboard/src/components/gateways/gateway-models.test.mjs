import assert from "node:assert/strict";
import test from "node:test";
import {
  SNIPPET_MODEL_PREFERENCES,
  gatewayModelQueryEnabled,
  gatewayModelQueryKey,
  parseGatewayModelListing,
  snippetModelState,
  isChatModelId,
  pickSnippetModel,
  resolveSnippetModel,
  snippetModelIds,
} from "./gateway-models.ts";
import { gatewayBlockReason, gatewayTestBlocked } from "./gateway-test.ts";

const listing = (provider, ids, source = "provider") => ({
  provider,
  source,
  models: ids.map((id) => ({ id })),
});

test("non-chat models are excluded", () => {
  for (const id of [
    "text-embedding-3-small",
    "whisper-1",
    "tts-1",
    "gpt-4o-mini-tts",
    "gpt-4o-transcribe",
    "dall-e-3",
    "gpt-image-1",
    "omni-moderation-latest",
    "gpt-4o-realtime-preview",
    "gpt-4o-audio-preview",
    "gpt-4o-search-preview",
    "gpt-3.5-turbo-instruct",
    "davinci-002",
    "babbage-002",
    "meta-llama/llama-guard-4-12b",
    "",
  ])
    assert.equal(isChatModelId(id), false, id);
  for (const id of ["gpt-4o-mini", "claude-haiku-4-5", "gemini-2.0-flash", "llama-3.1-8b-instant", "deepseek-chat"])
    assert.equal(isChatModelId(id), true, id);
});

test("picks the first preference that the provider actually reported, regardless of list order", () => {
  assert.equal(
    pickSnippetModel("openai", ["gpt-5", "text-embedding-3-small", "gpt-4.1-mini", "gpt-4o-mini", "o3"]),
    "gpt-4o-mini",
  );
  assert.equal(
    pickSnippetModel("anthropic", ["claude-opus-4-1", "claude-3-5-haiku-20241022", "claude-sonnet-4-5"]),
    "claude-3-5-haiku-20241022",
  );
  assert.equal(pickSnippetModel("groq", ["llama-3.3-70b-versatile", "llama-3.1-8b-instant"]), "llama-3.1-8b-instant");
});

test("Gemini preferences match the reported models/ prefix and return the reported ID", () => {
  assert.equal(pickSnippetModel("gemini", ["models/gemini-2.5-pro", "models/gemini-2.0-flash"]), "models/gemini-2.0-flash");
});

test("never invents an ID: falls back to the first chat-capable reported ID", () => {
  assert.equal(pickSnippetModel("openai", ["text-embedding-3-large", "ft:gpt-custom:acme", "gpt-5-pro"]), "ft:gpt-custom:acme");
  assert.equal(pickSnippetModel("unknown-provider", ["whisper-1", "some-chat"]), "some-chat");
  assert.equal(pickSnippetModel("openai", ["whisper-1", "dall-e-3"]), "");
  assert.equal(pickSnippetModel("openai", []), "");
  for (const [provider, preferred] of Object.entries(SNIPPET_MODEL_PREFERENCES)) {
    const picked = pickSnippetModel(provider, ["only-this-one"]);
    assert.equal(picked, "only-this-one", provider);
    assert.ok(!preferred.includes(picked));
  }
});

test("snippetModelIds uses only provider-reported, chat-capable, de-duplicated IDs", () => {
  assert.deepEqual(
    snippetModelIds(listing("openai", ["gpt-4o-mini", " gpt-4o-mini ", "whisper-1", "gpt-5"])),
    ["gpt-4o-mini", "gpt-5"],
  );
  assert.deepEqual(snippetModelIds(listing("deepseek", ["deepseek-chat"], "catalog")), []);
  assert.deepEqual(snippetModelIds(undefined), []);
  assert.deepEqual(snippetModelIds(null), []);
});

test("resolution order: configured default, then reported pick, then placeholder (empty)", () => {
  const reported = listing("openai", ["gpt-5", "gpt-4o-mini"]);
  assert.equal(resolveSnippetModel("openai", " my-default ", reported), "my-default");
  assert.equal(resolveSnippetModel("openai", "", reported), "gpt-4o-mini");
  assert.equal(resolveSnippetModel("openai", "", undefined), "");
  assert.equal(resolveSnippetModel("deepseek", "", listing("deepseek", ["deepseek-chat"], "catalog")), "");
});

test("security blocks are recognized and explain why", () => {
  const body = {
    error: "request blocked by security policy",
    reason: "prompt injection detected",
    threat_types: ["prompt_injection", "jailbreak"],
    trace_id: "0190a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5b",
  };
  assert.equal(gatewayTestBlocked(403, body), true);
  assert.equal(gatewayBlockReason(body), "prompt injection detected · prompt injection, jailbreak");
  assert.equal(gatewayBlockReason({ error: "response blocked by security policy" }), "A security policy on this gateway stopped the request.");
  assert.equal(gatewayTestBlocked(403, { error: { type: "permission_denied", message: "nope" } }), false);
  assert.equal(gatewayTestBlocked(401, { error: "unauthorized" }), false);
});

test("discovery parsing: server error bodies surface their status and message, never throw", () => {
  assert.deepEqual(
    parseGatewayModelListing(502, JSON.stringify({ error: "no provider credential configured" }), "openai"),
    { ok: false, status: 502, message: "no provider credential configured" },
  );
  assert.deepEqual(
    parseGatewayModelListing(403, JSON.stringify({ error: { message: "workspace role not resolved for this request" } }), "openai"),
    { ok: false, status: 403, message: "workspace role not resolved for this request" },
  );
  const html = parseGatewayModelListing(502, "<html><body>Bad gateway</body></html>", "openai");
  assert.equal(html.ok, false);
  assert.equal(html.status, 502);
  assert.equal(html.message, "The server returned an HTML error page (HTTP 502).");
  assert.deepEqual(parseGatewayModelListing(500, "", "openai"), { ok: false, status: 500, message: "HTTP 500" });
});

test("discovery parsing: 200 with an error envelope or no list is a failure, not an empty list", () => {
  assert.deepEqual(parseGatewayModelListing(200, JSON.stringify({ error: "provider unavailable" }), "openai"), {
    ok: false,
    status: 200,
    message: "provider unavailable",
  });
  assert.equal(parseGatewayModelListing(200, "not json", "openai").ok, false);
  assert.equal(parseGatewayModelListing(200, "[]", "openai").ok, false);
  assert.equal(
    parseGatewayModelListing(200, JSON.stringify({ provider: "anthropic", source: "provider", models: [] }), "openai").ok,
    false,
  );
});

test("discovery parsing: accepts the OSS shape, a data array, and string IDs", () => {
  assert.deepEqual(
    parseGatewayModelListing(200, JSON.stringify({ provider: "openai", source: "provider", models: [{ id: "gpt-4o-mini" }, { id: " " }, { id: "gpt-5", name: "GPT-5" }] }), "openai"),
    { ok: true, listing: { provider: "openai", source: "provider", models: [{ id: "gpt-4o-mini" }, { id: "gpt-5", name: "GPT-5" }] } },
  );
  assert.deepEqual(
    parseGatewayModelListing(200, JSON.stringify({ data: [{ id: "gpt-4o-mini", object: "model" }] }), "openai"),
    { ok: true, listing: { provider: "openai", source: "provider", models: [{ id: "gpt-4o-mini" }] } },
  );
  assert.deepEqual(
    parseGatewayModelListing(200, JSON.stringify({ provider: "deepseek", source: "catalog", models: ["deepseek-chat"] }), "deepseek"),
    { ok: true, listing: { provider: "deepseek", source: "catalog", models: [{ id: "deepseek-chat" }] } },
  );
});

test("query keying and enabling follow the gateway, not provider-key state", () => {
  const gw = { id: "gw-1", target_provider: "openai", is_active: true };
  assert.deepEqual(gatewayModelQueryKey(gw), ["gateway-models", "gw-1", "openai"]);
  assert.notDeepEqual(gatewayModelQueryKey({ ...gw, target_provider: "anthropic" }), gatewayModelQueryKey(gw));
  // The key must share the ["gateway-models"] prefix that provider-key mutations invalidate.
  assert.equal(gatewayModelQueryKey(gw)[0], "gateway-models");
  assert.equal(gatewayModelQueryEnabled(gw), true);
  assert.equal(gatewayModelQueryEnabled({ ...gw, is_active: false }), false);
  assert.equal(gatewayModelQueryEnabled({ ...gw, id: "" }), false);
  assert.equal(gatewayModelQueryEnabled({ ...gw, target_provider: "typesafe" }), false);
});

test("snippet state: discovered models drive a picker with the resolved default", () => {
  const state = snippetModelState({
    configured: "",
    provider: "openai",
    listing: listing("openai", ["gpt-5", "gpt-4o-mini", "whisper-1"]),
    discovering: false,
  });
  assert.deepEqual(state, { model: "gpt-4o-mini", options: ["gpt-5", "gpt-4o-mini"], mode: "picker", reason: null });
  assert.equal(snippetModelState({ configured: "", provider: "openai", listing: listing("openai", ["gpt-5", "gpt-4o-mini"]), discovering: false, picked: "gpt-5" }).model, "gpt-5");
  // A picked ID that is not offered is ignored rather than trusted.
  assert.equal(snippetModelState({ configured: "", provider: "openai", listing: listing("openai", ["gpt-4o-mini"]), discovering: false, picked: "made-up" }).model, "gpt-4o-mini");
});

test("snippet state: a failed discovery is a manual state, and a typed model ID enables the request", () => {
  const failed = snippetModelState({ configured: "", provider: "openai", listing: undefined, discovering: false, error: "HTTP 502 · no provider credential configured" });
  assert.deepEqual(failed, { model: "", options: [], mode: "manual", reason: "error" });
  const typed = snippetModelState({ configured: "", provider: "openai", listing: undefined, discovering: false, error: "HTTP 502", typed: " gpt-4o-mini " });
  assert.equal(typed.mode, "manual");
  assert.equal(typed.model, "gpt-4o-mini");
  assert.equal(snippetModelState({ configured: "", provider: "deepseek", listing: listing("deepseek", ["deepseek-chat"], "catalog"), discovering: false }).reason, "catalog");
  assert.equal(snippetModelState({ configured: "", provider: "openai", listing: listing("openai", ["whisper-1"]), discovering: false }).reason, "empty");
});

test("snippet state: loading only without a configured default; configured default needs no discovery", () => {
  assert.equal(snippetModelState({ configured: "", provider: "openai", discovering: true }).mode, "loading");
  assert.deepEqual(
    snippetModelState({ configured: "my-default", provider: "openai", discovering: true, error: "HTTP 502" }),
    { model: "my-default", options: ["my-default"], mode: "configured", reason: null },
  );
  const withList = snippetModelState({ configured: "my-default", provider: "openai", listing: listing("openai", ["gpt-4o-mini"]), discovering: false });
  assert.equal(withList.mode, "picker");
  assert.equal(withList.model, "my-default");
  assert.deepEqual(withList.options, ["my-default", "gpt-4o-mini"]);
});
