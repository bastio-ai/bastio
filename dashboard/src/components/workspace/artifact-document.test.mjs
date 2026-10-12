import assert from "node:assert/strict";
import test from "node:test";

import { buildArtifactDocument, extractHtmlArtifact } from "./artifact-document.ts";

test("combines a generated HTML page with CSS and JavaScript", () => {
  const artifact = extractHtmlArtifact([
    "```html",
    "<!doctype html><html><head><title>Card</title></head><body><button id=go>Go</button></body></html>",
    "```",
    "```css",
    "button { color: red; }",
    "```",
    "```javascript",
    "document.querySelector('#go').textContent = 'Done';",
    "```",
  ].join("\n"));

  assert.ok(artifact);
  const document = buildArtifactDocument(artifact);
  assert.ok(document.indexOf("Content-Security-Policy") < document.indexOf("<title>Card"));
  assert.match(document, /connect-src 'none'/);
  assert.match(document, /<style>button \{ color: red; \}\s*<\/style>/);
  assert.match(document, /document\.querySelector\('#go'\)/);
});

test("never offers a preview for code-only or oversized output", () => {
  assert.equal(extractHtmlArtifact("```js\nalert(1)\n```"), null);
  assert.equal(extractHtmlArtifact(`\`\`\`html\n<div>${"x".repeat(150_000)}</div>\n\`\`\``), null);
});

test("a model-controlled head cannot run before the preview policy", () => {
  const document = buildArtifactDocument({
    html: "<!doctype html><html><head><script>window.bad=true</script></head><body>Hi</body></html>",
    css: "",
    javascript: "",
  });
  assert.ok(document.indexOf("Content-Security-Policy") < document.indexOf("window.bad"));
  assert.match(document, /default-src 'none'/);
  assert.match(document, /form-action 'none'/);
});
