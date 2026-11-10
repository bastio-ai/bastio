import assert from "node:assert/strict";
import test from "node:test";
import {
  GET_STARTED_DISMISS_KEY,
  deriveGetStarted,
  firstRequestSnippets,
  readDismissed,
  shouldShowGetStarted,
  withApiKey,
  writeDismissed,
} from "./getting-started.ts";

const gateway = (overrides = {}) => ({
  id: "gw-1",
  name: "Support",
  slug: "support",
  listen_path: "/support",
  target_provider: "openai",
  target_model: "gpt-example",
  is_active: true,
  created_at: "2026-11-01T00:00:00Z",
  ...overrides,
});
const providerKey = { id: "pk-1", provider: "openai", is_default: true };
const appKey = (proxyId, overrides = {}) => ({
  id: `ak-${proxyId}`,
  name: "app",
  is_active: true,
  scopes: [`proxy:${proxyId}`],
  ...overrides,
});
const empty = { providerKeys: [], managedProviders: [], proxies: [], apiKeys: [], traceCount: 0 };

test("brand-new tenant starts at the provider step", () => {
  const state = deriveGetStarted(empty);
  assert.deepEqual(state.done, { provider: false, gateway: false, request: false });
  assert.equal(state.current, "provider");
  assert.equal(state.completed, 0);
  assert.equal(state.gateway, null);
  assert.equal(shouldShowGetStarted(state, false), true);
});

test("provider key moves to the gateway step", () => {
  const state = deriveGetStarted({ ...empty, providerKeys: [providerKey] });
  assert.equal(state.current, "gateway");
  assert.equal(state.completed, 1);
});

test("managed providers and server-auth gateways count as a connected provider", () => {
  assert.equal(deriveGetStarted({ ...empty, managedProviders: ["openai"] }).done.provider, true);
  assert.equal(
    deriveGetStarted({ ...empty, proxies: [gateway({ target_provider: "ollama" })] }).done.provider,
    true,
  );
});

test("gateway without an active key keeps step 2 open and targets that gateway", () => {
  const gw = gateway();
  const state = deriveGetStarted({
    ...empty,
    providerKeys: [providerKey],
    proxies: [gw],
    apiKeys: [appKey(gw.id, { is_active: false })],
  });
  assert.equal(state.current, "gateway");
  assert.equal(state.gateway?.id, gw.id);
});

test("disabled gateways do not complete step 2", () => {
  const gw = gateway({ is_active: false });
  const state = deriveGetStarted({ ...empty, providerKeys: [providerKey], proxies: [gw], apiKeys: [appKey(gw.id)] });
  assert.equal(state.done.gateway, false);
  assert.equal(state.gateway, null);
});

test("gateway with a key waits for the first request and prefers the keyed gateway", () => {
  const keyed = gateway({ id: "gw-old", created_at: "2026-10-01T00:00:00Z" });
  const newer = gateway({ id: "gw-new", created_at: "2026-11-05T00:00:00Z" });
  const state = deriveGetStarted({
    ...empty,
    providerKeys: [providerKey],
    proxies: [newer, keyed],
    apiKeys: [appKey(keyed.id)],
  });
  assert.equal(state.current, "request");
  assert.equal(state.completed, 2);
  assert.equal(state.gateway?.id, "gw-old");
});

test("any trace hides the guide, even when other data looks incomplete", () => {
  const state = deriveGetStarted({ ...empty, traceCount: 1 });
  assert.equal(state.current, null);
  assert.equal(state.completed, 3);
  assert.equal(shouldShowGetStarted(state, false), false);
});

test("dismissal hides an incomplete guide", () => {
  assert.equal(shouldShowGetStarted(deriveGetStarted(empty), true), false);
});

test("snippets use the placeholder and the gateway origin; withApiKey fills every occurrence", () => {
  const snippets = firstRequestSnippets(gateway(), "https://api.bastio.com");
  const curlTargets = snippets.curl
    .split(/\s+/)
    .map((token) => token.replace(/^['"]|['"]$/g, ""))
    .filter((token) => token.startsWith("https://"));
  assert.deepEqual(curlTargets, ["https://api.bastio.com/v1/guard/gw-1/v1/chat/completions"]);
  assert.match(snippets.curl, /\$BASTIO_API_KEY/);
  assert.match(snippets.sdk, /from openai import OpenAI/);
  assert.equal(snippets.sdkLabel, "OpenAI SDK");
  const filled = withApiKey(snippets.curl, "bst_secret");
  assert.doesNotMatch(filled, /\$BASTIO_API_KEY/);
  assert.match(filled, /Bearer bst_secret/);
  assert.equal(firstRequestSnippets(gateway({ target_provider: "anthropic" }), "http://localhost:4000").sdkLabel, "Anthropic SDK");
});

test("dismissal storage round-trips and survives throwing storage", () => {
  const values = new Map();
  const storage = { getItem: (k) => values.get(k) ?? null, setItem: (k, v) => values.set(k, v) };
  assert.equal(readDismissed(() => storage), false);
  writeDismissed(() => storage);
  assert.equal(values.get(GET_STARTED_DISMISS_KEY), "1");
  assert.equal(readDismissed(() => storage), true);

  const throwing = {
    getItem: () => {
      throw new Error("blocked");
    },
    setItem: () => {
      throw new Error("blocked");
    },
  };
  assert.equal(readDismissed(() => throwing), false);
  assert.doesNotThrow(() => writeDismissed(() => throwing));
  assert.equal(readDismissed(() => undefined), false);
  assert.equal(
    readDismissed(() => {
      throw new Error("SecurityError");
    }),
    false,
  );
});
