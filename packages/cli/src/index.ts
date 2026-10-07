#!/usr/bin/env node
import { Command } from "commander";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { join, dirname } from "node:path";
import { registerPushCommand } from "./commands/push.js";
import { registerRecallCommand } from "./commands/recall.js";
import { registerAskCommand } from "./commands/ask.js";
import { registerListCommand } from "./commands/list.js";
import { registerShowCommand } from "./commands/show.js";
import { registerDeleteCommand } from "./commands/delete.js";
import { registerForgetCommand } from "./commands/forget.js";
import { registerImportCommand } from "./commands/import.js";
import { registerAgentsCommands } from "./commands/agents.js";
import { registerGateCommand } from "./commands/gate.js";
import { registerHookCommand } from "./commands/hook.js";
import { registerConfigCommand } from "./commands/config.js";
import { registerLoginCommands } from "./commands/login.js";
import { registerAuthCommand } from "./commands/auth.js";
import { registerMcpCommand } from "./commands/mcp.js";
import { registerSetupCommands } from "./commands/setup.js";
import { registerHubCommands } from "./commands/hub.js";
import { registerTopicCommands } from "./commands/topic.js";
import { registerCaptureSessionCommand } from "./commands/capture.js";
import { registerDreamsCommands } from "./commands/dreams.js";
import { registerLinkCommands } from "./commands/link.js";
import { registerDaemonCommands } from "./commands/daemon.js";
import { registerStatusCommand } from "./commands/status.js";
import { registerInitCommand } from "./commands/init.js";
import { registerConnectCommand } from "./commands/connect.js";
import { registerSwitchCommand } from "./commands/switch.js";
import { registerCompileCommand } from "./commands/compile.js";
import { registerExportCommands } from "./commands/export.js";

const __dirname = dirname(fileURLToPath(import.meta.url));
const pkg = JSON.parse(
  readFileSync(join(__dirname, "..", "package.json"), "utf-8"),
);

const program = new Command();

program
  .name("memax")
  .description("Universal context & memory hub for AI agents")
  .version(pkg.version);

// --- Command registration (order defines help layout) ---

registerInitCommand(program);
registerConnectCommand(program);
registerSwitchCommand(program);
registerStatusCommand(program);
registerCompileCommand(program);
registerLinkCommands(program);
registerDaemonCommands(program);
registerPushCommand(program);
registerRecallCommand(program);
registerAskCommand(program);
registerListCommand(program);
registerShowCommand(program);
registerDeleteCommand(program);
registerForgetCommand(program);
registerExportCommands(program);
registerCaptureSessionCommand(program);
registerTopicCommands(program);
registerDreamsCommands(program);
registerHubCommands(program);
registerAgentsCommands(program);
registerGateCommand(program);
registerImportCommand(program);
registerSetupCommands(program);
registerHookCommand(program);
registerMcpCommand(program);
registerLoginCommands(program);
registerAuthCommand(program);
registerConfigCommand(program);

program.parse();
