/**
 * `gemini_user`: one managed block in `~/.gemini/AGENTS.md` (or under
 * `$GEMINI_CLI_HOME/.gemini`).
 *
 * Antigravity, in the IDE and as `agy`, reads `~/.gemini/AGENTS.md` and
 * `~/.gemini/GEMINI.md` in every session with no frontmatter, and truncates
 * a rule file past 24,000 bytes. Gemini CLI reads it once its
 * `context.fileName` lists AGENTS.md (`memax connect gemini --agents-md`);
 * by default it reads only GEMINI.md. So the block goes in AGENTS.md, never
 * GEMINI.md: Antigravity would read a block in either, and Gemini's own
 * memory tool writes to GEMINI.md. Neither tool expands `@file` into user
 * files reliably (Antigravity's `@file` is a path, not its contents), so
 * the lines are inline. The file is the person's whenever they have one, so
 * Memax always manages one block in it, as for Codex.
 */
import { basename } from "../text.js";
import { home } from "./home.js";

export const geminiUser = home({
  kind: "gemini_user",
  tool: "Antigravity",
  agent: "gemini",
  defaultPath: ".gemini/AGENTS.md",
  pathHint: "ending in .gemini/AGENTS.md",
  acceptsPath: (path) =>
    basename(path) === "AGENTS.md" && path.split("/").at(-2) === ".gemini",
  owner: () => "block",
  limits: { bytes: { value: 24_000, tool: "Antigravity" } },
});
