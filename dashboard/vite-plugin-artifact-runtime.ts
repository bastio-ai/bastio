import { build } from "esbuild";
import { readFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";

const dashboardDir = path.dirname(fileURLToPath(import.meta.url));
const virtualID = "virtual:bastio-artifact-runtime";
const resolvedID = "\0" + virtualID;

// Build a standalone, pinned runtime during each app build. Importing the
// virtual module is lazy, so normal workspace visits do not load this code.
// The same plugin is used by OSS, cloud dashboard, and employee workspace.
export function artifactRuntimePlugin() {
  return {
    name: "bastio-artifact-runtime",
    resolveId(id: string) {
      return id === virtualID ? resolvedID : null;
    },
    async load(id: string) {
      if (id !== resolvedID) return null;
      const result = await build({
        entryPoints: [path.join(dashboardDir, "artifact-runtime-entry.ts")],
        bundle: true,
        platform: "browser",
        format: "iife",
        target: "es2020",
        minify: true,
        write: false,
        absWorkingDir: dashboardDir,
      });
      const reactCode = result.outputFiles[0]?.text;
      if (!reactCode) throw new Error("Artifact runtime build produced no output");
      const utilityCode = await readFile(
        path.join(dashboardDir, "node_modules/@unocss/runtime/uno.global.js"),
        "utf8",
      );
      return `export const reactRuntime = ${JSON.stringify(reactCode)};\n` +
        `export const utilityRuntime = ${JSON.stringify(utilityCode)};`;
    },
  };
}
