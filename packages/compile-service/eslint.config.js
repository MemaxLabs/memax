// ESLint flat config for @memaxlabs/compiler.
//
// Small on purpose: the recommended JavaScript rules, read through the
// TypeScript parser. Types are checked by `tsc --noEmit` (with
// noUnusedLocals and noUnusedParameters), so the rules tsc already covers
// are off here. Style is Prettier's job.
import js from "@eslint/js";
import tsParser from "@typescript-eslint/parser";

export default [
  { ignores: ["dist/**", "node_modules/**"] },
  js.configs.recommended,
  {
    files: ["**/*.{ts,js}"],
    languageOptions: {
      parser: tsParser,
      parserOptions: { ecmaVersion: "latest", sourceType: "module" },
    },
    rules: {
      // tsc reports undefined names and unused bindings, with type awareness.
      "no-undef": "off",
      "no-unused-vars": "off",
      // A library never writes to the console; callers decide what to show.
      "no-console": "error",
      eqeqeq: ["error", "always"],
      "prefer-const": "error",
    },
  },
];
