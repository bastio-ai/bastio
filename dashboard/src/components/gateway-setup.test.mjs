import assert from "node:assert/strict";
import test from "node:test";
import {
  availableProviderKeys,
  gatewayAppKeys,
  gatewayConnection,
  gatewaySetupStatus,
  validateGatewaySearch,
} from "./gateway-setup.ts";

const gateway = {
  id: "gateway-1",
  name: "Support",
  target_provider: "openai",
  target_model: "gpt-example",
  is_active: true,
};
const provider = { id: "provider-1", provider: "openai", is_default: true };
const app = { id: "key-1", is_active: true, scopes: ["proxy:gateway-1"] };

test("setup distinguishes enabled routes, missing credentials, scoped app keys, and unchecked server auth", () => {
  assert.equal(gatewaySetupStatus(gateway), "Checking setup");
  assert.equal(gatewaySetupStatus(gateway, [], []), "Add provider key");
  assert.equal(
    gatewaySetupStatus(gateway, [provider], [{ ...app, scopes: [] }]),
    "Create app key",
  );
  assert.equal(
    gatewaySetupStatus(gateway, [provider], [app]),
    "Setup complete",
  );
  assert.equal(
    gatewaySetupStatus({ ...gateway, is_active: false }, [provider], [app]),
    "Disabled",
  );
  assert.equal(
    gatewaySetupStatus({ ...gateway, target_provider: "ollama" }, [], [app]),
    "Check server setup",
  );
  assert.deepEqual(
    gatewayAppKeys(gateway, [
      app,
      { ...app, is_active: false },
      { ...app, scopes: ["proxy:other"] },
    ]),
    [app],
  );
});

test("provider readiness includes shared defaults and this gateway, excluding unrelated credentials", () => {
  const bound = {
    ...provider,
    id: "bound",
    proxy_id: gateway.id,
    is_default: false,
  };
  assert.deepEqual(
    availableProviderKeys(gateway, [
      provider,
      bound,
      { ...provider, proxy_id: "other" },
      { ...provider, provider: "anthropic" },
      { ...provider, is_default: false },
    ]),
    [provider, bound],
  );
});

test("OpenRouter examples preserve its native model ID and never contain provider secrets", () => {
  const example = gatewayConnection(
    {
      ...gateway,
      target_provider: "openrouter",
      target_model: "anthropic/claude-example",
    },
    "https://cloud.bastio.com/",
  );
  assert.equal(
    example.baseURL,
    "https://cloud.bastio.com/v1/guard/gateway-1/v1",
  );
  assert.match(example.curl, /Bearer \$BASTIO_API_KEY/);
  assert.match(example.curl, /anthropic\/claude-example/);
  assert.match(example.python, /from openai import OpenAI/);
});

test("Anthropic SDK base omits v1 while curl includes it, with required native headers", () => {
  const example = gatewayConnection(
    { ...gateway, target_provider: "anthropic" },
    "https://bastio.example",
  );
  assert.equal(example.sdkBaseURL, "https://bastio.example/v1/guard/gateway-1");
  assert.match(example.curl, /\/v1\/messages'/);
  assert.match(example.curl, /anthropic-version: 2023-06-01/);
  assert.match(example.python, /client.messages.create/);
  assert.match(example.python, /max_tokens=256/);
});

test("normalized provider examples select their gateway and provider explicitly", () => {
  for (const target_provider of [
    "deepseek",
    "gemini",
    "groq",
    "ollama",
    "bedrock",
  ]) {
    const example = gatewayConnection(
      { ...gateway, target_provider, target_model: "" },
      "http://localhost:4000",
    );
    assert.equal(example.baseURL, "http://localhost:4000/v1");
    assert.match(example.curl, /X-Bastio-Proxy-Id: gateway-1/);
    assert.ok(example.curl.includes(`X-Bastio-Provider: ${target_provider}`));
    assert.match(example.python, /default_headers=/);
    assert.match(example.python, /YOUR_MODEL_ID/);
  }
});

test("examples escape model IDs in shell payloads and Python strings", () => {
  const example = gatewayConnection(
    { ...gateway, target_model: `model'with"quotes` },
    "https://bastio.example",
  );
  assert.ok(example.curl.includes(`'"'"'`));
  assert.ok(example.python.includes(JSON.stringify(`model'with"quotes`)));
});

test("gateway deep links accept known sections and discard invalid search values", () => {
  assert.deepEqual(
    validateGatewaySearch({ tab: "providers", gateway: "gateway-1" }),
    { tab: "providers", gateway: "gateway-1" },
  );
  assert.deepEqual(validateGatewaySearch({ tab: "unknown", gateway: 42 }), {});
});

test("legacy global API keys with null scopes do not break gateway setup", () => {
  const globalKey = { ...app, scopes: null };
  assert.deepEqual(gatewayAppKeys(gateway, [globalKey]), []);
  assert.equal(
    gatewaySetupStatus(gateway, [provider], [globalKey]),
    "Create app key",
  );
});
