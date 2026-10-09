// ESLint flat config for @memaxlabs/ledger.
//
// Same shape as packages/web: @typescript-eslint/parser as a parser only,
// plus the react-hooks rules. Types come from `tsc --noEmit` (strict, with
// noUnusedLocals and noUncheckedIndexedAccess); formatting comes from
// Prettier. The Ledger visual rules (no literal colours, the voice rules
// over the string catalogues) are enforced by tests in src/test/, because
// they read CSS and data rather than syntax.
import reactHooks from "eslint-plugin-react-hooks";
import tsParser from "@typescript-eslint/parser";

export default [
  {
    ignores: ["node_modules/**", "dist/**"],
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
    linterOptions: {
      reportUnusedDisableDirectives: "error",
    },
    plugins: {
      "react-hooks": reactHooks,
    },
    rules: {
      "react-hooks/rules-of-hooks": "error",
      "react-hooks/exhaustive-deps": "error",
      "no-console": "error",
      "no-debugger": "error",
      eqeqeq: ["error", "always", { null: "ignore" }],
    },
  },
];
