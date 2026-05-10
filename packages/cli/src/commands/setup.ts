import { Command } from "commander";
import chalk from "chalk";
import { existsSync } from "node:fs";
import { join } from "node:path";
import { homedir } from "node:os";
import { execSync } from "node:child_process";
import { createInterface } from "node:readline";
import {
  type AgentDef,
  type MemaxBin,
  commandExists,
  resolveMemaxBin,
} from "./setup-types.js";
import {
  ensureApiKey,
  ensureLocalAgentKey,
  setupMcpRemote,
  setupMcpOAuth,
  setupMcp,
  printMcpConfigs,
  removeMcpJson,
  removeMcpToml,
} from "./setup-mcp.js";
import { setupHooks, removeHooks } from "./setup-hooks.js";
import {
  injectInstructions,
  removeInstructions,
  installSkills,
  removeSkills,
} from "./setup-instructions.js";

// Re-export types so other packages importing from setup.ts still work
export type { AgentDef, MemaxBin };

// --- Agent definitions ---

function getAgents(): AgentDef[] {
  const home = homedir();
  const cwd = process.cwd();

  // Helper: standard { type: "url", url, headers? } shape used by Cursor, Windsurf, OpenClaw
  const stdUrlEntry = (
    url: string,
    headers?: Record<string, string>,
  ): Record<string, unknown> => ({
    type: "url",
    url,
    ...(headers ? { headers } : {}),
  });

  // Helper: { type: "http", url, headers? } shape used by VS Code, Copilot CLI
  const httpEntry = (
    url: string,
    headers?: Record<string, string>,
  ): Record<string, unknown> => ({
    type: "http",
    url,
    ...(headers ? { headers } : {}),
  });

  return [
    {
      name: "Claude Code",
      id: "claude-code",
      configPath: join(home, ".claude", "settings.json"),
      format: "json-mcpServers",
      mcpKey: "mcpServers",
      hasHooks: true,
      globalInstructionFile: join(home, ".claude", "CLAUDE.md"),
      detect: () =>
        existsSync(join(home, ".claude")) || commandExists("claude"),
      // Claude Code uses `claude mcp add` CLI, not JSON config — this is a fallback
      remoteEntry: stdUrlEntry,
    },
    {
      name: "Cursor",
      id: "cursor",
      configPath: join(home, ".cursor", "mcp.json"),
      format: "json-mcpServers",
      mcpKey: "mcpServers",
      hasHooks: false,
      globalInstructionFile: null,
      detect: () =>
        existsSync(join(home, ".cursor")) || commandExists("cursor"),
      remoteEntry: stdUrlEntry,
    },
    {
      name: "Windsurf",
      id: "windsurf",
      configPath: join(home, ".codeium", "windsurf", "mcp_config.json"),
      format: "json-mcpServers",
      mcpKey: "mcpServers",
      hasHooks: false,
      globalInstructionFile: null,
      detect: () =>
        existsSync(join(home, ".codeium", "windsurf")) ||
        commandExists("windsurf"),
      remoteEntry: stdUrlEntry,
    },
    {
      name: "Gemini CLI",
      id: "gemini",
      configPath: join(home, ".gemini", "settings.json"),
      format: "json-mcpServers",
      mcpKey: "mcpServers",
      hasHooks: true,
      globalInstructionFile: join(home, ".gemini", "GEMINI.md"),
      detect: () =>
        existsSync(join(home, ".gemini")) || commandExists("gemini"),
      // Gemini CLI uses httpUrl for streamable HTTP; url is its SSE field
      remoteEntry: (url, headers) => ({
        httpUrl: url,
        ...(headers ? { headers } : {}),
      }),
    },
    {
      name: "GitHub Copilot CLI",
      id: "copilot",
      configPath: join(home, ".copilot", "mcp-config.json"),
      format: "json-mcpServers",
      mcpKey: "mcpServers",
      hasHooks: false,
      globalInstructionFile: null,
      detect: () =>
        existsSync(join(home, ".copilot")) || commandExists("gh copilot"),
      // Copilot CLI requires type + url + tools for both remote and local servers
      remoteEntry: (url, headers) => ({
        type: "http",
        url,
        tools: [{ type: "function" }],
        ...(headers ? { headers } : {}),
      }),
      localEntry: (command, args) => ({
        command,
        args,
        tools: [{ type: "function" }],
      }),
    },
    {
      name: "Copilot (VS Code)",
      id: "vscode",
      configPath: join(".vscode", "mcp.json"),
      format: "json-servers",
      mcpKey: "servers",
      hasHooks: false,
      globalInstructionFile: null,
      detect: () => existsSync(".vscode") || commandExists("code"),
      // VS Code uses type: "http" for remote MCP servers
      remoteEntry: httpEntry,
    },
    {
      name: "Codex CLI",
      id: "codex",
      configPath: join(home, ".codex", "config.toml"),
      format: "toml",
      mcpKey: "mcp_servers",
      hasHooks: false,
      globalInstructionFile: join(home, ".codex", "AGENTS.md"),
      detect: () => existsSync(join(home, ".codex")) || commandExists("codex"),
      // Codex TOML is handled separately; this is for JSON fallback reference
      remoteEntry: stdUrlEntry,
    },
    {
      name: "OpenClaw",
      id: "openclaw",
      configPath: join(home, ".openclaw", "openclaw.json"),
      format: "json-mcpServers",
      mcpKey: "mcp.servers",
      hasHooks: false,
      globalInstructionFile: null,
      detect: () =>
        existsSync(join(home, ".openclaw")) || commandExists("openclaw"),
      remoteEntry: stdUrlEntry,
    },
    {
      name: "OpenCode",
      id: "opencode",
      configPath: join(cwd, ".opencode", "opencode.jsonc"),
      format: "json-mcpServers",
      mcpKey: "mcp",
      hasHooks: false,
      globalInstructionFile: null,
      detect: () =>
        existsSync(join(cwd, ".opencode")) || commandExists("opencode"),
      // OpenCode uses type: "remote" for remote, type: "local" for local
      remoteEntry: (url, headers) => ({
        type: "remote",
        url,
        ...(headers ? { headers } : {}),
      }),
      localEntry: (command, args) => ({
        type: "local",
        command,
        args,
      }),
    },
  ];
}

// --- Setup command ---

interface SetupOptions {
  mcp?: boolean;
  hooks?: boolean;
  instructions?: boolean;
  all?: boolean;
  local?: boolean;
  apiKey?: boolean; // opt-in: use per-agent API keys instead of OAuth
  print?: boolean;
  only?: string;
  skip?: string;
  hub?: string; // scope MCP key to a specific hub
  readOnly?: boolean;
  allowDelete?: boolean;
  allowOrganize?: boolean;
  agentSync?: boolean;
}

export async function setupCommand(options: SetupOptions): Promise<void> {
  const enableMcp = options.all || options.mcp;
  const enableHooks = options.all || options.hooks;
  const enableInstructions = options.all || options.instructions;

  if (!enableMcp && !enableHooks && !enableInstructions && !options.print) {
    printUsage();
    return;
  }
  if (options.hub && options.agentSync) {
    console.error(
      chalk.red(
        "\n  --agent-sync cannot be combined with --hub because hub-scoped MCP keys are restricted to that hub only.\n",
      ),
    );
    process.exit(1);
  }
  // Hub scoping requires API key mode — OAuth grants are configured during
  // the consent screen, not at setup time.
  if (options.hub && !options.apiKey) {
    console.error(
      chalk.red(
        "\n  --hub requires --api-key because hub scoping is configured per API key.\n  Use: memax setup --mcp --api-key --hub <id>\n",
      ),
    );
    process.exit(1);
  }
  // Permission flags only apply to remote API key mode — reject in OAuth and local mode
  const permissionFlags = [
    options.readOnly && "--read-only",
    options.allowDelete && "--allow-delete",
    options.allowOrganize && "--allow-organize",
    options.agentSync && "--agent-sync",
  ].filter(Boolean);
  if (permissionFlags.length > 0 && (!options.apiKey || options.local)) {
    console.error(
      chalk.red(
        `\n  ${permissionFlags.join(", ")} only apply to remote API key mode.\n  Use: memax setup --mcp --api-key ${permissionFlags.join(" ")}\n`,
      ),
    );
    process.exit(1);
  }
  // --api-key is meaningless with --local
  if (options.apiKey && options.local) {
    console.error(
      chalk.red(
        "\n  --api-key and --local are mutually exclusive.\n  Use --api-key for remote API key mode, or --local for local CLI mode.\n",
      ),
    );
    process.exit(1);
  }

  // --print: just output config JSON for manual copy/paste
  if (options.print) {
    await printMcpConfigs({
      local: options.local ?? false,
      apiKey: options.apiKey ?? false,
      hub: options.hub,
      readOnly: options.readOnly,
      allowDelete: options.allowDelete,
      allowOrganize: options.allowOrganize,
      agentSync: options.agentSync,
    });
    return;
  }

  // Remote mode (default): OAuth auto-discovery (no API key in config)
  // --api-key: use per-agent API keys (legacy mode, for CI/CD or agents without OAuth)
  // --local: use local CLI binary (memax mcp serve)
  const useRemote = !options.local;
  const useApiKey = options.apiKey ?? false;

  // Local mode: need memax binary
  let memaxBin: MemaxBin | null = null;
  if (!useRemote || enableHooks) {
    memaxBin = resolveMemaxBin();
    if (!memaxBin && !useRemote) {
      console.error(
        chalk.red(
          "\n  Could not find memax binary.\n  Install globally: npm install -g memax-cli@alpha\n",
        ),
      );
      process.exit(1);
    }
  }

  // Filter agents
  const allAgents = getAgents();
  const onlySet = options.only
    ? new Set(options.only.split(",").map((s) => s.trim().toLowerCase()))
    : null;
  const skipSet = options.skip
    ? new Set(options.skip.split(",").map((s) => s.trim().toLowerCase()))
    : new Set<string>();

  const agents = allAgents.filter((a) => {
    if (skipSet.has(a.id)) return false;
    if (onlySet) return onlySet.has(a.id);
    return a.detect();
  });

  if (agents.length === 0) {
    console.log(chalk.yellow("\n  No supported AI agents detected.\n"));
    console.log(chalk.gray("  Supported agents:"));
    for (const a of allAgents) {
      console.log(chalk.gray(`    • ${a.name} (--only ${a.id})`));
    }
    console.log(
      chalk.gray("\n  Use --only to force setup for a specific agent.\n"),
    );
    return;
  }

  console.log(chalk.bold("\n  Memax Setup\n"));
  if (useRemote && useApiKey) {
    console.log(chalk.gray("  Mode: remote server (API key)\n"));
  } else if (useRemote) {
    console.log(chalk.gray("  Mode: remote server (OAuth)\n"));
  } else {
    console.log(chalk.gray("  Mode: local CLI\n"));
  }

  const results: { agent: string; changes: string[] }[] = [];

  for (const agent of agents) {
    const changes: string[] = [];

    // MCP setup — OAuth (default), API key (--api-key), or local (--local)
    if (enableMcp) {
      try {
        if (useRemote && useApiKey) {
          // Legacy API key mode — creates per-agent key with agent_name
          const agentKey = await ensureApiKey(options.hub, agent.id, {
            readOnly: options.readOnly,
            allowDelete: options.allowDelete,
            allowOrganize: options.allowOrganize,
            agentSync: options.agentSync,
          });
          if (!agentKey) {
            console.error(
              chalk.red(
                `  ✗ ${agent.name}: Could not create API key. Run: memax login`,
              ),
            );
            continue;
          }
          setupMcpRemote(agent, agentKey);
          changes.push("MCP server (API key)");
        } else if (useRemote) {
          // OAuth mode (default) — no API key in config, agent auto-discovers auth
          setupMcpOAuth(agent);
          changes.push("MCP server (OAuth)");
        } else {
          const localAgentKey = await ensureLocalAgentKey(agent.id);
          if (!localAgentKey) {
            console.error(
              chalk.red(
                `  ✗ ${agent.name}: Could not provision local agent key. Run: memax login`,
              ),
            );
            continue;
          }
          setupMcp(agent, memaxBin!);
          changes.push("MCP server (local agent auth)");
        }
      } catch (err) {
        console.log(
          chalk.red(
            `  ✗ ${agent.name}: MCP setup failed — ${(err as Error).message}`,
          ),
        );
      }
    }

    // Hook setup (only for agents that support it — needs local binary)
    if (enableHooks && agent.hasHooks && memaxBin) {
      try {
        setupHooks(agent, memaxBin);
        changes.push("Context injection hook");
      } catch (err) {
        console.log(
          chalk.red(
            `  ✗ ${agent.name}: Hook setup failed — ${(err as Error).message}`,
          ),
        );
      }
    }

    // Inject memax instructions into agent's global instruction file
    if (enableInstructions && agent.globalInstructionFile) {
      try {
        injectInstructions(agent.globalInstructionFile);
        changes.push("Instructions injected");
      } catch (err) {
        console.log(
          chalk.red(
            `  ✗ ${agent.name}: Instruction injection failed — ${(err as Error).message}`,
          ),
        );
      }

      // Install memax skills for agents that support skill directories
      try {
        const skillCount = await installSkills(agent);
        if (skillCount > 0) {
          changes.push(
            `${skillCount} skill${skillCount > 1 ? "s" : ""} installed`,
          );
        }
      } catch (err) {
        console.log(
          chalk.red(
            `  ✗ ${agent.name}: Skill install failed — ${(err as Error).message}`,
          ),
        );
      }
    }

    if (changes.length > 0) {
      results.push({ agent: agent.name, changes });
    }
  }

  // Print summary
  if (results.length === 0) {
    console.log(chalk.yellow("  No changes made.\n"));
    return;
  }

  console.log(chalk.green("  Configured:\n"));
  for (const r of results) {
    console.log(chalk.white(`  ${r.agent}`));
    for (const c of r.changes) {
      console.log(chalk.gray(`    ✓ ${c}`));
    }
  }

  console.log(chalk.gray("\n  MCP tools available to all configured agents:"));
  console.log(
    chalk.gray("    • memax_recall — semantic search your knowledge"),
  );
  console.log(chalk.gray("    • memax_push   — save knowledge from sessions"));
  console.log(chalk.gray("    • memax_get    — read full memory by ID"));
  console.log(chalk.gray("    • memax_list   — browse memories"));

  if (enableHooks) {
    const hookAgents = results.filter((r) =>
      r.changes.includes("Context injection hook"),
    );
    if (hookAgents.length > 0) {
      console.log(
        chalk.gray(
          `\n  Hooks installed for: ${hookAgents.map((r) => r.agent).join(", ")}`,
        ),
      );
      console.log(
        chalk.gray(
          "  Every prompt gets relevant context injected automatically.",
        ),
      );
    }
  }

  console.log(
    chalk.gray("\n  Restart your agents for changes to take effect."),
  );

  // Offer to restore configs from cloud if this looks like a new device
  if (enableInstructions || options.all) {
    try {
      const { syncAgentMemoryCommand } = await import("./agent-configs.js");
      const rl = createInterface({
        input: process.stdin,
        output: process.stdout,
      });
      const restore = await new Promise<boolean>((resolve) => {
        rl.question(
          chalk.gray("\n  Restore agent configs from Memax cloud? [Y/n] "),
          (answer) => {
            rl.close();
            resolve(answer.trim().toLowerCase() !== "n");
          },
        );
      });
      if (restore) {
        await syncAgentMemoryCommand({ skipConflicts: true });
      }
    } catch {
      // Sync not available (not logged in, etc.) — skip silently
    }
  }

  console.log();
}

// --- Teardown command ---

export async function teardownCommand(options: {
  only?: string;
}): Promise<void> {
  const allAgents = getAgents();
  const onlySet = options.only
    ? new Set(options.only.split(",").map((s) => s.trim().toLowerCase()))
    : null;

  const agents = onlySet
    ? allAgents.filter((a) => onlySet.has(a.id))
    : allAgents;

  let removed = false;

  for (const agent of agents) {
    try {
      // Claude Code uses its own CLI
      if (agent.id === "claude-code") {
        if (commandExists("claude")) {
          // Remove from both user and project scope to clean up old
          // (pre-scope) and new (user-scoped) installs
          for (const scope of ["--scope user", ""]) {
            try {
              execSync(`claude mcp remove memax ${scope}`.trim(), {
                stdio: "pipe",
              });
              console.log(
                chalk.gray(
                  `  Removed MCP from ${agent.name}${scope ? " (user scope)" : " (project scope)"}`,
                ),
              );
              removed = true;
            } catch {
              // Not installed in this scope
            }
          }
        }
        if (agent.hasHooks && existsSync(agent.configPath)) {
          if (removeHooks(agent)) removed = true;
        }
        if (
          agent.globalInstructionFile &&
          removeInstructions(agent.globalInstructionFile)
        ) {
          console.log(chalk.gray(`  Removed instructions from ${agent.name}`));
          removed = true;
        }
        continue;
      }

      if (!existsSync(agent.configPath)) continue;

      if (agent.format === "toml") {
        if (removeMcpToml(agent)) removed = true;
      } else {
        if (removeMcpJson(agent)) removed = true;
      }
      if (agent.hasHooks && removeHooks(agent)) removed = true;
      if (
        agent.globalInstructionFile &&
        removeInstructions(agent.globalInstructionFile)
      ) {
        console.log(chalk.gray(`  Removed instructions from ${agent.name}`));
        removed = true;
      }
      if (removeSkills(agent)) {
        console.log(chalk.gray(`  Removed skills from ${agent.name}`));
        removed = true;
      }
    } catch {
      // Skip agents we can't clean up
    }
  }

  if (!removed) {
    console.log(chalk.yellow("\n  No Memax integrations found to remove.\n"));
    return;
  }

  console.log(
    chalk.green(
      "\n  Memax integrations removed.\n  Restart your agents for changes to take effect.\n",
    ),
  );
}

// --- Usage ---

function printUsage(): void {
  const agents = getAgents();
  const detected = agents.filter((a) => a.detect());

  console.log(
    chalk.bold("\n  Memax Setup — Configure AI Agent Integrations\n"),
  );

  if (detected.length > 0) {
    console.log(chalk.gray("  Detected agents:"));
    for (const a of detected) {
      const hookNote = a.hasHooks ? " (MCP + hooks)" : " (MCP)";
      console.log(chalk.white(`    • ${a.name}${hookNote}`));
    }
    console.log();
  }

  console.log(chalk.gray("  Usage:\n"));
  console.log(
    chalk.gray(
      "    memax setup --mcp               Remote MCP server for all detected agents",
    ),
  );
  console.log(
    chalk.gray(
      "    memax setup --instructions      Inject memax usage instructions into agent configs",
    ),
  );
  console.log(
    chalk.gray(
      "    memax setup --all               MCP + hooks + instructions",
    ),
  );
  console.log(
    chalk.gray(
      "    memax setup --mcp --local       Use local CLI instead of remote server",
    ),
  );
  console.log(
    chalk.gray(
      "    memax setup --mcp --api-key     Use API keys instead of OAuth (CI/CD)",
    ),
  );
  console.log(
    chalk.gray("    memax setup --print        Print MCP config to copy/paste"),
  );
  console.log(chalk.gray("    memax setup --mcp --only claude-code,cursor"));
  console.log(chalk.gray("    memax setup --mcp --read-only"));
  console.log(
    chalk.gray("    memax setup --mcp --hub memax-team --allow-organize"),
  );
  console.log(
    chalk.gray("    memax teardown             Remove all integrations\n"),
  );

  console.log(chalk.gray("  Supported agents:"));
  for (const a of agents) {
    const status = a.detect()
      ? chalk.green("detected")
      : chalk.gray("not found");
    console.log(
      chalk.gray(`    ${a.id.padEnd(14)} ${a.name.padEnd(20)} ${status}`),
    );
  }
  console.log();
}

export function registerSetupCommands(program: Command): void {
  program
    .command("setup")
    .description("Set up AI agent integrations (auto-detects installed agents)")
    .option("--mcp", "Enable MCP server (agent tools)")
    .option("--hooks", "Enable context injection hooks")
    .option(
      "--instructions",
      "Inject memax instructions into agent config files",
    )
    .option("--all", "Enable MCP + hooks + instructions")
    .option("--local", "Use local stdio MCP instead of remote server")
    .option(
      "--api-key",
      "Use per-agent API keys instead of OAuth (for CI/CD or agents without OAuth support)",
    )
    .option("--print", "Print MCP config JSON to copy/paste (no changes made)")
    .option("--only <agents>", "Only configure these agents (comma-separated)")
    .option("--skip <agents>", "Skip these agents (comma-separated)")
    .option("--hub <id>", "Scope MCP key to a specific hub")
    .option("--read-only", "Create remote MCP keys without write access")
    .option("--allow-delete", "Allow remote MCP keys to delete memories")
    .option(
      "--allow-organize",
      "Allow remote MCP keys to organize topics and run dreams",
    )
    .option("--agent-sync", "Allow remote MCP keys to sync agent configs")
    .action(setupCommand);

  program
    .command("teardown")
    .description("Remove Memax integrations from agents")
    .option(
      "--only <agents>",
      "Only remove from these agents (comma-separated)",
    )
    .action(teardownCommand);
}
