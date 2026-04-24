import { Command } from "commander";
import chalk from "chalk";
import { getClient } from "../lib/client.js";
import { confirm } from "../lib/prompt.js";

export async function deleteCommand(
  id: string,
  options: { yes?: boolean },
): Promise<void> {
  if (!id) {
    console.error(chalk.red("Provide a memory ID: memax forget <id>"));
    process.exit(1);
  }

  const client = getClient();

  // Show what's being deleted and confirm
  if (!options.yes) {
    try {
      const memory = await client.memories.get(id);
      console.log(chalk.yellow(`\n  Delete "${memory.title}"?\n`));
    } catch {
      console.log(chalk.yellow(`\n  Delete memory ${id}?\n`));
    }

    const confirmed = await confirm("  Type y to confirm: ");
    if (!confirmed) {
      console.log(chalk.gray("  Cancelled.\n"));
      return;
    }
  }

  try {
    await client.memories.delete(id);
    console.log(chalk.green("  Forgotten."), chalk.gray(id + "\n"));
  } catch (err) {
    console.error(chalk.red(`  Delete failed: ${(err as Error).message}\n`));
    process.exit(1);
  }
}

export function registerDeleteCommand(program: Command): void {
  program
    .command("delete <id>")
    .alias("forget")
    .description("Delete a memory")
    .option("-y, --yes", "Skip confirmation")
    .action(deleteCommand);
}
