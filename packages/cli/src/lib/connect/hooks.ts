// The session-start hook, installed in each agent's own hook settings
// (user level, so it runs in every repository; the hook finds the
// repository from the session's directory). Shapes checked against each
// agent's hook docs, Oct 2026 (docs-site cli/hook.mdx has the links):
//
// - Claude Code: ~/.claude/settings.json hooks.SessionStart (seconds).
// - Codex: ~/.codex/hooks.json hooks.SessionStart (seconds); Codex asks a
//   person to trust a new hook once, in /hooks.
// - Gemini CLI: ~/.gemini/settings.json hooks.SessionStart (milliseconds).
// - Cursor: ~/.cursor/hooks.json hooks.sessionStart (seconds).
// - Copilot CLI: ~/.copilot/hooks/memax.json hooks.sessionStart (timeoutSec).
//
// OpenCode, Windsurf and Antigravity CLI have no session-start hook that
// adds context; ChatGPT and VS Code have none at all. Writing is
// idempotent: an entry that already says the same thing is left alone,
// one that differs (an older command) is replaced, and nothing else in
// the file changes.
import { existsSync, mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { dirname, join } from "node:path";

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

const SUPPORT: Record<string, HookSupport> = {
  "claude-code": { file: join(".claude", "settings.json") },
  codex: {
    file: join(".codex", "hooks.json"),
    note: "Codex asks you to trust it once: run /hooks in Codex",
  },
  "gemini-cli": { file: join(".gemini", "settings.json") },
  cursor: { file: join(".cursor", "hooks.json") },
  copilot: { file: join(".copilot", "hooks", "memax.json") },
  opencode: {
    file: null,
    note: "OpenCode has no session-start hook; it reads AGENTS.md and Memax over MCP",
  },
  windsurf: {
    file: null,
    note: "Windsurf has no session-start hook; it reads its rules and Memax over MCP",
  },
  chatgpt: {
    file: null,
    note: "ChatGPT has no hooks; it reads Memax through its connector",
  },
};

export function hookSupport(kind: string): HookSupport {
  return (
    SUPPORT[kind] ?? {
      file: null,
      note: "this agent has no session-start hook Memax can use",
    }
  );
}

type Json = Record<string, unknown>;

function readJson(path: string): Json | null {
  if (!existsSync(path)) return {};
  try {
    const v: unknown = JSON.parse(readFileSync(path, "utf8"));
    return v && typeof v === "object" && !Array.isArray(v) ? (v as Json) : null;
  } catch {
    return null; // not ours to rewrite: say so instead
  }
}

function writeJson(path: string, value: Json): void {
  mkdirSync(dirname(path), { recursive: true });
  writeFileSync(path, `${JSON.stringify(value, null, 2)}\n`);
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
  switch (kind) {
    case "claude-code":
    case "codex":
      return {
        event: "SessionStart",
        entry: {
          matcher: "startup|resume|clear|compact",
          hooks: [{ type: "command", command, timeout: 10 }],
        },
      };
    case "gemini-cli":
      return {
        event: "SessionStart",
        entry: {
          matcher: "*",
          hooks: [{ name: "memax", type: "command", command, timeout: 10_000 }],
        },
      };
    case "cursor":
      return { event: "sessionStart", entry: { command, timeout: 10 } };
    case "copilot":
      return {
        event: "sessionStart",
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
  const json = readJson(path);
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
  const out: Json = { ...json };
  if (kind === "cursor" || kind === "copilot") out.version ??= 1;
  out.hooks = { ...hooks, [event]: next };
  try {
    writeJson(path, out);
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
