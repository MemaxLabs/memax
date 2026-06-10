// ESLint flat config for @memaxlabs/web.
//
// Intentionally minimal: this exists specifically to enforce the
// react-hooks rules that catch the whole class of "rendered fewer
// hooks than expected" runtime crashes (e.g. the settings-dialog
// Intelligence tab meltdown from commit 62787bcc — hook order drift
// between renders threw inside React and escalated into the
// segment error boundary).
//
// We pull in @typescript-eslint/parser as a PARSER ONLY — no
// typescript-eslint rules are enabled. The parser is required
// because the default ESLint parser (Espree) can't read TS syntax
// (type annotations, generics, `interface`), and the react-hooks
// rules need real AST coverage of every .ts/.tsx file to work.
// Type-layer correctness still comes from `tsc --noEmit`; style
// stays with Prettier.
//
// If the `react-hooks` rules stop being enough, the right move is
// to add specific rules one by one, not adopt a whole preset.
import reactHooks from "eslint-plugin-react-hooks";
import tsParser from "@typescript-eslint/parser";

// Shell plugin: registers rule names as accepted no-ops so existing
// `/* eslint-disable-next-line @typescript-eslint/no-unused-vars */`
// style directives scattered through the codebase don't crash lint
// with "Definition for rule X was not found". The disable directives
// predate any ESLint wiring; registering shells preserves author
// intent without pulling in the full rule sets (each of which would
// surface dozens of new violations unrelated to the hooks focus of
// this config). If we later want real enforcement for any of these,
// swap the shell for the real plugin — the directives will already
// be in place.
function shellPlugin(ruleNames) {
  const rules = {};
  for (const name of ruleNames) {
    rules[name] = {
      meta: { type: "problem", schema: [] },
      create: () => ({}),
    };
  }
  return { rules };
}

const typescriptEslintShell = shellPlugin([
  "no-explicit-any",
  "no-unused-vars",
]);
const nextNextShell = shellPlugin(["no-img-element"]);
const importShell = shellPlugin(["first"]);

export default [
  {
    ignores: [
      ".next/**",
      "node_modules/**",
      "dist/**",
      "build/**",
      "public/**",
      "next-env.d.ts",
    ],
  },
  {
    files: ["**/*.{ts,tsx,js,jsx,mjs,cjs}"],
    languageOptions: {
      parser: tsParser,
      parserOptions: {
        ecmaVersion: "latest",
        sourceType: "module",
        ecmaFeatures: { jsx: true },
      },
    },
    // reportUnusedDisableDirectives=off because the existing
    // directives were written for an ESLint setup that never
    // actually ran (see package.json history — `lint` was just
    // `tsc --noEmit`). Half of them wouldn't fire today because
    // the rules aren't on; escalating those to errors would
    // create a churn list disconnected from the reason we're
    // adding ESLint (the hooks rule).
    linterOptions: {
      reportUnusedDisableDirectives: "off",
    },
    plugins: {
      "react-hooks": reactHooks,
      "@typescript-eslint": typescriptEslintShell,
      "@next/next": nextNextShell,
      import: importShell,
    },
    rules: {
      // Error — hook ordering bugs render as confusing "Rendered
      // fewer hooks than expected" runtime crashes that escape
      // TypeScript entirely. This was the root cause of the
      // YouIntelligence / DreamHistory blank-page bug; the rule
      // catches the exact pattern (early return between hook
      // calls) at lint time instead.
      "react-hooks/rules-of-hooks": "error",
      // Warn — missing-dependency warnings are a pattern signal,
      // not a hard bug. Escalating to error would block most
      // legitimate `useEffect` callbacks that intentionally don't
      // list every reference.
      "react-hooks/exhaustive-deps": "warn",
    },
  },
];
