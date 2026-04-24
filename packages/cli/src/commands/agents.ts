import { Command } from "commander";
import {
  doctorAgentConfigsCommand,
  listAgentConfigsCommand,
  registerAgentConfigCommands,
  syncAgentMemoryCommand,
} from "./agent-configs.js";
import {
  doctorAgentSessionsCommand,
  listAgentSessionsCommand,
  registerAgentSessionCommands,
  syncAgentSessionsCommand,
} from "./agent-sessions.js";

async function syncAgentsCommand(options: {
  push?: boolean;
  pull?: boolean;
}): Promise<void> {
  await syncAgentMemoryCommand(options);
  await syncAgentSessionsCommand(options);
}

async function listAgentsCommand(): Promise<void> {
  await listAgentConfigsCommand();
  await listAgentSessionsCommand();
}

async function doctorAgentsCommand(): Promise<void> {
  await doctorAgentConfigsCommand();
  await doctorAgentSessionsCommand();
}

export function registerAgentsCommands(program: Command): void {
  const agentsCmd = program
    .command("agents")
    .description("Manage synced agent configs and sessions");

  agentsCmd
    .command("sync")
    .description(
      "Sync agent configs and sessions bidirectionally with Memax cloud",
    )
    .option("--push", "Force push local configs to cloud (overwrite)")
    .option("--pull", "Force pull cloud data to local (overwrite)")
    .action(syncAgentsCommand);

  agentsCmd
    .command("list")
    .description("List synced agent configs and sessions in the cloud")
    .action(listAgentsCommand);

  agentsCmd
    .command("doctor")
    .description(
      "Explain agent sync identity, discovery, and safe restore behavior on this machine",
    )
    .action(doctorAgentsCommand);

  registerAgentConfigCommands(agentsCmd);
  registerAgentSessionCommands(agentsCmd);
}
