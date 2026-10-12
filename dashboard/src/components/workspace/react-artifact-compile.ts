import * as esbuild from "esbuild-wasm";
import wasmURL from "esbuild-wasm/esbuild.wasm?url";

import { resolveArtifactImport, type ReactArtifact } from "./react-artifact";

export type CompiledReactArtifact = {
  entry: string;
  modules: Record<string, string>;
  imports: Record<string, string>;
  css: string;
  autoMount: boolean;
};

let initializePromise: Promise<void> | null = null;

async function ensureCompiler() {
  initializePromise ??= esbuild.initialize({ wasmURL, worker: true }).catch((error: unknown) => {
    initializePromise = null;
    throw error;
  });
  await initializePromise;
}

export async function compileReactArtifact(artifact: ReactArtifact): Promise<CompiledReactArtifact> {
  await ensureCompiler();
  const modules: Record<string, string> = {};
  const imports: Record<string, string> = {};
  for (const [path, file] of Object.entries(artifact.files)) {
    try {
      const result = await esbuild.transform(file.code, {
        loader: file.loader,
        format: "cjs",
        jsx: "automatic",
        jsxImportSource: "react",
        target: "es2020",
        sourcefile: path,
      });
      modules[path] = result.code;
      if (path === artifact.entry && !/\bexport\s+default\b/.test(file.code)) {
        const component = /\b(?:function|class|const|let)\s+App\b/.test(file.code)
          ? "App"
          : file.code.match(/\b(?:function|class|const|let)\s+([A-Z][a-z][A-Za-z0-9_]*)\b/)?.[1];
        if (component) modules[path] += `\nmodule.exports.default = ${component};\n`;
      }
      for (const match of result.code.matchAll(/\brequire\(["']([^"']+)["']\)/g)) {
        const specifier = match[1];
        if (!specifier) continue;
        const resolved = resolveArtifactImport(path, specifier, artifact.files);
        if (!resolved) {
          throw new Error(`Unsupported import ${JSON.stringify(specifier)} in ${path}. Supported packages: react, lucide-react, recharts, framer-motion, date-fns.`);
        }
        imports[`${path}\u0000${specifier}`] = resolved;
      }
    } catch (error) {
      const message = error instanceof Error ? error.message : String(error);
      throw new Error(`Could not build ${path}: ${message}`);
    }
  }
  const entryCode = artifact.files[artifact.entry]?.code ?? "";
  return {
    entry: artifact.entry,
    modules,
    imports,
    css: artifact.css,
    autoMount: !/(?:^|\/)(?:main|index)\.(?:tsx|jsx|ts|js)$/i.test(artifact.entry) &&
      !/\bcreateRoot\s*\(/.test(entryCode),
  };
}
