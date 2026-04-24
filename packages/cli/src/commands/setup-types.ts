import { existsSync } from "node:fs";
import { join, dirname } from "node:path";
import { homedir, platform } from "node:os";
import { execSync } from "node:child_process";

// --- Shared interfaces ---

export interface AgentDef {
  name: string;
  id: string;
  configPath: string; // global MCP config file
  format: "json-mcpServers" | "json-servers" | "toml";
  /** Key under which MCP servers live */
  mcpKey: string;
  hasHooks: boolean;
  /** Global instruction file path (e.g. ~/.claude/CLAUDE.md) — null if none */
  globalInstructionFile: string | null;
  detect: () => boolean; // is this agent likely installed?
  /**
   * Returns the correct MCP server entry shape for this agent's remote config.
   * Each agent has its own schema expectations for remote/HTTP MCP servers.
   * The `authHeaders` param is undefined for OAuth mode (agent handles auth)
   * or an object with Authorization header for API key mode.
   */
  remoteEntry: (
    mcpUrl: string,
    authHeaders?: Record<string, string>,
  ) => Record<string, unknown>;
  /**
   * Returns the correct MCP server entry shape for local stdio mode.
   * Most agents use { command, args }, but some (OpenCode) need a type field.
   * If undefined, the default { command, args } shape is used.
   */
  localEntry?: (command: string, args: string[]) => Record<string, unknown>;
}

export interface MemaxBin {
  command: string;
  args: string[];
  shell: string;
}

// --- Shared utilities ---

export function commandExists(cmd: string): boolean {
  try {
    const which = platform() === "win32" ? "where" : "which";
    execSync(`${which} ${cmd}`, { stdio: "pipe" });
    return true;
  } catch {
    return false;
  }
}

export function resolveMemaxBin(): MemaxBin | null {
  // 1. Local repo build (preferred during dev so setup/hooks use the same CLI code being tested)
  const localBuild = join(process.cwd(), "packages", "cli", "dist", "index.js");
  if (existsSync(localBuild)) {
    return {
      command: "node",
      args: [localBuild],
      shell: `node ${localBuild}`,
    };
  }

  // 2. Global install — use absolute path so agents find it without shell PATH
  if (commandExists("memax")) {
    try {
      const which = platform() === "win32" ? "where memax" : "which memax";
      const absPath = execSync(which, { encoding: "utf-8", stdio: "pipe" })
        .trim()
        .split("\n")[0];
      if (absPath) {
        return { command: absPath, args: [], shell: absPath };
      }
    } catch {
      // fall through
    }
    return { command: "memax", args: [], shell: "memax" };
  }

  // 3. npx as last resort (slow startup — agents may timeout on first run)
  try {
    execSync("npx --yes memax-cli --version", {
      encoding: "utf-8",
      timeout: 15000,
      stdio: "pipe",
    });
    return {
      command: "npx",
      args: ["-y", "memax-cli"],
      shell: "npx -y memax-cli",
    };
  } catch {
    // npx failed
  }

  return null;
}
