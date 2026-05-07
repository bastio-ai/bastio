import { defineConfig } from "vitest/config";
import { resolve } from "node:path";

export default defineConfig({
  resolve: {
    alias: {
      "@bastio/core": resolve(__dirname, "../core/src/index.ts"),
    },
  },
});
