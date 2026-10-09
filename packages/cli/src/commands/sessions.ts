// memax sessions: everywhere you are signed in (the web app, the CLI on
// each machine, devices, MCP clients), from /v2/sessions. Signing another
// session out needs you on the web app (Settings on memax.app), so an
// agent using this login can't sign you out elsewhere; `memax logout`
// signs this one out.
import { Command } from "commander";
import chalk from "chalk";
import type { V2 } from "memax-sdk";
import { getClient } from "../lib/client.js";
import { apiFailureMessage, clock, MARK, pad } from "./v2-output.js";

const SURFACE_WORDS: Record<V2.SessionSurface, string> = {
  web: "web",
  cli: "cli",
  device: "device",
  mcp: "mcp",
};

/** Where a session was last seen: "203.0.113.7, Lisbon". */
function where(s: V2.Session): string {
  return [s.address, s.city].filter(Boolean).join(", ");
}

/** The rows `memax sessions` prints, this session first. */
export function renderSessions(
  sessions: V2.Session[],
  now = new Date(),
): string[] {
  if (sessions.length === 0) {
    return ["", chalk.gray("  You aren't signed in anywhere."), ""];
  }
  const sorted = [...sessions].sort(
    (a, b) => Number(b.current) - Number(a.current),
  );
  const width = Math.max(18, ...sorted.map((s) => s.client.length + 2));
  const lines = ["", chalk.bold("  Sessions"), ""];
  for (const s of sorted) {
    const mark = s.current ? chalk.green(MARK.kept) : " ";
    const note = [
      s.current ? "this one" : `last used ${clock(s.last_used_at, now)}`,
      where(s),
    ]
      .filter(Boolean)
      .join(" · ");
    lines.push(
      `  ${mark} ${pad(SURFACE_WORDS[s.surface], 8)}${pad(s.client, width)}${chalk.gray(note)}`.trimEnd(),
    );
  }
  lines.push(
    "",
    chalk.gray(
      "  Sign others out in Settings on memax.app; memax logout signs this one out.",
    ),
    "",
  );
  return lines;
}

/** Tab-separated, one session a line, for pipes. */
export function sessionLines(sessions: V2.Session[]): string[] {
  return sessions.map((s) =>
    [
      s.id,
      s.surface,
      s.client,
      s.current ? "current" : "",
      s.last_used_at,
      s.address ?? "",
      s.city ?? "",
    ].join("\t"),
  );
}

export async function sessionsCommand(opts: {
  format?: string;
}): Promise<void> {
  try {
    const { items } = await getClient().v2.sessions.list();
    if (opts.format === "json") {
      console.log(JSON.stringify(items, null, 2));
      return;
    }
    if (!process.stdout.isTTY) {
      for (const line of sessionLines(items)) console.log(line);
      return;
    }
    for (const line of renderSessions(items)) console.log(line);
  } catch (err) {
    console.error(chalk.red(`  ${apiFailureMessage(err)}`));
    process.exit(1);
  }
}

export function registerSessionsCommand(program: Command): void {
  program
    .command("sessions")
    .description(
      "Everywhere you are signed in: the web app, the CLI, devices and MCP clients",
    )
    .option("--format <format>", "Output format: text, json", "text")
    .action(sessionsCommand);
}
