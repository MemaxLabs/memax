import { Command } from "commander";
import {
  doctorAgentConfigsCommand,
  listAgentConfigsCommand,
  registerAgentConfigCommands,
  syncAgentMemoryCommand,
} from "./agent-configs.js";

export function registerAgentsCommands(program: Command): void {
  const agentsCmd = program
    .command("agents")
    .description("Manage synced agent configs");

  agentsCmd
    .command("sync")
    .description("Sync agent configs bidirectionally with Memax cloud")
    .option("--push", "Force push local configs to cloud (overwrite)")
    .option("--pull", "Force pull cloud data to local (overwrite)")
    .action(syncAgentMemoryCommand);

  agentsCmd
    .command("list")
    .description("List synced agent configs in the cloud")
    .action(listAgentConfigsCommand);

  agentsCmd
    .command("doctor")
    .description(
      "Explain agent sync identity, discovery, and safe restore behavior on this machine",
    )
    .action(doctorAgentConfigsCommand);

  registerAgentConfigCommands(agentsCmd);
}
