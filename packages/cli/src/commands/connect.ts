// memax connect <agent>: MCP settings, the session-start hook, the
// connection in this repository's space, and a first compile (plan 25
// §7.2). The flow is lib/connect/run.ts; this file wires it to the
// machine and prints what it did. Running it again changes nothing.
import { homedir } from "node:os";
import chalk from "chalk";
import type { Command } from "commander";
import { MemaxError, refusalOf } from "memax-sdk";
import { getClient, usesAPIKey } from "../lib/client.js";
import { loadConfig } from "../lib/config.js";
import { loadCredentials } from "../lib/credentials.js";
import { daemonPaths } from "../lib/daemon/paths.js";
import {
  CLOUD_AGENTS,
  cloudAgentFor,
  runConnectCloud,
  type CloudConnectOptions,
  type CloudConnectReport,
} from "../lib/connect/cloud.js";
import { confirm } from "../lib/prompt.js";
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

function personalLine(r: ConnectReport): Line | null {
  const p = r.personal;
  if (!p) return null;
  const level = p.autonomy
    ? p.autonomy[0].toUpperCase() + p.autonomy.slice(1)
    : "";
  switch (p.state) {
    case "connected":
      return [done, "Personal", `${p.space}, at ${level}`];
    case "already":
      return [same, "Personal", `${p.space}, at ${level}`];
    default:
      return [
        wait,
        "Personal",
        `${p.space}: once it has signed in, run this again to connect it there`,
      ];
  }
}

function homeLine(r: ConnectReport): Line | null {
  const h = r.home;
  if (!h) return null;
  const compiled =
    h.compile === "compiled"
      ? "; written"
      : h.compile === "timed_out"
        ? "; still compiling, memax status shows when it's written"
        : h.compile === "failed"
          ? "; couldn't compile, try memax compile"
          : "";
  switch (h.state) {
    case "added":
      return [done, "Home", `${h.path}: ${h.note ?? "added"}${compiled}`];
    case "moved":
      return [
        done,
        "Home",
        `moved to ${h.path} (${r.name} reads it now), ${h.note ?? ""}${compiled}`,
      ];
    case "present":
      return [same, "Home", `${h.path}, your Personal space`];
    case "unlinked":
      return [
        wait,
        "Home",
        "this machine was unlinked; memax link --home links it again",
      ];
    case "refused":
      return [wait, "Home", `${h.path}: ${h.note ?? "Memax won't write it"}`];
    case "absent":
      return r.agent === "gemini-cli"
        ? [
            skip,
            "Home",
            "Gemini CLI reads your Personal space from ~/.gemini/AGENTS.md once it reads AGENTS.md: add --agents-md",
          ]
        : [skip, "Home", `${r.name} isn't set up in this home folder`];
    default:
      return [
        skip,
        "Home",
        "memax link --home writes your Personal space into its own file here",
      ];
  }
}

function shimLine(r: ConnectReport): Line | null {
  switch (r.shim?.state) {
    case "created":
      return [done, "GEMINI.md", "added: it imports AGENTS.md"];
    case "present":
      return r.shim.note
        ? [wait, "GEMINI.md", r.shim.note]
        : [same, "GEMINI.md", `compiled in this space (${r.shim.path})`];
    case "refused":
      return [fail, "GEMINI.md", r.shim.note ?? "not added"];
    case "not_asked":
      return r.contextFile?.state === "written" ||
        r.contextFile?.state === "present"
        ? null
        : [
            skip,
            "GEMINI.md",
            "not compiled: Antigravity reads AGENTS.md. For Gemini CLI, add --agents-md",
          ];
    default:
      return null;
  }
}

function contextFileLine(r: ConnectReport): Line | null {
  const c = r.contextFile;
  switch (c?.state) {
    case "written":
      return [
        done,
        "AGENTS.md",
        `Gemini CLI reads AGENTS.md now (context.fileName in ${c.file})`,
      ];
    case "present":
      return [same, "AGENTS.md", `Gemini CLI reads AGENTS.md (${c.file})`];
    case "refused":
      return [fail, "AGENTS.md", c.note ?? "not changed"];
    default:
      return null;
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
  for (const l of [personalLine(r), homeLine(r)]) if (l) lines.push(l);
  for (const l of [contextFileLine(r), shimLine(r)]) if (l) lines.push(l);
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
    r.compile.state === "failed" ||
    r.home?.compile === "failed" ||
    r.shim?.state === "refused" ||
    r.contextFile?.state === "refused"
  );
}

/** memax connect --cloud: the connection, the key once, the hook file and the steps. */
export function renderCloud(r: CloudConnectReport): string[] {
  const out = ["", chalk.bold(`  Memax · connect --cloud ${r.name}`), ""];
  const line = (mark: string, label: string, text: string) =>
    out.push(`  ${mark} ${pad(label, 9)}${text}`.trimEnd());
  const c = r.connection;
  if (c) {
    const at = `${c.display_name}, at ${c.autonomy[0]!.toUpperCase()}${c.autonomy.slice(1)}`;
    if (c.state === "connected") line(done, "Agent", at);
    else if (c.state === "rekeyed")
      line(done, "Agent", `${at}: a new start key`);
    else line(same, "Agent", at);
  }
  const h = r.hook;
  if (h) {
    const o = h.outcome;
    if (typeof o === "object") line(fail, "Hook", o.error);
    else if (o === "written")
      line(
        done,
        "Hook",
        `${h.path}: commit it (it names the key, never holds it)`,
      );
    else if (o === "present") line(same, "Hook", `already in ${h.path}`);
    else line(wait, "Hook", `${h.path}, to add and commit:`);
    if (o === "printed" && h.content)
      out.push(
        "",
        ...h.content
          .trimEnd()
          .split("\n")
          .map((l) => `      ${l}`),
      );
  }
  if (r.key) {
    out.push(
      "",
      "  Start key, shown once (Memax keeps only its hash):",
      "",
      `      ${chalk.bold(r.key.secret)}`,
      "",
      `  It only starts ${r.name}'s sessions in ${r.space}: it reads what changed since the compile in the repository, and nothing else.`,
      `  It expires on ${r.key.expires_at.slice(0, 10)}; memax connect --cloud ${r.agent} --yes gives it a new one.`,
    );
  }
  if (r.note) out.push("", `  ${r.note}`);
  if (out.at(-1) !== "") out.push("");
  out.push(r.key ? "  Then:" : "  Steps:");
  r.steps.forEach((s, i) => out.push(`    ${i + 1}. ${s}`));
  if (h && r.owned !== true)
    out.push(
      "",
      `  A CODEOWNERS entry for ${h.path} and AGENTS.md makes a change to how the start runs need an owner's review.`,
    );
  out.push("");
  return out;
}

/** Whether anything failed (exit 1). */
export function cloudFailed(r: CloudConnectReport): boolean {
  return typeof r.hook?.outcome === "object";
}

async function connectCloud(
  arg: string,
  opts: ConnectOptions & CloudConnectOptions,
) {
  const agent = cloudAgentFor(arg);
  if (!agent) {
    console.error(
      chalk.red(
        `  memax connect --cloud is for ${CLOUD_AGENTS.join(", ")}, not ${arg}.`,
      ),
    );
    process.exitCode = 2;
    return;
  }
  const json = opts.format === "json";
  const interactive = !!process.stdin.isTTY && !!process.stdout.isTTY && !json;
  if (!usesAPIKey() && !loadCredentials()?.access_token) {
    console.error(chalk.red("  Sign in first: memax login."));
    process.exitCode = 1;
    return;
  }
  try {
    const r = await runConnectCloud(agent, opts, {
      memax: getClient(),
      paths: daemonPaths(),
      cwd: process.cwd(),
      apiUrl: loadConfig().api_url,
      confirm: interactive && !opts.yes ? confirm : undefined,
    });
    if (json) console.log(JSON.stringify(r, null, 2));
    else for (const l of renderCloud(r)) console.log(l);
    process.exitCode = cloudFailed(r) ? 1 : 0;
  } catch (err) {
    console.error(chalk.red(`  ${cloudMessage(err)}`));
    process.exitCode = 1;
  }
}

/** A refusal in words: the server's own, which says what to do. */
function cloudMessage(err: unknown): string {
  if (err instanceof MemaxError) {
    const p = refusalOf(err);
    return p?.message ?? err.message;
  }
  return (err as Error).message;
}

export function registerConnectCommand(program: Command): void {
  program
    .command("connect <agent>")
    .description(
      `Connect an agent here: its MCP settings, its session-start hook, its connection to this repository's space, and a first compile. Agents: ${CONNECT_AGENTS.join(", ")}. With --cloud (${CLOUD_AGENTS.join(", ")}): a cloud agent's start key, printed once, and the hook file to commit`,
    )
    .option("--space <slug>", "The space (default: .memax.yml, then the link)")
    .option(
      "--cloud",
      "A cloud agent (its sandbox, not this machine): a start key and the committed hook file",
    )
    .option(
      "--yes",
      "With --cloud: give a connected cloud agent a new start key without asking",
    )
    .option("--no-hook", "Don't install the session-start hook")
    .option("--no-compile", "Don't compile, even in a linked repository")
    .option(
      "--agents-md",
      "Gemini CLI only: have Gemini CLI read AGENTS.md on this machine (context.fileName), your Personal space's ~/.gemini/AGENTS.md included",
    )
    .option(
      "--gemini-md",
      "Gemini CLI only: add a GEMINI.md that imports AGENTS.md to the repository, for teammates (never with --agents-md)",
    )
    .option(
      "--timeout <seconds>",
      "How long to wait for the first compile",
      "30",
    )
    .option("--format <format>", "Output format: text, json", "text")
    .action(
      async (
        arg: string,
        opts: ConnectOptions & CloudConnectOptions & { cloud?: boolean },
      ) => {
        if (opts.cloud) return connectCloud(arg, opts);
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
        if ((opts.geminiMd || opts.agentsMd) && agent.kind !== "gemini-cli") {
          const flag = opts.agentsMd ? "--agents-md" : "--gemini-md";
          console.error(
            chalk.red(
              `  ${flag} is for Gemini CLI: memax connect gemini ${flag}.`,
            ),
          );
          process.exitCode = 2;
          return;
        }
        if (opts.geminiMd && opts.agentsMd) {
          console.error(
            chalk.red(
              "  Use one: with --agents-md Gemini CLI reads AGENTS.md itself, so a GEMINI.md that imports it would load it twice. --agents-md is the one to keep.",
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
            env: process.env,
            appUrl: appBaseURL(),
          });
          if (json) console.log(JSON.stringify(r, null, 2));
          else for (const l of renderConnect(r, appBaseURL())) console.log(l);
          process.exitCode = connectFailed(r) ? 1 : 0;
        } catch (err) {
          console.error(chalk.red(`  ${(err as Error).message}`));
          process.exitCode = 1;
        }
      },
    );
}
