import js from "@eslint/js";
import globals from "globals";
import reactHooks from "eslint-plugin-react-hooks";
import reactRefresh from "eslint-plugin-react-refresh";
import jsxA11y from "eslint-plugin-jsx-a11y";
import tseslint from "typescript-eslint";

export default tseslint.config(
  { ignores: ["dist", "node_modules", "coverage"] },
  {
    extends: [
      js.configs.recommended,
      ...tseslint.configs.recommendedTypeChecked,
      reactRefresh.configs.vite,
      jsxA11y.flatConfigs.recommended,
    ],
    files: ["**/*.{ts,tsx}"],
    languageOptions: {
      ecmaVersion: 2020,
      globals: globals.browser,
      parserOptions: {
        // sw.ts needs its own project (tsconfig.sw.json): it targets the
        // "WebWorker" lib instead of "DOM", which TypeScript cannot mix
        // into a single program with the rest of the app (see that
        // config's own comment).
        project: ["./tsconfig.eslint.json", "./tsconfig.sw.json"],
        tsconfigRootDir: import.meta.dirname,
      },
    },
    plugins: {
      "react-hooks": reactHooks,
    },
    rules: {
      // eslint-plugin-react-hooks@7's packaged "recommended"/"recommended-latest"
      // presets ship the full React Compiler rule family (set-state-in-effect,
      // purity, immutability, ...), which assumes code is written for the
      // compiler's memoization model. This codebase does not use React
      // Compiler, and that family flags many ordinary, correct patterns (e.g.
      // calling an async fetch function inside useEffect). Enabling only the
      // two classic, framework-agnostic hook-correctness rules instead.
      "react-hooks/rules-of-hooks": "error",
      "react-hooks/exhaustive-deps": "warn",

      // typescript-eslint's recommendedTypeChecked "no-unsafe-*" family
      // (assignment/member-access/call/argument/return) fires primarily on
      // boundaries with loosely-typed third-party data: PocketBase SDK
      // records/errors and raw fetch() JSON payloads. Properly typing every
      // such boundary is real, broader work tracked separately, not a
      // mechanical/behavior-preserving lint fix, so these are disabled here
      // rather than silenced with dozens of scattered per-line comments.
      "@typescript-eslint/no-unsafe-assignment": "off",
      "@typescript-eslint/no-unsafe-member-access": "off",
      "@typescript-eslint/no-unsafe-call": "off",
      "@typescript-eslint/no-unsafe-argument": "off",
      "@typescript-eslint/no-unsafe-return": "off",

      // React event handlers (onClick, onSubmit, ...) are allowed to be
      // async; React ignores the returned promise by design, so this is not
      // an unhandled-rejection risk in the way non-JSX void callbacks are.
      "@typescript-eslint/no-misused-promises": [
        "error",
        { checksVoidReturn: { attributes: false } },
      ],
    },
  },
);
