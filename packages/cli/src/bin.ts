#!/usr/bin/env node
// The `memax` binary. `memax daemon run` goes straight to the daemon,
// without loading the rest of the CLI (see daemon-main.ts); everything
// else is the full command set in index.ts.
const [command, sub] = process.argv.slice(2);
if (command === "daemon" && sub === "run") {
  const { runDaemon } = await import("./daemon-main.js");
  process.exitCode = await runDaemon(process.argv.slice(4));
} else {
  await import("./index.js");
}

export {};
