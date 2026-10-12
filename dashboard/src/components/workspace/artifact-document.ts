// A preview is derived only from a completed, persisted assistant message.
// The in-flight chat draft stays inert plain text until the server finishes
// its output security scan.
export type HtmlArtifact = { html: string; css: string; javascript: string };
import type { CompiledReactArtifact } from "./react-artifact-compile";

const MAX_ARTIFACT_LENGTH = 150_000;
const FENCE = /^```(html|htm|css|javascript|js)[^\n]*\n([\s\S]*?)^```[ \t]*$/gim;

export function extractHtmlArtifact(content: string): HtmlArtifact | null {
  const blocks = [...content.matchAll(FENCE)];
  const html = blocks.find(
    (match) =>
      /^(html|htm)$/i.test(match[1] ?? "") &&
      /<(?:!doctype|html|body|main|div|section|article|svg|canvas|h[1-6]|p|button|form)\b/i.test(match[2] ?? ""),
  );
  if (!html) return null;

  const artifact = {
    html: html[2] ?? "",
    css: blocks
      .filter((match) => /^css$/i.test(match[1] ?? ""))
      .map((match) => match[2] ?? "")
      .join("\n"),
    javascript: blocks
      .filter((match) => /^(javascript|js)$/i.test(match[1] ?? ""))
      .map((match) => match[2] ?? "")
      .join("\n"),
  };
  if (artifact.html.length + artifact.css.length + artifact.javascript.length > MAX_ARTIFACT_LENGTH) {
    return null;
  }
  return artifact;
}

const CSP = [
  "default-src 'none'",
  "script-src 'unsafe-inline'",
  "style-src 'unsafe-inline'",
  "img-src data: blob:",
  "font-src data:",
  "media-src data: blob:",
  "connect-src 'none'",
  "worker-src 'none'",
  "frame-src 'none'",
  "object-src 'none'",
  "form-action 'none'",
  "base-uri 'none'",
  "navigate-to 'none'",
].join("; ");

export function buildArtifactDocument(artifact: HtmlArtifact): string {
  // This wrapper always places our policy before model-generated markup,
  // including when the model supplied an entire <!doctype><html> page.
  // Do not let a model-controlled <head> precede the policy.
  const head = `<meta http-equiv="Content-Security-Policy" content="${CSP}">` +
    (artifact.css ? `<style>${artifact.css.replace(/<\/style/gi, "<\\/style")}</style>` : "");
  const script = artifact.javascript
    ? `<script>${artifact.javascript.replace(/<\/script/gi, "<\\/script")}</script>`
    : "";
  return `<!doctype html><html><head>${head}</head><body>${artifact.html}${script}</body></html>`;
}

function escapeScript(value: string): string {
  return value.replace(/<\/script/gi, "<\\/script");
}

function inlineJSON(value: unknown): string {
  return JSON.stringify(value).replace(/</g, "\\u003c").replace(/\u2028|\u2029/g, " ");
}

export function buildReactArtifactDocument(
  artifact: CompiledReactArtifact,
  reactRuntime: string,
  utilityRuntime: string,
): string {
  const factories = Object.entries(artifact.modules).map(([path, code]) =>
    `${inlineJSON(path)}:function(module,exports,require){\n${escapeScript(code)}\n}`,
  ).join(",\n");
  const bootstrap = `
const factories={${factories}};
const imports=${inlineJSON(artifact.imports)};
const cache=Object.create(null);
const packages=globalThis.__BASTIO_ARTIFACT_MODULES__;
const errorBox=document.getElementById('artifact-error');
function showError(error){
  errorBox.textContent=error && error.message ? error.message : String(error);
  errorBox.hidden=false;
}
window.addEventListener('error',event=>showError(event.error||event.message));
window.addEventListener('unhandledrejection',event=>showError(event.reason));
function load(path){
  if(Object.prototype.hasOwnProperty.call(packages,path)) return packages[path];
  if(Object.prototype.hasOwnProperty.call(cache,path)) return cache[path].exports;
  const factory=factories[path];
  if(!factory) throw new Error('Missing artifact file: '+path);
  const module={exports:{}};
  cache[path]=module;
  factory(module,module.exports,name=>{
    const key=path+'\\u0000'+name;
    const target=imports[key];
    if(!target) throw new Error('Unsupported import '+name+' in '+path);
    if(target.startsWith('css:')) return {};
    return load(target);
  });
  return module.exports;
}
try {
  const entry=load(${inlineJSON(artifact.entry)});
  if(${artifact.autoMount}){
    const Component=entry.default||entry.App||entry;
    if(typeof Component!=='function' && typeof Component!=='object')
      throw new Error('Export a React component as default from ${artifact.entry.replaceAll("'", "")}.');
    packages['react-dom/client'].createRoot(document.getElementById('root'),{
      onUncaughtError:showError,
      onCaughtError:showError,
    }).render(packages.react.createElement(Component));
  }
}catch(error){showError(error);}
`;
  const base = `*{box-sizing:border-box}body{margin:0;min-height:100vh;font-family:Inter,ui-sans-serif,system-ui,sans-serif;color:#17191d;background:#fff}#artifact-error{position:fixed;z-index:9999;inset:auto 12px 12px;padding:12px;border:1px solid #f0b5b5;border-radius:6px;background:#fff2f2;color:#8b2020;font:12px/1.5 ui-monospace,monospace;white-space:pre-wrap;max-height:50vh;overflow:auto}`;
  // Inline all pinned libraries and utility CSS generation. The frame never
  // downloads npm packages, talks to a third-party bundler, or inherits the
  // workspace origin. CSP is first, before any generated source.
  return `<!doctype html><html><head><meta charset="utf-8"><meta http-equiv="Content-Security-Policy" content="${CSP}"><meta name="viewport" content="width=device-width,initial-scale=1"><style>${base}\n${artifact.css.replace(/<\/style/gi, "<\\/style")}</style><script>${escapeScript(reactRuntime)}</script><script>${escapeScript(utilityRuntime)}</script></head><body><div id="root"></div><pre id="artifact-error" hidden></pre><script>${escapeScript(bootstrap)}</script></body></html>`;
}
