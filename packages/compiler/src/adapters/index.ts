/**
 * The adapter registry.
 *
 * The default set is the Phase 1 gate (plan §5.7, D2 and D3): the canonical
 * AGENTS.md, the CLAUDE.md shim, scoped Cursor rules and the ChatGPT copy.
 * The P3 adapters render and parse the same way, and stay out of the default
 * set until their phase. The GEMINI.md shim is opt-in for good: compiled
 * only for a space that asks for it.
 *
 * The home adapters (`claude_user`, `codex_user`) write a personal space
 * into one agent's user-level file on a linked machine. They're never
 * defaults: a machine gets the files its person approved there.
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
import { cursorMdc } from "./cursor-mdc.js";
import { geminiMd } from "./gemini-md.js";
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
];

/** The kinds that write into the person's home folder. */
export const HOME_TARGET_KINDS: readonly AdapterKind[] = adapters
  .filter((a) => a.role === "home")
  .map((a) => a.kind);

/** The targets compiled when nothing else is configured. */
export const DEFAULT_TARGET_KINDS: readonly AdapterKind[] = [
  "agents_md",
  "claude_md",
  "cursor_mdc",
  "chatgpt",
];

/**
 * Targets compiled only when someone asks for one, never by default. The
 * GEMINI.md shim is for people still on Gemini CLI: Antigravity CLI, which
 * replaced it for most people, reads AGENTS.md, and a shim beside it could
 * make it load the record twice.
 */
export const OPT_IN_TARGET_KINDS: readonly AdapterKind[] = ["gemini_md"];

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
