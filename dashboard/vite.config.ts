import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import path from "path";
import { execSync } from "node:child_process";

/**
 * Resolve a build SHA from CI env vars or the local git tree.
 * Order: GITHUB_SHA → VERCEL_GIT_COMMIT_SHA → `git rev-parse --short HEAD` → "dev".
 */
function getBuildSha(): string {
  if (process.env.GITHUB_SHA) return process.env.GITHUB_SHA.slice(0, 7);
  if (process.env.VERCEL_GIT_COMMIT_SHA) return process.env.VERCEL_GIT_COMMIT_SHA.slice(0, 7);
  try {
    return execSync("git rev-parse --short HEAD", { encoding: "utf-8" }).trim();
  } catch {
    return "dev";
  }
}

export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: {
      "@": path.resolve(__dirname, "./src"),
    },
  },
  define: {
    "import.meta.env.VITE_BUILD_SHA": JSON.stringify(getBuildSha()),
  },
  server: {
    port: 3000,
    proxy: {
      "/v1": "http://localhost:4000",
      "/health": "http://localhost:4000",
    },
  },
});
