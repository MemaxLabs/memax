/**
 * `gemini_md`: the Gemini CLI shim, opt-in (OPT_IN_TARGET_KINDS): never in
 * the default set. Antigravity CLI, which replaced Gemini CLI for most
 * people, reads AGENTS.md itself.
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
