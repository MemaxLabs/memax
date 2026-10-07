import { Command } from "commander";
import chalk from "chalk";
import {
  readFileSync,
  writeFileSync,
  mkdirSync,
  existsSync,
  chmodSync,
} from "node:fs";
import { join } from "node:path";
import { homedir } from "node:os";
import { MEMAX_HOOK_PROMPT, UNIX_HOOK } from "./setup-hooks.js";

type Agent = "claude-code";

const SUPPORTED_AGENTS: Agent[] = ["claude-code"];

export function hookCommand(action: string, agent: string): void {
  if (!SUPPORTED_AGENTS.includes(agent as Agent)) {
    console.error(chalk.red(`Unsupported agent: ${agent}`));
    console.error(chalk.gray(`Supported: ${SUPPORTED_AGENTS.join(", ")}`));
    process.exit(1);
  }

  console.log(
    chalk.yellow(
      "  Note: `memax hook install|uninstall` is deprecated. Use `memax connect claude-code`, which installs the session-start hook.\n",
    ),
  );

  switch (action) {
    case "install":
      installHook(agent as Agent);
      break;
    case "uninstall":
      uninstallHook(agent as Agent);
      break;
    default:
      console.error(
        chalk.red(`Unknown action: ${action}. Use install or uninstall.`),
      );
      process.exit(1);
  }
}

export function registerHookCommand(program: Command): void {
  program
    .command("hook <action> [agent]")
    .description(
      "Agent hooks: `session-start` prints what changed since the agent's last session here (agents run it at session start; memax connect installs it). `install|uninstall <agent>` are deprecated",
    )
    .option(
      "--agent <id>",
      "With session-start: claude-code, codex, gemini, cursor or copilot",
    )
    .option("--debug", "With session-start: say why it printed nothing")
    .allowUnknownOption()
    .action(
      async (
        action: string,
        agent: string | undefined,
        opts: { agent?: string; debug?: boolean },
      ) => {
        // `memax hook session-start|flush` normally never gets here: bin.ts
        // runs them without loading the CLI (hook-main.ts).
        if (action === "session-start" || action === "flush") {
          const { runHook } = await import("../hook-main.js");
          const argv = [
            ...(opts.agent ? ["--agent", opts.agent] : []),
            ...(opts.debug ? ["--debug"] : []),
          ];
          process.exit(await runHook(action, argv));
        }
        if (!agent) {
          console.error(chalk.red(`Usage: memax hook ${action} <agent>`));
          process.exit(1);
        }
        hookCommand(action, agent);
      },
    );
}

function installHook(agent: Agent): void {
  switch (agent) {
    case "claude-code":
      installClaudeCodeHook();
      break;
  }
}

function uninstallHook(agent: Agent): void {
  switch (agent) {
    case "claude-code":
      uninstallClaudeCodeHook();
      break;
  }
}

function installClaudeCodeHook(): void {
  // Write the prompt file and hook script to ~/.memax/hooks/
  const hooksDir = join(homedir(), ".memax", "hooks");
  mkdirSync(hooksDir, { recursive: true });

  writeFileSync(join(hooksDir, "memax-prompt.md"), MEMAX_HOOK_PROMPT + "\n");

  const scriptPath = join(hooksDir, "context-inject-claude-code.sh");
  writeFileSync(scriptPath, UNIX_HOOK);
  chmodSync(scriptPath, 0o755);

  // Update Claude Code settings (~/.claude/settings.json)
  const claudeSettingsDir = join(homedir(), ".claude");
  const claudeSettingsFile = join(claudeSettingsDir, "settings.json");
  mkdirSync(claudeSettingsDir, { recursive: true });

  let settings: Record<string, unknown> = {};
  if (existsSync(claudeSettingsFile)) {
    try {
      settings = JSON.parse(readFileSync(claudeSettingsFile, "utf-8"));
    } catch {
      // Start fresh if parse fails
    }
  }

  const hooks = (settings.hooks ?? {}) as Record<string, unknown[]>;

  // Remove any existing memax hooks (idempotent)
  if (hooks["UserPromptSubmit"]) {
    hooks["UserPromptSubmit"] = (
      hooks["UserPromptSubmit"] as Array<{
        hooks?: Array<{ command?: string }>;
      }>
    ).filter((h) => !h.hooks?.some((hh) => hh.command?.includes("memax")));
  }

  hooks["UserPromptSubmit"] = [
    ...((hooks["UserPromptSubmit"] as unknown[]) ?? []),
    {
      matcher: "",
      hooks: [{ type: "command", command: scriptPath, timeout: 30 }],
    },
  ];
  settings.hooks = hooks;

  writeFileSync(claudeSettingsFile, JSON.stringify(settings, null, 2) + "\n");

  console.log(chalk.green("  Memax hook installed for Claude Code\n"));
  console.log(chalk.gray("  Hook script:"), scriptPath);
  console.log(chalk.gray("  Settings:"), claudeSettingsFile);
  console.log(
    chalk.gray(
      "\n  Every prompt reinforces Memax as the primary memory system.\n" +
        "  The agent uses MCP tools (memax_recall, memax_push) to fetch and save context.\n",
    ),
  );
}

function uninstallClaudeCodeHook(): void {
  const claudeSettingsFile = join(homedir(), ".claude", "settings.json");

  if (!existsSync(claudeSettingsFile)) {
    console.log(
      chalk.yellow("No Claude Code settings found. Nothing to uninstall."),
    );
    return;
  }

  try {
    const settings = JSON.parse(readFileSync(claudeSettingsFile, "utf-8"));
    const hooks = settings.hooks as Record<string, unknown[]> | undefined;

    if (hooks?.["UserPromptSubmit"]) {
      hooks["UserPromptSubmit"] = (
        hooks["UserPromptSubmit"] as Array<{
          hooks?: Array<{ command?: string }>;
        }>
      ).filter((h) => !h.hooks?.some((hh) => hh.command?.includes("memax")));

      if ((hooks["UserPromptSubmit"] as unknown[]).length === 0) {
        delete hooks["UserPromptSubmit"];
      }

      settings.hooks = hooks;
      writeFileSync(
        claudeSettingsFile,
        JSON.stringify(settings, null, 2) + "\n",
      );
    }

    console.log(chalk.green("Memax hook uninstalled from Claude Code."));
  } catch {
    console.error(chalk.red("Failed to read Claude Code settings."));
    process.exit(1);
  }
}
