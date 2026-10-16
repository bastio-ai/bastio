import { test } from "node:test";
import assert from "node:assert/strict";
import { emptyFilters, parseTraceSearch, traceFiltersFromSearch, traceFiltersToSearch, matchesTraceFilters, traceType, parseTagFilter, mergeTraceRows } from "./trace-filters.ts";
import { parseRefreshInterval, shouldRefreshDashboard } from "../dashboard-refresh-policy.ts";

const trace = { id: "one", trace_name: "plendio.card_refresh", provider: "openrouter", model: "model-a", path: "/v1/openrouter/chat/completions", status: "ok", threat_detected: true, environment: "production", release: "v1", end_user_id: "peter", started_at: "2026-10-16T10:00:00Z", tags: { feature: "catalogue" } };

test("combined filters apply identically to operational traces and placeholder rows", () => {
  const filters = { ...emptyFilters, traceName: "CARD", provider: "openrouter", model: "model-a", traceType: "llm", security: "threat", environment: "production", endUser: "peter", release: "v1", tags: "feature:catalogue", from: "2026-10-16T09:00:00Z", to: "2026-10-16T11:00:00Z" };
  assert.equal(matchesTraceFilters(trace, filters), true);
  for (const [field, value] of Object.entries({ provider: "mcp", model: "other", traceType: "embedding", security: "clean", environment: "development", endUser: "adam", release: "v2", tags: "feature:checkout", from: "2026-10-16T12:00:00Z", to: "2026-10-16T08:00:00Z" })) {
    assert.equal(matchesTraceFilters(trace, { ...filters, [field]: value }), false, field);
  }
  assert.equal(matchesTraceFilters({ ...trace, status: "error" }, { ...emptyFilters, status: "error,blocked,rate_limited" }), true);
  assert.equal(matchesTraceFilters(trace, { ...emptyFilters, status: "error,blocked,rate_limited" }), false);
});

test("browser activity cannot bypass name, provider, kind or search filters", () => {
  const browser = { ...trace, trace_name: "browser.example.test", provider: "browser-extension", path: "/v1/governance/events", environment: "browser-extension" };
  assert.equal(matchesTraceFilters(browser, { ...emptyFilters, traceType: "extension" }), true);
  assert.equal(matchesTraceFilters(browser, { ...emptyFilters, provider: "openrouter" }), false);
  assert.equal(matchesTraceFilters(browser, { ...emptyFilters, traceName: "card_refresh" }), false);
  assert.equal(matchesTraceFilters(browser, { ...emptyFilters, search: "unrelated" }), false);
});

test("trace categories distinguish embeddings, local AI, tools and imported records", () => {
  assert.equal(traceType(trace), "llm");
  assert.equal(traceType({ ...trace, path: "/v1/openrouter/embeddings" }), "embedding");
  assert.equal(traceType({ ...trace, provider: "ollama", path: "local/observe" }), "local");
  assert.equal(traceType({ ...trace, provider: "mcp" }), "mcp");
  assert.equal(traceType({ ...trace, provider: "" }), "other");
});

test("filters round-trip through URLs and invalid URL types cannot crash the page", () => {
  const selected = { ...emptyFilters, traceName: "plendio.card", provider: "openrouter", traceType: "embedding", tags: "feature:catalogue" };
  assert.deepEqual(traceFiltersFromSearch(traceFiltersToSearch(selected)), selected);
  assert.deepEqual(parseTraceSearch({ traceType: "invalid", security: "maybe", from: "not-a-date", provider: ["bad"], search: 42 }), {});
  assert.deepEqual(parseTagFilter(":bad,empty:,feature:catalogue"), ["feature:catalogue"]);
  assert.deepEqual(parseTraceSearch({ from: "2026-10-16T10:00:00+02:00", tags: "invalid,feature:catalogue" }), { from: "2026-10-16T08:00:00.000Z", tags: "feature:catalogue" });
});

test("traces with equal timestamps keep a deterministic order across sources", () => {
  const a = { ...trace, id: "a" }, b = { ...trace, id: "b" };
  assert.deepEqual(mergeTraceRows([a], [b]).map((row) => row.id), ["b", "a"]);
  assert.deepEqual(mergeTraceRows([b], [a]).map((row) => row.id), ["b", "a"]);
});

test("refresh defaults to one minute and never ticks while paused or hidden", () => {
  assert.equal(parseRefreshInterval(""), 60_000);
  assert.equal(parseRefreshInterval("10000"), 60_000);
  assert.equal(parseRefreshInterval("120000"), 120_000);
  assert.equal(shouldRefreshDashboard(true, "visible", 10_000, 60_000), false);
  assert.equal(shouldRefreshDashboard(true, "visible", 60_000, 60_000), true);
  assert.equal(shouldRefreshDashboard(false, "visible", 100_000, 60_000), false);
  assert.equal(shouldRefreshDashboard(true, "hidden", 100_000, 60_000), false);
});
