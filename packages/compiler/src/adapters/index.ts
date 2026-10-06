/**
 * The adapter registry.
 *
 * The default set is the Phase 1 gate (plan §5.7, D2 and D3): the canonical
 * AGENTS.md, the CLAUDE.md shim, scoped Cursor rules and the ChatGPT copy.
 * The P3 adapters render and parse the same way, and stay out of the default
 * set until their phase.
 */
import type { AdapterKind, TargetSettings } from "../types.js";
import type { Adapter } from "./adapter.js";
import { agentsMd } from "./agents-md.js";
import { chatgpt } from "./chatgpt.js";
import { claudeMd } from "./claude-md.js";
import { claudeRules } from "./claude-rules.js";
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
];

/** The targets compiled when nothing else is configured. */
export const DEFAULT_TARGET_KINDS: readonly AdapterKind[] = [
  "agents_md",
  "claude_md",
  "cursor_mdc",
  "chatgpt",
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

export type { Adapter, Cap, Limits, Role } from "./adapter.js";
