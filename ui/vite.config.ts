import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import { VitePWA } from "vite-plugin-pwa";
import path from "path";

export default defineConfig({
  plugins: [
    react(),
    tailwindcss(),
    // F11 — installable PWA + Web Push notifications. injectManifest (not
    // generateSW) because src/sw.ts owns its own "push"/"notificationclick"
    // handlers alongside the precached app shell — generateSW only knows
    // how to author a caching-only worker, with no room for our own event
    // listeners.
    VitePWA({
      strategies: "injectManifest",
      srcDir: "src",
      filename: "sw.ts",
      injectManifest: {
        // Precache the built app shell only (JS/CSS/HTML, self-hosted
        // @fontsource fonts, and the icons below) — never "/api/*",
        // "/ws/*", or "/_/" (PocketBase's own routes), which live outside
        // dist/ entirely and are therefore never matched by this glob.
        globPatterns: ["**/*.{js,css,html,svg,png,woff,woff2,ico,webmanifest}"],
      },
      registerType: "prompt",
      // manifest.webmanifest and sw.js are served by the Go hub itself
      // (cmd/hub/main.go) with explicit content types — no injected
      // <link>/<script> tags needed from this plugin.
      injectRegister: false,
      manifest: {
        name: "NexWatch",
        short_name: "NexWatch",
        description: "Self-hosted server fleet monitoring: metrics, alerts, and uptime checks.",
        start_url: "/",
        scope: "/",
        display: "standalone",
        // DESIGN.md §1: --color-void (page background) and the same value
        // for theme_color, since this is a dark-only app with no light
        // variant to accent against — matches index.html's own dark body
        // background instead of introducing a second, unused brand color.
        background_color: "#0a0e16",
        theme_color: "#0a0e16",
        icons: [
          { src: "pwa-64x64.png", sizes: "64x64", type: "image/png" },
          { src: "pwa-192x192.png", sizes: "192x192", type: "image/png" },
          { src: "pwa-512x512.png", sizes: "512x512", type: "image/png" },
          {
            src: "maskable-icon-512x512.png",
            sizes: "512x512",
            type: "image/png",
            purpose: "maskable",
          },
        ],
      },
      devOptions: {
        // The dev server proxies /api and /ws to the local hub (below) and
        // has no "dist" build to precache from, so the SW stays off in
        // dev — the offline banner and install-prompt hint still work,
        // since neither depends on an active service worker.
        enabled: false,
      },
    }),
  ],
  resolve: {
    alias: {
      "@": path.resolve(__dirname, "./src"),
    },
  },
  server: {
    port: 5173,
    proxy: {
      "/api": {
        target: "http://localhost:8090",
        changeOrigin: true,
      },
      "/ws": {
        target: "ws://localhost:8090",
        ws: true,
      },
      "/_": {
        target: "http://localhost:8090",
        changeOrigin: true,
      },
    },
  },
  build: {
    outDir: "dist",
    emptyOutDir: true,
    rollupOptions: {
      output: {
        // Split large, infrequently-changing vendor code into its own
        // cacheable chunks so a deploy that only touches app code doesn't
        // force re-downloading React/uPlot/PocketBase.
        manualChunks: {
          "vendor-react": ["react", "react-dom", "react-router-dom"],
          "vendor-uplot": ["uplot"],
          "vendor-pocketbase": ["pocketbase"],
        },
      },
    },
  },
});
