// `memax-start`: the cloud start as one file with no dependencies, so a
// committed hook's cold `npx -y @memaxlabs/start@<version>` fetches one
// small tarball, not memax-cli and its MCP SDK. It is the CLI's own code
// (packages/cli/src/cloud-start-main.ts and lib/cloud-start/), bundled by
// scripts/build.mjs; `memax hook cloud-start` runs the same code.
import module from "node:module";
import { runCloudStart } from "../../cli/src/cloud-start-main.js";

/** The package's version, set by the build. */
declare const MEMAX_START_VERSION: string;

module.enableCompileCache?.();

// Exit at once: a stdin pipe the agent left open mustn't hold the hook.
process.exit(await runCloudStart(process.argv.slice(2), MEMAX_START_VERSION));
