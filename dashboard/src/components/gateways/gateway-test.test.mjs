import {
  gatewayModelChoices,
  defaultGatewayTestModel,
  matchesGatewayModel,
  listedGatewayModelChoices,
  defaultListedGatewayModel,
} from "./gateway-models.ts";
import assert from "node:assert/strict";
import test from "node:test";
import {
  buildGatewayTestRequest,
  gatewayTestFailure,
  gatewayTestReply,
  gatewayTraceId,
} from "./gateway-test.ts";
const proxy = {
  id: "00000000-0000-0000-0000-000000000001",
  target_provider: "openai",
};
test("hello uses the dashboard session and cannot supply credentials or arbitrary prompts", () => {
  const request = buildGatewayTestRequest(
    proxy,
    " gpt-example ",
    "https://cloud.bastio.com/",
  );
  assert.equal(
    request.url,
    `https://cloud.bastio.com/v1/proxies/${proxy.id}/test`,
  );
  assert.deepEqual(JSON.parse(request.body), { model: "gpt-example" });
  assert.equal(request.headers["Cache-Control"], "no-store");
  assert.equal(request.headers.Authorization, undefined);
});
test("replies are extracted across provider formats without treating reasoning-only output as a reply", () => {
  for (const body of [
    { choices: [{ message: { content: "Hello" } }] },
    { content: [{ type: "text", text: "Hello" }] },
    { candidates: [{ content: { parts: [{ text: "Hello" }] } }] },
    { message: { content: "Hello" } },
    { answers: { greeting: { noul: "Hello" } } },
    { output: { message: { content: [{ text: "Hello" }] } } },
  ])
    assert.equal(gatewayTestReply(body), "Hello");
  assert.equal(
    gatewayTestReply({
      choices: [{ message: { reasoning_content: "thinking" } }],
    }),
    "",
  );
  assert.equal(
    gatewayTestReply({ content: [{ type: "thinking", thinking: "thinking" }] }),
    "",
  );
});
test("auth, usage, entitlement, and security failures are distinct", () => {
  assert.equal(
    gatewayTestFailure(401, { error: "invalid api key" }).title,
    "Authentication failed",
  );
  assert.match(
    gatewayTestFailure(401, { error: "invalid api key" }).detail,
    /provider key/,
  );
  assert.match(
    gatewayTestFailure(402, { error: { message: "Credits exhausted" } }).detail,
    /Credits exhausted/,
  );
  assert.equal(
    gatewayTestFailure(403, {
      error: "feature_not_in_tier",
      message: "Requires Enterprise",
    }).title,
    "Your plan does not include this feature",
  );
  assert.equal(
    gatewayTestFailure(403, {
      error: { type: "security_blocked", message: "Policy rejected request" },
    }).title,
    "Security policy blocked the test",
  );
});
test("trace links accept only real trace UUIDs", () => {
  assert.equal(gatewayTraceId(proxy.id, {}), proxy.id);
  assert.equal(
    gatewayTraceId(null, { bastio: { trace_id: proxy.id } }),
    proxy.id,
  );
  assert.equal(gatewayTraceId("../../bad", {}), undefined);
});

test("the model picker scopes suggestions to the gateway provider and preserves configured defaults", () => {
  const openRouter = gatewayModelChoices("openrouter", "tenant/special-model");
  assert.ok(openRouter.length > 1);
  assert.ok(openRouter.every((choice) => choice.provider === "openrouter"));
  assert.equal(openRouter[0].model, "tenant/special-model");
  const claude = gatewayModelChoices("anthropic");
  assert.ok(claude.some((choice) => /Claude/.test(choice.label)));
  assert.ok(!claude.some((choice) => choice.legacy));
  assert.equal(
    defaultGatewayTestModel("openrouter", " tenant/model "),
    "tenant/model",
  );
  assert.ok(
    gatewayModelChoices("openai").some(
      (choice) => choice.model === defaultGatewayTestModel("openai"),
    ),
  );
  assert.equal(defaultGatewayTestModel("unknown"), "");
});

test("model search accepts human names and IDs without mixing providers", () => {
  const routerClaude = gatewayModelChoices("openrouter").find((choice) =>
    choice.label.includes("Claude Sonnet"),
  );
  assert.ok(matchesGatewayModel(routerClaude, "  CLAUDE SONNET "));
  assert.ok(matchesGatewayModel(routerClaude, routerClaude.model));
  assert.ok(!matchesGatewayModel(routerClaude, "Gemini"));
});

test("model discovery never offers or auto-selects an unavailable catalogue model", () => {
  const choices = listedGatewayModelChoices({
    provider: "openai",
    source: "provider",
    models: [{ id: "gpt-4o" }, { id: "gpt-4o-mini" }],
  });
  assert.deepEqual(
    choices.map((choice) => choice.model),
    ["gpt-4o", "gpt-4o-mini"],
  );
  assert.equal(defaultListedGatewayModel(choices, "gpt-6-luna"), "gpt-4o-mini");
  assert.equal(defaultListedGatewayModel(choices, "gpt-4o"), "gpt-4o");
  assert.equal(defaultListedGatewayModel([]), "");
});
test("provider model errors explain why replacing a Bastio key cannot help", () => {
  const failure = gatewayTestFailure(403, {
    error: {
      code: "model_not_found",
      message: "Project example does not have access to model gpt-6-luna",
    },
  });
  assert.equal(failure.title, "Your provider key cannot use this model");
  assert.match(failure.detail, /new Bastio API key will not change it/);
});

test("hello defaults prefer small non-reasoning models regardless of provider list order", () => {
  const choices = listedGatewayModelChoices({
    provider: "openai",
    source: "provider",
    models: [
      { id: "gpt-5-nano" },
      { id: "gpt-4o-mini" },
      { id: "gpt-4.1-nano" },
    ],
  });
  assert.equal(choices[0].label, "GPT-5 Nano");
  assert.equal(defaultListedGatewayModel(choices), "gpt-4.1-nano");
  assert.equal(defaultListedGatewayModel(choices, "gpt-5-nano"), "gpt-5-nano");
});
