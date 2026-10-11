/**
 * `copilot_user`: one managed block in Copilot's user instructions,
 * `~/.copilot/copilot-instructions.md` (or under `$COPILOT_HOME`).
 *
 * Copilot CLI loads that file in every session, in every repository
 * (`copilot instruction list` names it as a personal source), and VS Code
 * reads it in its Copilot harness. It has no frontmatter, and Copilot
 * doesn't expand `@file` outside the repository, so the lines are inline.
 * The file is the person's whenever they have one, so Memax always manages
 * one block in it. Memax never writes into `~/.copilot/instructions/`,
 * whose modular files are the person's own.
 *
 * VS Code's Local harness also reads `~/.claude/rules`, so on a machine with
 * Claude Code's file too it loads the Personal space twice; its Copilot
 * harness doesn't. The CLI says so when it links both (lib/home/files.ts).
 */
import { basename } from "../text.js";
import { home } from "./home.js";

export const copilotUser = home({
  kind: "copilot_user",
  tool: "Copilot",
  agent: "copilot",
  defaultPath: ".copilot/copilot-instructions.md",
  pathHint: "ending in copilot-instructions.md, in Copilot's folder",
  acceptsPath: (path) =>
    basename(path) === "copilot-instructions.md" &&
    path.includes("/") &&
    !path.startsWith(".github/"),
  owner: () => "block",
  limits: {},
});
