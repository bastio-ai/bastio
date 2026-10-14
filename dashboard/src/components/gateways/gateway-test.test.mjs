import assert from "node:assert/strict";
import test from "node:test";
import {
  buildGatewayTestRequest,
  gatewayTestFailure,
  gatewayTestReply,
  gatewayTraceId,
  helloPrompt,
} from "./gateway-test.ts";
const proxy = {
  id: "00000000-0000-0000-0000-000000000001",
  target_provider: "openai",
};
test("hello goes through the real scoped route with a bounded non-streaming request", () => {
  const request = buildGatewayTestRequest(
    proxy,
    "gpt-example",
    "https://cloud.bastio.com/",
  );
  assert.equal(
    request.url,
    `https://cloud.bastio.com/v1/guard/${proxy.id}/v1/chat/completions`,
  );
  assert.equal(request.headers["Cache-Control"], "no-store");
  const body = JSON.parse(request.body);
  assert.equal(body.max_completion_tokens, 128);
  assert.equal(body.stream, false);
  assert.equal(body.messages[0].content, helloPrompt);
  assert.ok(!JSON.stringify(request).includes("sk-"));
});
test("Claude and normalized providers use their correct protocol and explicit routing", () => {
  const claude = buildGatewayTestRequest(
    { ...proxy, target_provider: "anthropic" },
    "claude-example",
    "https://bastio.example",
  );
  assert.ok(claude.url.endsWith("/v1/messages"));
  assert.equal(claude.headers["anthropic-version"], "2023-06-01");
  assert.equal(JSON.parse(claude.body).max_tokens, 128);
  const gemini = buildGatewayTestRequest(
    { ...proxy, target_provider: "gemini" },
    "gemini-example",
    "https://bastio.example",
  );
  assert.equal(gemini.headers["X-Bastio-Proxy-Id"], proxy.id);
  assert.equal(gemini.headers["X-Bastio-Provider"], "gemini");
});
test("replies are extracted across provider formats without treating reasoning-only output as a reply", () => {
  for (const body of [
    { choices: [{ message: { content: "Hello" } }] },
    { content: [{ type: "text", text: "Hello" }] },
    { candidates: [{ content: { parts: [{ text: "Hello" }] } }] },
    { message: { content: "Hello" } },
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
    /Bastio API key/,
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
