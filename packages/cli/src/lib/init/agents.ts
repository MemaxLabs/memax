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
import { AGENTS as REGISTRY } from "../agents/agents.js";

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

/**
 * The agents of the registry (lib/agents/, from @memaxlabs/agents) that
 * memax init and memax connect know, in its order. `setupId` is its short
 * key, the id memax setup's MCP writer goes by, for an agent with MCP
 * settings on this machine.
 */
export const AGENTS: AgentEntry[] = REGISTRY.filter((a) => a.cli).map((a) => ({
  kind: a.id,
  setupId: a.mcp ? a.key : null,
  name: a.name,
  where: a.where,
  homeDirs: a.detect.home.map((d) => join(...d.split("/"))),
  repoDirs: [...a.detect.repo],
  commands: [...a.detect.commands],
  start: a.start,
  ...(a.connector ? { connector: true } : {}),
}));

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
