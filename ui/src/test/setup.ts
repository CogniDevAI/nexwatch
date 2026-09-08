// Registers jest-dom's DOM matchers (toBeInTheDocument, toHaveTextContent,
// etc.) on Vitest's `expect`, including the TypeScript augmentation for
// vitest's Assertion interface. Loaded once via vitest.config.ts's
// `test.setupFiles`, before every test file.
import "@testing-library/jest-dom/vitest";
import { cleanup } from "@testing-library/react";
import { afterEach } from "vitest";

// React Testing Library does not auto-cleanup outside of Jest's global
// afterEach, so it must be wired up manually here since this project uses
// vitest's explicit imports (no injected globals).
afterEach(() => {
  cleanup();
});

// jsdom has no matchMedia implementation. uPlot (src/lib/uplotHelpers.ts,
// src/components/charts/MetricChart.tsx) calls window.matchMedia at module
// import time to detect device pixel ratio changes, so any test that
// transitively imports a chart component throws "matchMedia is not a
// function" without this polyfill — even one that never actually renders
// the chart. Defined here (not per-test-file) since it must exist before
// uPlot's module-level code runs, which happens at import time.
if (!window.matchMedia) {
  window.matchMedia = (query: string) => ({
    matches: false,
    media: query,
    onchange: null,
    addListener: () => {},
    removeListener: () => {},
    addEventListener: () => {},
    removeEventListener: () => {},
    dispatchEvent: () => false,
  });
}
