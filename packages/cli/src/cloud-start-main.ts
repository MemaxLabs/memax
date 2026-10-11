// `memax hook cloud-start --agent <id>`, reached from bin.ts without the
// rest of the CLI, and the whole of `@memaxlabs/start` (bin `memax-start`,
// packages/start bundles this file and what it imports). A cloud agent's
// committed session-start hook runs it in a fresh sandbox: it reads the
// compiled files' headers, makes one request with a 2.5 s deadline, and
// prints what changed since (lib/cloud-start/). It always exits 0 and, on
// any failure, prints nothing. The local session-start hook (hook-main.ts)
// never imports this: it keeps its rule of no network and 100 ms.
//
// Node built-ins only, so the package has no dependencies.
import { mkdirSync } from "node:fs";
import { homedir } from "node:os";
import { hookAgent } from "./lib/hook/agents.js";
import { writeSeen } from "./lib/hook/seen.js";
import { print, readEvent } from "./lib/hook/stdio.js";
import { cloudStart } from "./lib/cloud-start/run.js";

export const CLOUD_USAGE = `Usage: memax hook cloud-start --agent <id> [--debug]
       memax-start --agent <id> [--debug]

For a cloud agent's committed session-start hook (Claude Code's cloud
sessions, Copilot's cloud agent): prints what changed in this repository's
compiled context since the compile its committed files hold, as a
<memax-context> block, or nothing. It reads MEMAX_START_KEY (and
MEMAX_API_URL), sends the files' compile IDs and drift hashes, never their
words, waits at most 2.5 s, and always exits 0. --agent is claude-code
(default), copilot or codex, and sets the output format. --debug (or
MEMAX_DEBUG=1) says on stderr why it printed nothing.
`;

function flag(argv: string[], name: string): string | undefined {
  const i = argv.indexOf(name);
  if (i >= 0) return argv[i + 1];
  const eq = argv.find((a) => a.startsWith(`${name}=`));
  return eq?.slice(name.length + 1);
}

/** The cloud start. `version` names the client in User-Agent. */
export async function runCloudStart(
  argv: string[],
  version: string,
): Promise<number> {
  if (argv.includes("--help") || argv.includes("-h")) {
    process.stdout.write(CLOUD_USAGE);
    return 0;
  }
  const debug = argv.includes("--debug") || process.env.MEMAX_DEBUG === "1";
  const started = performance.now();
  const say = (why: string) => {
    if (debug)
      process.stderr.write(
        `memax cloud-start: ${why}; ${Math.round(performance.now() - started)} ms\n`,
      );
  };
  try {
    const agent = hookAgent(flag(argv, "--agent") ?? "claude-code");
    const event = await readEvent();
    const res = await cloudStart({
      agent,
      event,
      env: process.env,
      cwd: process.cwd(),
      home: homedir(),
      now: new Date(),
      version,
    });
    print(res.output);
    if (res.seen) {
      try {
        mkdirSync(res.seen.dir, { recursive: true, mode: 0o700 });
        writeSeen(res.seen.path, res.seen.dir, res.seen.value);
      } catch {
        // a read-only home: the next start may say it again
      }
    }
    say(
      `${res.skipped ?? "printed"}${res.requestMs !== undefined ? ` (request ${res.requestMs} ms)` : ""}`,
    );
  } catch (err) {
    say(`failed: ${(err as Error).message}`);
  }
  return 0;
}
