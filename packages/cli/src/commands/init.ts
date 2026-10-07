// memax init (`npx memax-cli init`): detect the agents and their files,
// sign in, connect the agents, import what they already know as
// proposals, settle what disagrees, and compile (plan 25 §7.3). The flow
// is lib/init/run.ts; this file wires it to the machine.
import { existsSync, readFileSync } from "node:fs";
import { homedir } from "node:os";
import { join } from "node:path";
import chalk from "chalk";
import type { Command } from "commander";
import { getClient, usesAPIKey } from "../lib/client.js";
import { loadCredentials } from "../lib/credentials.js";
import { daemonPaths } from "../lib/daemon/paths.js";
import { runGit } from "../lib/init/git.js";
import { runInit } from "../lib/init/run.js";
import type { AgentEntry } from "../lib/init/agents.js";
import type {
  CompileOutcome,
  InitDeps,
  InitOptions,
  McpOutcome,
} from "../lib/init/types.js";
import { ask, confirm, confirmDefault } from "../lib/prompt.js";
import { cliVersion } from "../lib/version.js";
import { compile, type CompileReport } from "./compile.js";
import { startDaemon } from "./daemon-control.js";
import { signInWithBrowser } from "./login.js";
import { appBaseURL } from "./mcp-v2.js";
import { getAgents } from "./setup.js";
import { getNestedKey, setupMcpOAuth } from "./setup-mcp.js";

/** Whether an agent's MCP settings already name memax (memax setup, or an earlier init). */
function mcpPresent(
  id: string,
  configPath: string,
  mcpKey: string,
  format: string,
): boolean {
  const file =
    id === "claude-code" ? join(homedir(), ".claude.json") : configPath;
  if (!existsSync(file)) return false;
  try {
    const text = readFileSync(file, "utf8");
    if (format === "toml") return /^\[mcp_servers\.memax\]/m.test(text);
    if (format === "yaml-mcp-servers") return /^\s+memax:/m.test(text);
    const config = JSON.parse(text) as Record<string, unknown>;
    return !!getNestedKey(config, id === "claude-code" ? "mcpServers" : mcpKey)
      ?.memax;
  } catch {
    return false;
  }
}

function hasMcp(agent: AgentEntry): boolean {
  const def = getAgents().find((a) => a.id === agent.setupId);
  return !!def && mcpPresent(def.id, def.configPath, def.mcpKey, def.format);
}

/** Writes an agent's MCP settings for OAuth, as memax setup --mcp does. */
async function writeMcp(agent: AgentEntry): Promise<McpOutcome> {
  const def = getAgents().find((a) => a.id === agent.setupId);
  if (!def) return "unsupported";
  try {
    setupMcpOAuth(def);
    return "written";
  } catch (err) {
    return { error: (err as Error).message };
  }
}

async function compileHere(
  space: string,
  timeout: number,
): Promise<CompileOutcome | null> {
  let json = "";
  await compile(
    { space, format: "json", timeout: String(timeout) },
    {
      memax: getClient(),
      paths: daemonPaths(),
      cwd: process.cwd(),
      out: (l) => (json += l),
    },
  );
  try {
    const r = JSON.parse(json) as CompileReport;
    return {
      timedOut: r.timed_out,
      writer: r.writer,
      targets: r.targets.map((t) => ({
        label: t.label,
        kind: t.kind,
        sync_state: t.sync_state,
        files: t.files,
      })),
    };
  } catch {
    return null;
  }
}

export function initDeps(o: InitOptions): InitDeps {
  const interactive =
    !!process.stdin.isTTY && !!process.stdout.isTTY && o.format !== "json";
  return {
    memax: getClient(),
    paths: daemonPaths(),
    cwd: process.cwd(),
    home: homedir(),
    env: { path: process.env.PATH ?? "", platform: process.platform },
    out: (l) => console.log(l),
    interactive,
    prompt: {
      confirm: (q, def) => (def ? confirmDefault(q) : confirm(q)),
      ask: (q) => ask(q),
    },
    git: runGit,
    hasCredentials: () => usesAPIKey() || !!loadCredentials()?.access_token,
    signIn: async () => {
      if (!interactive) return false;
      console.log("");
      if (!(await confirmDefault("  Sign in to Memax in your browser? [Y/n] ")))
        return false;
      return signInWithBrowser();
    },
    hasMcp,
    writeMcp,
    startDaemon: async () =>
      (await startDaemon({
        paths: daemonPaths(),
        out: (l) => console.log(l),
      })) === 0,
    compile: compileHere,
    appUrl: appBaseURL(),
    version: cliVersion(),
    now: () => performance.now(),
    sleep: (ms) => new Promise((r) => setTimeout(r, ms)),
  };
}

export function registerInitCommand(program: Command): void {
  program
    .command("init")
    .description(
      "Set Memax up here: find your agents and their files, import what they know as proposals, settle what disagrees, and compile",
    )
    .option(
      "--space <slug>",
      "The project space (default: .memax.yml, the link, then the git remote; else a new one)",
    )
    .option(
      "-y, --yes",
      "Take the recommended answer to every question (for CI and scripts)",
    )
    .option(
      "--no-personal",
      "Leave machine-local memory (~/.claude, ~/.codex, ~/.gemini) where it is",
    )
    .option("--no-connect", "Don't write the agents' MCP settings")
    .option(
      "--no-daemon",
      "Write the files once instead of starting the daemon",
    )
    .option("--dry-run", "Show what would be uploaded, and upload nothing")
    .option("--wait <seconds>", "How long to wait for the judge", "20")
    .option("--timing", "Show how long each step took, against its budget")
    .option("--format <format>", "Output format: text, json", "text")
    .action(async (opts: InitOptions) => {
      try {
        process.exitCode = await runInit(opts, initDeps(opts));
      } catch (err) {
        console.error(chalk.red(`  ${(err as Error).message}`));
        process.exitCode = 1;
      }
    });
}
