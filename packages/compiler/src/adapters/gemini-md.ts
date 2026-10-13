/**
 * `gemini_md`: the Gemini CLI shim, opt-in (OPT_IN_TARGET_KINDS): never in
 * the default set. Antigravity CLI, which replaced Gemini CLI for most
 * people, reads AGENTS.md itself.
 *
 * Gemini CLI doesn't read AGENTS.md unless configured to, but it resolves
 * `@./file.md` imports (five levels deep, inside the project root). So
 * GEMINI.md becomes `@./AGENTS.md` plus Gemini-only lines.
 */
import { agent } from "@memaxlabs/agents";
import { shim } from "./shim.js";

const GEMINI = agent("gemini-cli");

export const geminiMd = shim({
  kind: "gemini_md",
  tool: GEMINI.name,
  agent: GEMINI.key,
  defaultPath: "GEMINI.md",
  fileNames: ["GEMINI.md"],
  importLine: (relative) =>
    `@${relative.startsWith("../") ? "" : "./"}${relative}`,
  limits: {},
});
