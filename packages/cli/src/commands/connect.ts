// memax connect <agent>: MCP settings, the session-start hook, the
// connection in this repository's space, and a first compile (plan 25
// §7.2). The flow is lib/connect/run.ts; this file wires it to the
// machine and prints what it did. Running it again changes nothing.
import { homedir } from "node:os";
import chalk from "chalk";
import type { Command } from "commander";
import { getClient, usesAPIKey } from "../lib/client.js";
import { loadCredentials } from "../lib/credentials.js";
import { daemonPaths } from "../lib/daemon/paths.js";
import {
  agentFor,
  CONNECT_AGENTS,
  runConnect,
  type ConnectOptions,
  type ConnectReport,
} from "../lib/connect/run.js";
import { compileHere, hasMcp, writeMcp } from "./init.js";
import { appBaseURL } from "./mcp-v2.js";
import { finalizeCodexOAuthLogin } from "./setup-mcp.js";
import { MARK, pad } from "./v2-output.js";

type Line = [mark: string, label: string, text: string];

const done = chalk.green(MARK.done);
const same = chalk.gray("=");
const skip = chalk.gray("-");
const wait = chalk.yellow(MARK.waiting);
const fail = chalk.red("✗");

function mcpLine(r: ConnectReport, app: string): Line {
  const m = r.mcp;
  if (typeof m === "object") return [fail, "MCP", `couldn't write: ${m.error}`];
  switch (m) {
    case "written":
      return [
        done,
        "MCP",
        "settings written; it signs in to Memax on first use",
      ];
    case "present":
      return [same, "MCP", "already set up"];
    case "plugin":
      return [same, "MCP", "from the Memax plugin"];
    case "connector": {
      const where = r.connection.space
        ? `${app}/${r.connection.space}/agents`
        : `${app}/settings`;
      return [
        wait,
        "MCP",
        `${r.name} connects through its connector: ${where}`,
      ];
    }
    default:
      return [skip, "MCP", "not written here"];
  }
}

function hookLines(r: ConnectReport): Line[] {
  const h = r.hook;
  const o = h.outcome;
  const lines: Line[] = [];
  if (typeof o === "object") lines.push([fail, "Hook", o.error]);
  else
    switch (o) {
      case "written":
        lines.push([done, "Hook", `session start, in ${h.file}`]);
        break;
      case "present":
        lines.push([same, "Hook", `already in ${h.file}`]);
        break;
      case "plugin":
        lines.push([same, "Hook", "from the Memax plugin"]);
        break;
      case "windows":
        lines.push([
          skip,
          "Hook",
          "not on Windows yet (it reads the daemon's cache)",
        ]);
        break;
      case "skipped":
        lines.push([skip, "Hook", "skipped (--no-hook)"]);
        break;
      case "unsupported":
        lines.push([
          skip,
          "Hook",
          h.note ?? "this agent has no session-start hook",
        ]);
        return lines;
    }
  if (h.note && (o === "written" || o === "present"))
    lines.push([wait, "", h.note]);
  return lines;
}

function connectionLine(r: ConnectReport): Line {
  const c = r.connection;
  const level = c.autonomy
    ? c.autonomy[0].toUpperCase() + c.autonomy.slice(1)
    : "";
  switch (c.state) {
    case "connected":
      return [done, "Space", `${c.space}, at ${level}`];
    case "already":
      return [same, "Space", `${c.space}, at ${level}`];
    case "on_sign_in":
      return [
        wait,
        "Space",
        c.note
          ? `${c.space}: ${c.note}`
          : `${c.space}: connects at ${level} when it first signs in`,
      ];
    case "no_space":
      return [wait, "Space", c.note ?? "no space here"];
    default:
      return [
        wait,
        "Space",
        "sign in (memax login), then run this again to connect it to a space",
      ];
  }
}

function compileLine(r: ConnectReport): Line | null {
  const c = r.compile;
  switch (c.state) {
    case "compiled":
      return [
        done,
        "Compile",
        `${(c.files ?? []).join(", ") || "compiled"} written here`,
      ];
    case "in_sync":
      return [same, "Compile", `in sync: ${(c.files ?? []).join(", ")}`];
    case "not_linked":
      return [
        skip,
        "Compile",
        "this repository isn't linked; memax link writes the compiled files here",
      ];
    case "no_targets":
      return [skip, "Compile", "the space has no targets yet"];
    case "timed_out":
      return [
        wait,
        "Compile",
        "still compiling; memax status shows when it's written",
      ];
    case "failed":
      return [fail, "Compile", "couldn't compile; try memax compile"];
    default:
      return null;
  }
}

export function renderConnect(r: ConnectReport, app: string): string[] {
  const lines: Line[] = [mcpLine(r, app), ...hookLines(r), connectionLine(r)];
  const c = compileLine(r);
  if (c) lines.push(c);
  return [
    "",
    chalk.bold(`  Memax · connect ${r.name}`),
    "",
    ...lines.map(([mark, label, text]) =>
      `  ${mark} ${pad(label, 9)}${text}`.trimEnd(),
    ),
    "",
  ];
}

/** Whether anything failed (exit 1). */
export function connectFailed(r: ConnectReport): boolean {
  return (
    typeof r.mcp === "object" ||
    typeof r.hook.outcome === "object" ||
    r.compile.state === "failed"
  );
}

export function registerConnectCommand(program: Command): void {
  program
    .command("connect <agent>")
    .description(
      `Connect an agent here: its MCP settings, its session-start hook, its connection to this repository's space, and a first compile. Agents: ${CONNECT_AGENTS.join(", ")}`,
    )
    .option("--space <slug>", "The space (default: .memax.yml, then the link)")
    .option("--no-hook", "Don't install the session-start hook")
    .option("--no-compile", "Don't compile, even in a linked repository")
    .option(
      "--timeout <seconds>",
      "How long to wait for the first compile",
      "30",
    )
    .option("--format <format>", "Output format: text, json", "text")
    .action(async (arg: string, opts: ConnectOptions) => {
      const agent = agentFor(arg);
      if (!agent) {
        console.error(
          chalk.red(
            `  memax connect doesn't know ${arg}. Agents: ${CONNECT_AGENTS.join(", ")}.`,
          ),
        );
        process.exitCode = 2;
        return;
      }
      const json = opts.format === "json";
      const interactive =
        !!process.stdin.isTTY && !!process.stdout.isTTY && !json;
      try {
        const r = await runConnect(agent, opts, {
          memax: getClient(),
          paths: daemonPaths(),
          cwd: process.cwd(),
          home: homedir(),
          platform: process.platform,
          hasCredentials: () =>
            usesAPIKey() || !!loadCredentials()?.access_token,
          hasMcp,
          writeMcp,
          afterMcp: async (a) => {
            if (a.kind === "codex" && interactive)
              await finalizeCodexOAuthLogin();
          },
          compile: compileHere,
        });
        if (json) console.log(JSON.stringify(r, null, 2));
        else for (const l of renderConnect(r, appBaseURL())) console.log(l);
        process.exitCode = connectFailed(r) ? 1 : 0;
      } catch (err) {
        console.error(chalk.red(`  ${(err as Error).message}`));
        process.exitCode = 1;
      }
    });
}
