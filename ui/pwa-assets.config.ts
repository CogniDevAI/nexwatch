import { defineConfig, minimal2023Preset } from "@vite-pwa/assets-generator/config";

// Generates the PWA icon set (F11) from public/favicon.svg — the app's
// existing brand mark (DESIGN.md §1's void/signal palette) — using the
// library's "minimal2023" preset: 64/192/512 transparent PNGs, a
// favicon.ico, a maskable 512 icon, and a 180px Apple touch icon.
//
// Run `pnpm exec pwa-assets-generator` after changing favicon.svg to
// regenerate; the resulting PNGs are committed under public/ so the
// production build never depends on this generator (or its sharp native
// dependency) at build or runtime — see vite.config.ts's manifest.icons
// list, which references these committed files directly.
export default defineConfig({
  headLinkOptions: {
    preset: "2023",
  },
  preset: minimal2023Preset,
  images: ["public/favicon.svg"],
});
