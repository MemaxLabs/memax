// memax gate [G-id]: the decisions agents are waiting on you for (plan 25
// §7.2, E8). Without an ID it lists the waiting gates in your spaces on
// the V2 record; with one it shows the question and answers it, picked
// interactively or with --option for scripts. The answer is kept as your
// decision, from the CLI (client-attested), so where a space's decisions
// need a person on the web (team spaces by default) it points you there.
import { randomUUID } from "node:crypto";
import { Command } from "commander";
import chalk from "chalk";
import { MemaxError, refusalOf, type V2 } from "memax-sdk";
import { getClient } from "../lib/client.js";
import { ask } from "../lib/prompt.js";
import { appBaseURL } from "./mcp-v2.js";

interface GateOptions {
  space?: string;
  option?: string;
  format?: string;
}

/** Thrown for a failure the command reports and exits 1 on. */
class GateError extends Error {}

async function v2Spaces(only?: string): Promise<V2.Space[]> {
  const { items } = await getClient().v2.spaces.list();
  const spaces = items.filter((sp) => sp.v2_enabled_at);
  if (!only) return spaces;
  const match = spaces.filter((sp) => sp.id === only || sp.slug === only);
  if (!match.length)
    throw new GateError(
      `No space on the V2 record called ${only}. Check the slug with memax hub list.`,
    );
  return match;
}

export function gateURL(sp: V2.Space, ref: string): string {
  const base = appBaseURL();
  return base
    ? `${base}/${encodeURIComponent(sp.slug)}/review?ref=${encodeURIComponent(ref)}`
    : "";
}

/** The option an answer names: its number (from 1) or its label. */
export function parseOption(g: V2.Gate, raw: string): number | undefined {
  const text = raw.trim();
  if (/^\d+$/.test(text)) {
    const n = Number(text);
    return n >= 1 && n <= g.options.length ? n : undefined;
  }
  const i = g.options.findIndex(
    (o) => o.label.toLowerCase() === text.toLowerCase(),
  );
  return i >= 0 ? i + 1 : undefined;
}

/** How a gate stands, in one line. */
export function gateStatusLine(g: V2.Gate): string {
  switch (g.status) {
    case "answered":
      return `Answered: ${g.answer?.label}. Kept as ${g.answer?.memory.ref}.`;
    case "withdrawn":
      return "Withdrawn: the question was taken back.";
    case "expired":
      return "Expired without an answer; the agent stopped waiting.";
    default:
      return `Waiting on you until ${g.expires_at.slice(0, 10)}.`;
  }
}

/** The gate as the terminal shows it. */
export function formatGate(g: V2.Gate, sp: V2.Space): string[] {
  const glyph =
    g.status === "waiting" ? "○" : g.status === "answered" ? "✓" : "-";
  const lines = [
    "",
    `  ${glyph} ${chalk.bold(g.ref)} ${chalk.gray(`· ${sp.name} · asked by ${g.agent ?? "an agent"}`)}`,
    "",
    `  ${chalk.bold(g.question)}`,
  ];
  if (g.context) lines.push(`  ${chalk.gray(g.context)}`);
  lines.push("");
  g.options.forEach((o, i) => {
    const chosen = g.answer?.option === i + 1;
    lines.push(
      `    ${chosen ? chalk.green(`${i + 1}`) : `${i + 1}`}  ${o.label}`,
    );
    if (o.detail) lines.push(`       ${chalk.gray(o.detail)}`);
  });
  lines.push("", `  ${chalk.gray(gateStatusLine(g))}`);
  return lines;
}

/** Finds a gate by display ID in the spaces, or by id anywhere. */
async function findGate(
  ref: string,
  only?: string,
): Promise<{ gate: V2.Gate; space: V2.Space }> {
  const client = getClient();
  const spaces = await v2Spaces(only);
  const found: { gate: V2.Gate; space: V2.Space }[] = [];
  for (const sp of spaces) {
    try {
      const gate = await client.v2.gates.get(ref, { space: sp.id });
      found.push({ gate, space: sp });
    } catch (err) {
      if (!(err instanceof MemaxError) || err.status !== 404) throw err;
    }
  }
  if (!found.length) throw new GateError(`Gate not found: ${ref}.`);
  if (found.length > 1)
    throw new GateError(
      `${ref} is in more than one of your spaces (${found.map((f) => f.space.slug).join(", ")}). Say which with --space.`,
    );
  return found[0];
}

async function listGates(options: GateOptions): Promise<void> {
  const client = getClient();
  const waiting: { gate: V2.Gate; space: V2.Space }[] = [];
  for (const sp of await v2Spaces(options.space)) {
    const page = await client.v2.gates.list(sp.id, { status: "waiting" });
    for (const gate of page.items) waiting.push({ gate, space: sp });
  }
  if (options.format === "json") {
    console.log(
      JSON.stringify(
        waiting.map((w) => w.gate),
        null,
        2,
      ),
    );
    return;
  }
  if (!process.stdout.isTTY) {
    for (const { gate, space } of waiting)
      console.log(
        `${gate.ref}\t${space.slug}\t${gate.agent ?? ""}\t${gate.question}`,
      );
    return;
  }
  if (!waiting.length) {
    console.log(chalk.gray("\n  Nothing waiting on you.\n"));
    return;
  }
  console.log(chalk.bold("\n  Waiting on you\n"));
  for (const { gate, space } of waiting) {
    console.log(
      `  ○ ${chalk.bold(gate.ref)}  ${chalk.gray(`${space.name} · ${gate.agent ?? "an agent"} · until ${gate.expires_at.slice(0, 10)}`)}`,
    );
    console.log(`    ${gate.question}`);
  }
  console.log(chalk.gray("\n  Answer one with memax gate <G-id>.\n"));
}

async function answerGate(ref: string, options: GateOptions): Promise<void> {
  const { gate, space } = await findGate(ref, options.space);
  if (options.format === "json" && options.option === undefined) {
    console.log(JSON.stringify(gate, null, 2));
    return;
  }
  for (const line of formatGate(gate, space)) console.log(line);
  if (gate.status !== "waiting") {
    if (options.option !== undefined)
      throw new GateError(
        `${gate.ref} isn't waiting, so it can't be answered.`,
      );
    console.log();
    return;
  }
  if (gate.needs_web) {
    const url = gateURL(space, gate.ref);
    const where = url
      ? ` Answer it at ${url}`
      : " Answer it in Review on the web.";
    const msg = `Decisions in ${space.name} need a person on the web.${where}`;
    if (options.option !== undefined) throw new GateError(msg);
    console.log(chalk.yellow(`\n  ${msg}\n`));
    return;
  }

  let raw = options.option;
  if (raw === undefined) {
    if (!process.stdin.isTTY) {
      console.log(
        chalk.gray("\n  Answer it with --option <number or label>.\n"),
      );
      return;
    }
    raw = await ask(
      `\n  Answer (1-${gate.options.length}), or press Enter to leave it: `,
    );
    if (!raw) {
      console.log(chalk.gray("  Left waiting.\n"));
      return;
    }
  }
  const option = parseOption(gate, raw);
  if (option === undefined)
    throw new GateError(
      `"${raw}" isn't one of the options. Use a number from 1 to ${gate.options.length}, or an option's label.`,
    );

  try {
    const res = await getClient().v2.gates.answer(
      gate.id,
      { option },
      { idempotencyKey: randomUUID(), ifMatch: gate.version, via: "cli" },
    );
    const label = res.gate.answer?.label ?? gate.options[option - 1].label;
    const memory = res.memory?.ref ?? res.gate.answer?.memory.ref;
    if (options.format === "json") {
      console.log(JSON.stringify(res, null, 2));
      return;
    }
    console.log(
      chalk.green(`\n  ✓ Answered ${gate.ref}: ${label}.`),
      chalk.gray(`Kept as ${memory}, a decision by you.\n`),
    );
  } catch (err) {
    const refusal = refusalOf(err);
    if (refusal?.message) throw new GateError(refusal.message);
    if (err instanceof MemaxError) throw new GateError(err.message);
    throw err;
  }
}

export async function gateCommand(
  ref: string | undefined,
  options: GateOptions,
): Promise<void> {
  try {
    if (ref) await answerGate(ref, options);
    else await listGates(options);
  } catch (err) {
    const msg =
      err instanceof GateError
        ? err.message
        : `Gate failed: ${(err as Error).message}`;
    console.error(chalk.red(`  ${msg}`));
    process.exit(1);
  }
}

export function registerGateCommand(program: Command): void {
  program
    .command("gate [id]")
    .description(
      "Answer a decision an agent is waiting on (G-0012); without an ID, list the waiting ones",
    )
    .option("--space <space>", "The space, by slug or ID")
    .option("--option <option>", "Answer with this option, by number or label")
    .option("--format <format>", "Output format: text, json", "text")
    .action(gateCommand);
}
