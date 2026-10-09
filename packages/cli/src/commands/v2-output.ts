// The marks and words the V2 commands print, as on the Cli board:
// ● kept or in sync, ○ waiting on you, ✓ done, ▬ forgotten. In-flight
// work (compiling, not written yet) is ◌, and a stopped target is -.
import { homedir } from "node:os";
import chalk from "chalk";
import type { V2 } from "memax-sdk";
import { holdDetail } from "../lib/daemon/holds.js";

export const MARK = {
  kept: "●",
  waiting: "○",
  done: "✓",
  forgotten: "▬",
  working: "◌",
  off: "-",
} as const;

export function syncMark(state: V2.SyncState): string {
  switch (state) {
    case "in_sync":
      return chalk.green(MARK.kept);
    case "drifted":
    case "held":
      return chalk.yellow(MARK.waiting);
    case "off":
      return chalk.gray(MARK.off);
    default:
      return chalk.gray(MARK.working);
  }
}

export function syncWord(state: V2.SyncState): string {
  return {
    in_sync: "in sync",
    compiling: "compiling",
    pending_delivery: "not written yet",
    drifted: "drifted",
    held: "held",
    off: "off",
  }[state];
}

/** The third column of a target row: when, how many edits, or why. */
export function targetNote(t: V2.Target, now = new Date()): string {
  switch (t.sync_state) {
    case "drifted":
      return t.open_drift === 1
        ? "1 local edit"
        : `${t.open_drift} local edits`;
    case "in_sync":
      if (t.delivery === "copy") return "copy it from the app";
      return clock(t.delivered?.at ?? t.last_compile?.compiled_at, now);
    case "held":
      return holdDetail(t);
    case "pending_delivery":
      return t.delivery === "pr" ? "pull requests aren't available yet" : "";
    default:
      return "";
  }
}

/** `● AGENTS.md   in sync   14:31`, the rows of `memax status`. */
export function targetRows(targets: V2.Target[], now = new Date()): string[] {
  const width = Math.max(16, ...targets.map((t) => t.label.length + 2));
  return [...targets]
    .sort((a, b) => order(a) - order(b) || a.label.localeCompare(b.label))
    .map((t) =>
      `  ${syncMark(t.sync_state)} ${pad(t.label, width)}${pad(syncWord(t.sync_state), 16)}${chalk.gray(targetNote(t, now))}`.trimEnd(),
    );
}

const KIND_ORDER: V2.TargetKind[] = [
  "agents_md",
  "claude_md",
  "gemini_md",
  "cursor_mdc",
  "copilot",
  "windsurf",
  "claude_rules",
  "chatgpt",
];

/** The canonical AGENTS.md, its shims, scoped rules, then the copy-out. */
export function order(t: Pick<V2.Target, "kind">): number {
  const i = KIND_ORDER.indexOf(t.kind);
  return i < 0 ? KIND_ORDER.length : i;
}

/** `14:31` today, `Oct 4` before. */
export function clock(iso: string | undefined, now = new Date()): string {
  if (!iso) return "";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "";
  const sameDay = d.toDateString() === now.toDateString();
  if (sameDay) {
    return `${String(d.getHours()).padStart(2, "0")}:${String(d.getMinutes()).padStart(2, "0")}`;
  }
  return d.toLocaleDateString("en-US", { month: "short", day: "numeric" });
}

/**
 * What a failed call means for the person, and what to do about it.
 * `command` (`memax init`) is the command that failed, when running it
 * again carries on from where it stopped.
 */
export function apiFailureMessage(err: unknown, command?: string): string {
  const e = err as { status?: number; code?: string; message?: string };
  if (e?.status === 401 || e?.code === "unauthorized")
    return "Sign in first: memax login";
  // Still limited after the SDK waited out what it could.
  if (command && (e?.status === 429 || e?.code === "rate_limited"))
    return `Memax is limiting how fast this account sends requests. Wait a minute, then run ${command} again: it carries on from where it stopped.`;
  if (e?.code === "network_error") return e.message ?? "Can't reach Memax.";
  if (e?.status === 404) return e.message ?? "Not found in your spaces.";
  return e?.message ?? String(err);
}

/** The board's two-letter agent marks. */
export function agentMark(agent: V2.AgentKind, name: string): string {
  const marks: Partial<Record<V2.AgentKind, string>> = {
    "claude-code": "CC",
    codex: "CX",
    cursor: "CU",
    chatgpt: "GPT",
    claude: "CL",
    "gemini-cli": "GM",
    copilot: "CP",
    opencode: "OC",
    windsurf: "WS",
  };
  return (
    marks[agent] ??
    (name
      .replace(/[^A-Za-z0-9]/g, "")
      .slice(0, 2)
      .toUpperCase() ||
      "AG")
  );
}

/** Pads to `width` visible characters (marks are one column wide). */
export function pad(s: string, width: number): string {
  return s.length >= width ? s + " " : s + " ".repeat(width - s.length);
}

/** `~/code/memax` for a path under home. */
export function tildify(path: string): string {
  const home = homedir();
  return path === home || path.startsWith(home + "/")
    ? "~" + path.slice(home.length)
    : path;
}
