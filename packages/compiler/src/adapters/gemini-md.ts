/**
 * `gemini_md`: the Gemini CLI shim (P3, not in the default set).
 *
 * Gemini CLI doesn't read AGENTS.md unless configured to, but it resolves
 * `@./file.md` imports (five levels deep, inside the project root). So
 * GEMINI.md becomes `@./AGENTS.md` plus Gemini-only lines.
 */
import { shim } from "./shim.js";

export const geminiMd = shim({
  kind: "gemini_md",
  tool: "Gemini CLI",
  agent: "gemini",
  defaultPath: "GEMINI.md",
  fileNames: ["GEMINI.md"],
  importLine: (relative) =>
    `@${relative.startsWith("../") ? "" : "./"}${relative}`,
  limits: {},
});
