// memax daemon start | stop | status | run | install | uninstall
import chalk from "chalk";
import type { Command } from "commander";
import { daemonPaths } from "../lib/daemon/paths.js";
import { startDaemon, stopDaemon } from "./daemon-control.js";
import { registerDaemonServiceCommands } from "./daemon-service.js";
import { readDaemonStatus, renderDaemonStatus } from "./daemon-status.js";

type Out = (line: string) => void;

export function registerDaemonCommands(program: Command): void {
  const daemon = program
    .command("daemon")
    .description(
      "The background process that writes compiled files into linked repositories",
    );
  const out: Out = (l) => console.log(l);

  daemon
    .command("start")
    .description("Start the daemon in the background")
    .action(async () => {
      process.exitCode = await startDaemon({ paths: daemonPaths(), out });
    });

  daemon
    .command("stop")
    .description("Stop the daemon")
    .action(async () => {
      process.exitCode = await stopDaemon({ paths: daemonPaths(), out });
    });

  daemon
    .command("status")
    .description(
      "Whether the daemon runs, and where each linked repository's files stand",
    )
    .option("--format <format>", "Output format: text, json", "text")
    .action(async (opts: { format?: string }) => {
      const s = await readDaemonStatus(daemonPaths());
      if (opts.format === "json") {
        console.log(JSON.stringify(s, null, 2));
        return;
      }
      for (const line of renderDaemonStatus(s)) console.log(line);
    });

  daemon
    .command("run")
    .description("Run the daemon in the foreground (for service managers)")
    .action(async () => {
      const { runDaemon } = await import("../daemon-main.js");
      process.exitCode = await runDaemon([]);
    });

  registerDaemonServiceCommands(daemon);
}
