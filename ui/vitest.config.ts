import { defineConfig, mergeConfig } from "vitest/config";
import viteConfig from "./vite.config";

// Vitest reuses the app's Vite config (plugins, the "@" alias, etc.) and
// layers test-only settings on top, so path resolution stays identical
// between `pnpm dev`/`pnpm build` and `pnpm test`.
export default mergeConfig(
  viteConfig,
  defineConfig({
    test: {
      environment: "jsdom",
      globals: false,
      setupFiles: ["./src/test/setup.ts"],
      css: true,
      include: ["src/**/*.test.{ts,tsx}"],
      exclude: ["node_modules", "dist"],
      restoreMocks: true,
      coverage: {
        provider: "v8",
        reporter: ["text", "html", "lcov"],
        include: ["src/**/*.{ts,tsx}"],
        exclude: [
          "src/main.tsx",
          "src/vite-env.d.ts",
          "src/test/**",
          "src/**/*.test.{ts,tsx}",
          "src/**/*.d.ts",
        ],
      },
    },
  }),
);
