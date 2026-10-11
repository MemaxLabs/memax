/**
 * The adapter registry.
 *
 * The default set is what a new space compiles: the canonical AGENTS.md,
 * which carries path-scoped facts in subsections, the CLAUDE.md shim and
 * the ChatGPT copy. Every scoped rules format is opt-in. Nearly every agent
 * reads AGENTS.md, so a scoped file it also reads (Cursor's `.mdc`, Claude
 * Code's `.claude/rules`, read by VS Code and Copilot CLI too, Copilot's
 * `.github/instructions`, Devin's rules, which Devin CLI reads beside
 * Cursor's and Claude's) would load each scoped fact twice
 * (adapter-load-matrix-2026-10.md). The GEMINI.md shim is opt-in too.
 *
 * The home adapters (`claude_user`, `codex_user`, `gemini_user`,
 * `copilot_user`) write a personal space into one agent's user-level file
 * on a linked machine. They're never defaults: a machine gets the files its
 * person approved.
 */
import type { AdapterKind, TargetSettings } from "../types.js";
import type { Adapter } from "./adapter.js";
import { agentsMd } from "./agents-md.js";
import { chatgpt } from "./chatgpt.js";
import { claudeMd } from "./claude-md.js";
import { claudeRules } from "./claude-rules.js";
import { claudeUser } from "./claude-user.js";
import { codexUser } from "./codex-user.js";
import { copilot } from "./copilot.js";
import { copilotUser } from "./copilot-user.js";
import { cursorMdc } from "./cursor-mdc.js";
import { geminiMd } from "./gemini-md.js";
import { geminiUser } from "./gemini-user.js";
import { windsurf } from "./windsurf.js";

/** Every adapter, in the order results list them. */
export const adapters: readonly Adapter[] = [
  agentsMd,
  claudeMd,
  cursorMdc,
  chatgpt,
  geminiMd,
  copilot,
  windsurf,
  claudeRules,
  claudeUser,
  codexUser,
  geminiUser,
  copilotUser,
];

/** The kinds that write into the person's home folder. */
export const HOME_TARGET_KINDS: readonly AdapterKind[] = adapters
  .filter((a) => a.role === "home")
  .map((a) => a.kind);

/** The targets compiled when nothing else is configured. */
export const DEFAULT_TARGET_KINDS: readonly AdapterKind[] = [
  "agents_md",
  "claude_md",
  "chatgpt",
];

/**
 * Targets compiled only when someone asks for one, never by default, in the
 * adapters' order. The GEMINI.md shim is for people still on Gemini CLI:
 * Antigravity CLI, which replaced it for most people, reads AGENTS.md. The
 * scoped formats load a scoped fact a second time in every agent that also
 * reads AGENTS.md, unless that space's AGENTS.md is set to `scoped: omit`.
 */
export const OPT_IN_TARGET_KINDS: readonly AdapterKind[] = [
  "cursor_mdc",
  "gemini_md",
  "copilot",
  "windsurf",
  "claude_rules",
];

export function defaultTargets(): TargetSettings[] {
  return DEFAULT_TARGET_KINDS.map((kind) => ({ kind }));
}

export function isAdapterKind(value: unknown): value is AdapterKind {
  return adapters.some((a) => a.kind === value);
}

export function getAdapter(kind: AdapterKind): Adapter {
  const adapter = adapters.find((a) => a.kind === kind);
  if (!adapter) throw new Error(`unknown adapter kind: ${kind}`);
  return adapter;
}

export type { Adapter, Cap, Limits, Owner, Role, Root } from "./adapter.js";
export { HOME_BUDGET, PERSONAL_INTRO_LINE } from "./home.js";
