import assert from "node:assert/strict";
import test from "node:test";
import {
  SNIPPET_MODEL_PREFERENCES,
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
