import { Command } from "commander";
import chalk from "chalk";
import { loadConfig, saveConfig, getConfigDir } from "../lib/config.js";

export function configGetCommand(key: string | undefined): void {
  const config = loadConfig();

  if (!key) {
    console.log(chalk.gray(`Config directory: ${getConfigDir()}`));
    console.log();
    for (const [k, v] of Object.entries(config)) {
      console.log(`${chalk.bold(k)}: ${v}`);
    }
    return;
  }

  const value = (config as unknown as Record<string, unknown>)[key];
  if (value === undefined) {
    console.error(chalk.red(`Unknown config key: ${key}`));
    process.exit(1);
  }
  console.log(String(value));
}

export function configSetCommand(key: string, value: string): void {
  saveConfig({ [key]: value });
  console.log(chalk.green("Set"), `${key} = ${value}`);
}

export function registerConfigCommand(program: Command): void {
  const configCmd = program
    .command("config")
    .description("Manage Memax configuration");

  configCmd
    .command("get [key]")
    .description("Get config value (or all values)")
    .action(configGetCommand);

  configCmd
    .command("set <key> <value>")
    .description("Set a config value")
    .action(configSetCommand);
}
