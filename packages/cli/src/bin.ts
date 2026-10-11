#!/usr/bin/env node
// The `memax` binary. Three paths skip the rest of the CLI: `memax daemon
// run` goes straight to the daemon (see daemon-main.ts), `memax hook
// session-start|flush` to the agents' session-start hook (hook-main.ts),
// whose budget is 100 ms from process start to exit, and `memax hook
// cloud-start` to a cloud agent's start (cloud-start-main.ts), which asks
// the network and so never shares the local hook's code path. Everything
// else is the full command set in index.ts.
import module from "node:module";

// Reuse compiled code between runs (Node 22.1+; a no-op before): about a
// third of the hook's start-up is compiling its modules.
module.enableCompileCache?.();

const [command, sub] = process.argv.slice(2);
if (command === "daemon" && sub === "run") {
  const { runDaemon } = await import("./daemon-main.js");
  process.exitCode = await runDaemon(process.argv.slice(4));
} else if (command === "hook" && (sub === "session-start" || sub === "flush")) {
  const { runHook } = await import("./hook-main.js");
  // Exit at once: a stdin pipe the agent left open mustn't hold the hook.
  process.exit(await runHook(sub, process.argv.slice(4)));
} else if (command === "hook" && sub === "cloud-start") {
  // A cloud agent's start: networked, so apart from session-start.
  const [{ runCloudStart }, { cliVersion }] = await Promise.all([
    import("./cloud-start-main.js"),
    import("./lib/version.js"),
  ]);
  process.exit(await runCloudStart(process.argv.slice(4), cliVersion()));
} else {
  await import("./index.js");
}

export {};
