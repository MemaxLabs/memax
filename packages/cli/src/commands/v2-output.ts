// The marks and words the V2 commands print, as on the Cli board:
// ● kept or in sync, ○ waiting on you, ✓ done, ▬ forgotten. In-flight
// work (compiling, not written yet) is ◌, and a stopped target is -.
import { homedir } from "node:os";
import chalk from "chalk";
import type { V2 } from "memax-sdk";

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
    off: "off",
  }[state];
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

/** What a failed call means for the person, and what to do about it. */
export function apiFailureMessage(err: unknown): string {
  const e = err as { status?: number; code?: string; message?: string };
  if (e?.status === 401 || e?.code === "unauthorized")
    return "Sign in first: memax login";
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
