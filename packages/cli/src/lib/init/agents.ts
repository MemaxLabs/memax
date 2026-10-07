// The agents memax init looks for on this machine (plan 25 §7.3 step 1):
// one entry per agent, with how to tell it is installed, where it keeps
// its things (for people to read), the id `memax setup` writes its MCP
// config by, and the autonomy it starts at in a new space (step 3:
// Propose, with Cursor and Gemini CLI at Read, as the Connect board
// draws). Detection only looks for directories and commands on PATH;
// nothing runs an agent or reads its private formats.
import { existsSync, statSync } from "node:fs";
import { delimiter, join } from "node:path";
import type { V2 } from "memax-sdk";

export interface AgentEntry {
  kind: V2.AgentKind;
  /** The id `memax setup` knows it by (its MCP writer), if it has one. */
  setupId: string | null;
  name: string;
  /** Where people see it lives: "~/.claude", ".cursor/ in this repo". */
  where: string;
  /** Directories under the home directory that mean it is installed. */
  homeDirs: string[];
  /** Directories in the repository that mean it is used here. */
  repoDirs: string[];
  /** Commands on PATH that mean it is installed. */
  commands: string[];
  /** The level it starts at in a space; a person raises it later. */
  start: V2.Autonomy;
  /** ChatGPT connects through its connector, in the browser. */
  connector?: boolean;
}

export const AGENTS: AgentEntry[] = [
  {
    kind: "claude-code",
    setupId: "claude-code",
    name: "Claude Code",
    where: "~/.claude",
    homeDirs: [".claude"],
    repoDirs: [],
    commands: ["claude"],
    start: "propose",
  },
  {
    kind: "codex",
    setupId: "codex",
    name: "Codex",
    where: "~/.codex",
    homeDirs: [".codex"],
    repoDirs: [],
    commands: ["codex"],
    start: "propose",
  },
  {
    kind: "cursor",
    setupId: "cursor",
    name: "Cursor",
    where: "~/.cursor",
    homeDirs: [".cursor"],
    repoDirs: [".cursor"],
    commands: ["cursor", "cursor-agent"],
    start: "read",
  },
  {
    kind: "gemini-cli",
    setupId: "gemini",
    name: "Gemini CLI",
    where: "~/.gemini",
    homeDirs: [".gemini"],
    repoDirs: [],
    commands: ["gemini"],
    start: "read",
  },
  {
    kind: "copilot",
    setupId: "copilot",
    name: "Copilot",
    where: "~/.copilot",
    homeDirs: [".copilot"],
    repoDirs: [],
    commands: ["copilot"],
    start: "propose",
  },
  {
    kind: "opencode",
    setupId: "opencode",
    name: "OpenCode",
    where: "~/.config/opencode",
    homeDirs: [join(".config", "opencode")],
    repoDirs: [".opencode"],
    commands: ["opencode"],
    start: "propose",
  },
  {
    kind: "windsurf",
    setupId: "windsurf",
    name: "Windsurf",
    where: "~/.codeium/windsurf",
    homeDirs: [join(".codeium", "windsurf")],
    repoDirs: [".windsurf"],
    commands: ["windsurf"],
    start: "propose",
  },
  {
    kind: "chatgpt",
    setupId: null,
    name: "ChatGPT",
    where: "connector",
    homeDirs: [],
    repoDirs: [],
    commands: [],
    start: "propose",
    connector: true,
  },
];

export interface DetectEnv {
  home: string;
  /** The repository's root, when init runs in one. */
  root: string | null;
  /** PATH, to look commands up without running anything. */
  path: string;
  platform: NodeJS.Platform;
}

export interface DetectedAgent {
  agent: AgentEntry;
  found: boolean;
  /** What showed it: "~/.claude", "claude on PATH", ".cursor/ in this repo". */
  evidence: string;
}

function isDir(p: string): boolean {
  try {
    return statSync(p).isDirectory();
  } catch {
    return false;
  }
}

/** Whether a command is on PATH, by looking, not running. */
export function onPath(
  cmd: string,
  path: string,
  platform: NodeJS.Platform,
): boolean {
  const exts = platform === "win32" ? [".exe", ".cmd", ".bat", ""] : [""];
  for (const dir of path.split(platform === "win32" ? ";" : delimiter)) {
    if (!dir) continue;
    for (const ext of exts) {
      if (existsSync(join(dir, cmd + ext))) return true;
    }
  }
  return false;
}

/** Every known agent, and whether it is on this machine (or used here). */
export function detectAgents(env: DetectEnv): DetectedAgent[] {
  return AGENTS.map((agent) => {
    for (const d of agent.homeDirs) {
      if (isDir(join(env.home, d)))
        return {
          agent,
          found: true,
          evidence: `~/${d.split(/[\\/]/).join("/")}`,
        };
    }
    if (env.root) {
      for (const d of agent.repoDirs) {
        if (isDir(join(env.root, d)))
          return { agent, found: true, evidence: `${d}/ in this repo` };
      }
    }
    for (const c of agent.commands) {
      if (onPath(c, env.path, env.platform))
        return { agent, found: true, evidence: `${c} on PATH` };
    }
    return { agent, found: false, evidence: "" };
  });
}
