import { test } from "node:test";
import assert from "node:assert/strict";
import { findingExplanation } from "./finding-explanation.ts";

const finding = { threat_type: "pii", detector_name: "pii", matched_pattern: "ssn", action_taken: "log_only" };

test("ambiguous government identifiers are potential matches, not verified people", () => {
  const ssn = findingExplanation(finding);
  assert.equal(ssn.title, "Possible US Social Security number");
  assert.match(ssn.description, /product or reference numbers/);
  assert.match(ssn.description, /does not confirm personal data/);
  assert.match(ssn.outcome, /did not block or rewrite/);
  assert.equal(ssn.direction, undefined);
  const sin = findingExplanation({ ...finding, matched_pattern: "canadian_sin" });
  assert.match(sin.description, /checksum/);
  assert.match(sin.description, /Other numeric identifiers/);
});

test("explains the policy saved with the call, without inventing a reason for legacy events", () => {
  const omitted = findingExplanation(finding, { tags: { "bastio.content_capture": "metadata_only" } });
  assert.match(omitted.evidence, /because this call used metadata-only/);
  assert.match(omitted.evidence, /cannot be recovered/);
  const legacy = findingExplanation(finding, { trace_name: "plendio.card_refresh" });
  assert.match(legacy.evidence, /did not record why/);
  assert.doesNotMatch(legacy.evidence, /because this call used/);
  const captureEnabled = findingExplanation(finding, { tags: { "bastio.content_capture": "enabled" } });
  assert.match(captureEnabled.evidence, /No matched excerpt/);
});

test("a retained excerpt is shown by the evidence view, not described as missing", () => {
  assert.equal(findingExplanation({ ...finding, matched_content: "***-**-6789" }).evidence, undefined);
  assert.ok(findingExplanation({ ...finding, matched_content: "  " }).evidence);
});

test("source provenance is not guessed to be the input or response", () => {
  assert.equal(findingExplanation({ ...finding, source: "input" }).direction, "AI request");
  assert.equal(findingExplanation({ ...finding, source: "response" }).direction, "AI response");
  assert.equal(findingExplanation({ ...finding, source: "tool" }).direction, "Tool content");
  assert.equal(findingExplanation({ ...finding, source: "retrieval" }).direction, "Retrieved content");
  assert.equal(findingExplanation({ ...finding, source: "memory" }).direction, "Memory content");
  assert.equal(findingExplanation({ ...finding, source: "pattern" }).direction, undefined);
  assert.equal(findingExplanation({ ...finding, source: "toString" }).direction, undefined);
});

test("non-PII detectors and classifier-specific patterns get accurate generic explanations", () => {
  assert.equal(findingExplanation({ ...finding, detector_name: "jailbreak", threat_type: "jailbreak" }).title, "Security finding to review");
  assert.equal(findingExplanation({ ...finding, detector_name: "privacy-filter", matched_pattern: "classifier-hash" }).title, "Possible personal data");
  assert.match(findingExplanation({ ...finding, action_taken: "block" }).outcome, /blocked or withheld/);
});
