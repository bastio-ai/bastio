export type ReactArtifact = {
  entry: string;
  files: Record<string, { code: string; loader: "tsx" | "jsx" | "ts" | "js" }>;
  css: string;
};

export const ARTIFACT_MODULES = new Set([
  "react",
  "react/jsx-runtime",
  "react-dom",
  "react-dom/client",
  "lucide-react",
  "recharts",
  "framer-motion",
  "date-fns",
  "@/components/ui/button",
  "@/components/ui/card",
  "@/components/ui/badge",
  "@/components/ui/input",
  "@/components/ui/textarea",
  "@/components/ui/separator",
  "@/components/ui/progress",
  "@/components/ui/alert",
  "@/components/ui/tabs",
  "@/components/ui/table",
]);

const MAX_SOURCE_LENGTH = 150_000;
const CODE_FENCE = /^```([^\n]*)\n([\s\S]*?)^```[ \t]*$/gim;
const FILE_NAME = /(?:^|[\s:=/])(?:file(?:name)?=["']?)?([\w./-]+\.(?:tsx|jsx|ts|js|css))["']?(?:\s|$)/i;

function normalizePath(value: string): string | null {
  const parts: string[] = [];
  for (const segment of value.replaceAll("\\", "/").replace(/^\/+/, "").split("/")) {
    if (!segment || segment === ".") continue;
    if (segment === "..") {
      if (!parts.length) return null;
      parts.pop();
    } else {
      parts.push(segment);
    }
  }
  return parts.join("/") || null;
}

function loaderFor(info: string, file: string | null, code: string): ReactArtifact["files"][string]["loader"] | null {
  const extension = file?.split(".").at(-1)?.toLowerCase();
  const language = info.toLowerCase().match(/(?:^|\s|:)(tsx|jsx|typescript|javascript|ts|js)(?:\s|:|$)/)?.[1];
  const value = extension ?? language;
  if (!value) return null;
  if (value === "tsx") return "tsx";
  if (value === "jsx") return "jsx";
  const hasJSX = /<[A-Za-z][\w.:-]*(?:\s|>|\/)/.test(code);
  if (value === "ts" || value === "typescript") return hasJSX ? "tsx" : "ts";
  return hasJSX ? "jsx" : "js";
}

export function extractReactArtifact(content: string): ReactArtifact | null {
  const blocks = [...content.matchAll(CODE_FENCE)];
  const files: ReactArtifact["files"] = {};
  const css: string[] = [];
  let unnamed = 0;
  let total = 0;
  for (const block of blocks) {
    const info = (block[1] ?? "").trim();
    const code = block[2] ?? "";
    total += code.length;
    if (total > MAX_SOURCE_LENGTH) return null;
    const path = normalizePath(info.match(FILE_NAME)?.[1] ?? "");
    if (/^(css|scss)(?:\s|$)/i.test(info) || path?.endsWith(".css")) {
      css.push(code);
      continue;
    }
    const loader = loaderFor(info, path, code);
    if (!loader) continue;
    const name = path ?? (unnamed++ === 0 ? "App.tsx" : `Snippet${unnamed}.tsx`);
    files[name] = { code, loader };
  }

  const names = Object.keys(files);
  if (!names.length) return null;
  const hasComponent = names.some((name) =>
    /<[A-Za-z][\w.:-]*(?:\s|>|\/)/.test(files[name]?.code ?? ""),
  );
  if (!hasComponent) return null;

  // Prefer an explicit app entry, then a standalone component. If the
  // model provided an index.tsx that mounts itself, run it as the entry.
  const entry = names.find((name) => /(?:^|\/)(?:main|index)\.(?:tsx|jsx|ts|js)$/i.test(name)) ??
    names.find((name) => /(?:^|\/)App\.(?:tsx|jsx|ts|js)$/i.test(name)) ??
    names.find((name) => /export\s+default\b/.test(files[name]?.code ?? "")) ??
    names[0];
  if (!entry) return null;
  return { entry, files, css: css.join("\n") };
}

export function resolveArtifactImport(
  from: string,
  specifier: string,
  files: ReactArtifact["files"],
): string | null {
  if (ARTIFACT_MODULES.has(specifier)) return specifier;
  if (!specifier.startsWith(".")) return null;
  const directory = from.slice(0, Math.max(0, from.lastIndexOf("/") + 1));
  const base = normalizePath(directory + specifier);
  if (!base) return null;
  for (const candidate of [base, `${base}.tsx`, `${base}.jsx`, `${base}.ts`, `${base}.js`,
    `${base}/index.tsx`, `${base}/index.jsx`, `${base}/index.ts`, `${base}/index.js`]) {
    if (candidate in files) return candidate;
  }
  if (base.endsWith(".css")) return "css:" + base;
  return null;
}
