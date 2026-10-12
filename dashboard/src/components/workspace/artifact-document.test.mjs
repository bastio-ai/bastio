import assert from "node:assert/strict";
import test from "node:test";

import { buildArtifactDocument, buildReactArtifactDocument, extractHtmlArtifact } from "./artifact-document.ts";
import { extractReactArtifact, resolveArtifactImport } from "./react-artifact.ts";

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

test("selects a React entry and resolves local component imports", () => {
  const artifact = extractReactArtifact([
    "```tsx src/Counter.tsx",
    "export default function Counter() { return <button>Count</button>; }",
    "```",
    "```tsx src/App.tsx",
    "import Counter from './Counter'; export default function App() { return <Counter />; }",
    "```",
    "```css src/app.css",
    "button { color: blue; }",
    "```",
  ].join("\n"));
  assert.ok(artifact);
  assert.equal(artifact.entry, "src/App.tsx");
  assert.equal(resolveArtifactImport(artifact.entry, "./Counter", artifact.files), "src/Counter.tsx");
  assert.equal(resolveArtifactImport(artifact.entry, "./app.css", artifact.files), "css:src/app.css");
  assert.match(artifact.css, /color: blue/);
  assert.equal(resolveArtifactImport(artifact.entry, "../../secret", artifact.files), null);
});

test("React preview embeds local runtime and puts CSP ahead of generated code", () => {
  const document = buildReactArtifactDocument({
    entry: "App.tsx",
    modules: { "App.tsx": "module.exports.default = function App(){ return null; };" },
    imports: {}, css: "body{color:blue}", autoMount: true,
  }, "globalThis.__BASTIO_ARTIFACT_MODULES__={};", "/* utilities */");
  assert.ok(document.indexOf("Content-Security-Policy") < document.indexOf("module.exports.default"));
  assert.match(document, /connect-src 'none'/);
  assert.match(document, /frame-src 'none'/);
  assert.match(document, /body\{color:blue\}/);
  assert.doesNotMatch(document, /https?:\/\//);
});
