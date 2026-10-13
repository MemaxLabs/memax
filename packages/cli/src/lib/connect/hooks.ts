// The session-start hook, installed in each agent's own hook settings
// (user level, so it runs in every repository; the hook finds the
// repository from the session's directory). Where each agent keeps it and
// the event it runs on are the agents registry's (lib/agents/, from
// @memaxlabs/agents); the entry's shape, checked against each agent's hook
// docs, Oct 2026, is here: Claude Code's and Codex's time out in seconds,
// Gemini CLI's in milliseconds, Cursor's in seconds and Copilot CLI's in
// `timeoutSec`; Codex asks a person to trust a new hook once, in /hooks.
//
// OpenCode, Windsurf and Antigravity CLI have no session-start hook that
// adds context; ChatGPT and VS Code have none at all. Writing is
// idempotent: an entry that already says the same thing is left alone,
// one that differs (an older command) is replaced, and nothing else in
// the file changes.
import { existsSync, mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { AGENTS as REGISTRY } from "../agents/agents.js";
import type { Hook } from "../agents/types.js";
import { parseJsoncObject, setJsonc } from "../jsonc.js";

export type HookOutcome =
  | "written"
  | "present"
  /** The Memax plugin for Claude Code runs the hook. */
  | "plugin"
  | "unsupported"
  | { error: string };

export interface HookSupport {
  /** Where the hook goes, under the home directory; null when the agent has none. */
  file: string | null;
  /** What to tell a person about it. */
  note?: string;
}

const MARK = "memax hook session-start";

/** The command an agent runs: nothing at all where the CLI isn't installed. */
export function hookCommand(agent: string): string {
  return `command -v memax >/dev/null 2>&1 && memax hook session-start --agent ${agent} || true`;
}

/** What to tell a person about an agent's hook, beyond where it is. */
const NOTES: Record<string, string> = {
  codex: "Codex asks you to trust it once: run /hooks in Codex",
  opencode:
    "OpenCode has no session-start hook; it reads AGENTS.md and Memax over MCP",
  windsurf:
    "Windsurf has no session-start hook; it reads its rules and Memax over MCP",
  chatgpt: "ChatGPT has no hooks; it reads Memax through its connector",
};

/** The agents registry's hook for an agent, if it has one. */
function hookOf(kind: string): Hook | null {
  return REGISTRY.find((a) => a.id === kind)?.hook ?? null;
}

export function hookSupport(kind: string): HookSupport {
  const hook = hookOf(kind);
  const note = NOTES[kind];
  if (hook)
    return { file: join(...hook.file.split("/")), ...(note ? { note } : {}) };
  return {
    file: null,
    note: note ?? "this agent has no session-start hook Memax can use",
  };
}

type Json = Record<string, unknown>;

/** The file's text, or "" when it isn't there. */
function readText(path: string): string {
  return existsSync(path) ? readFileSync(path, "utf8") : "";
}

/** The settings, comments allowed; null when they aren't JSON (not ours to rewrite: say so instead). */
function readJson(path: string): Json | null {
  try {
    return parseJsoncObject(readText(path));
  } catch {
    return null;
  }
}

const mentions = (v: unknown) => JSON.stringify(v ?? null).includes(MARK);

/**
 * Puts `entry` in `list` (an event's array of matcher groups, or of
 * hooks), taking out any other Memax hook; a group that held other hooks
 * too keeps them. Returns null when the list already holds exactly
 * `entry` and nothing else of ours.
 */
function upsert(list: unknown, entry: Json): unknown[] | null {
  const items = Array.isArray(list) ? list : [];
  const ours = items.filter(mentions);
  if (ours.length === 1 && JSON.stringify(ours[0]) === JSON.stringify(entry))
    return null;
  const others = items.flatMap((item) => {
    if (!mentions(item)) return [item];
    const g = item as Json;
    const rest = Array.isArray(g.hooks)
      ? g.hooks.filter((h) => !mentions(h))
      : [];
    return rest.length > 0 ? [{ ...g, hooks: rest }] : [];
  });
  return [...others, entry];
}

/** The entry each agent's settings get. */
export function hookEntry(kind: string): { event: string; entry: Json } {
  const command = hookCommand(kind);
  const event = hookOf(kind)?.event ?? "";
  switch (kind) {
    case "claude-code":
    case "codex":
      return {
        event,
        entry: {
          matcher: "startup|resume|clear|compact",
          hooks: [{ type: "command", command, timeout: 10 }],
        },
      };
    case "gemini-cli":
      return {
        event,
        entry: {
          matcher: "*",
          hooks: [{ name: "memax", type: "command", command, timeout: 10_000 }],
        },
      };
    case "cursor":
      return { event, entry: { command, timeout: 10 } };
    case "copilot":
      return {
        event,
        entry: { type: "command", bash: command, timeoutSec: 10 },
      };
    default:
      throw new Error(`no session-start hook for ${kind}`);
  }
}

/** Whether Claude Code has the Memax plugin on (it runs the hook and the MCP server). */
export function hasClaudePlugin(home: string): boolean {
  const settings = readJson(join(home, ".claude", "settings.json"));
  const enabled = settings?.enabledPlugins;
  if (!enabled || typeof enabled !== "object") return false;
  return Object.entries(enabled as Json).some(
    ([id, on]) => on === true && id.split("@")[0] === "memax",
  );
}

/** Whether the agent's settings already run the Memax hook. */
export function hookInstalled(kind: string, home: string): boolean {
  const s = hookSupport(kind);
  if (!s.file) return false;
  const json = readJson(join(home, s.file));
  return !!json && mentions(json.hooks);
}

/** Installs (or updates) the agent's session-start hook. */
export function installHook(kind: string, home: string): HookOutcome {
  const s = hookSupport(kind);
  if (!s.file) return "unsupported";
  if (kind === "claude-code" && hasClaudePlugin(home)) return "plugin";
  const path = join(home, s.file);
  const text = readText(path);
  const json = parseJsoncObject(text);
  if (!json)
    return {
      error: `${path} isn't valid JSON; fix it, or add the hook by hand (docs.memax.app/cli/hook)`,
    };
  const { event, entry } = hookEntry(kind);
  const hooks =
    json.hooks && typeof json.hooks === "object" && !Array.isArray(json.hooks)
      ? (json.hooks as Json)
      : {};
  const next = upsert(hooks[event], entry);
  if (!next) return "present";
  // Only the event's list (and Cursor's and Copilot's version) change:
  // the person's comments and other settings stay as they are.
  let out = setJsonc(text, ["hooks", event], next);
  if ((kind === "cursor" || kind === "copilot") && json.version === undefined)
    out = setJsonc(out, ["version"], 1);
  try {
    mkdirSync(dirname(path), { recursive: true });
    writeFileSync(path, out);
  } catch (err) {
    return { error: (err as Error).message };
  }
  return "written";
}

/** Where the hook lives, for people: `~/.claude/settings.json`. */
export function hookFileLabel(kind: string): string | null {
  const s = hookSupport(kind);
  return s.file ? `~/${s.file.split(/[\\/]/).join("/")}` : null;
}
