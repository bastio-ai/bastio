// A preview is derived only from a completed, persisted assistant message.
// The in-flight chat draft stays inert plain text until the server finishes
// its output security scan.
export type HtmlArtifact = { html: string; css: string; javascript: string };

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
