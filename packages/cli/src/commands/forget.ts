// memax forget <M-id>: Forget a memory on the V2 record, on purpose (plan
// 25 §5.13, rule 7; the Cli board). It says what Forget removes (the
// compiled files and the agents, from the server's preview) and what goes
// with it, and you type the ID to confirm: Forget can't be undone. Then it
// waits a few seconds for the propagation and says what was rewritten and
// who will be told. A V1 memory id keeps V1's delete.
import { randomUUID } from "node:crypto";
import { Command } from "commander";
import chalk from "chalk";
import { MemaxError, forgetCarriesOf, refusalOf, type V2 } from "memax-sdk";
import { getClient } from "../lib/client.js";
import { ask } from "../lib/prompt.js";
import { deleteCommand } from "./delete.js";
import { appBaseURL } from "./mcp-v2.js";
import { MARK } from "./v2-output.js";

interface ForgetOptions {
  space?: string;
  confirm?: string;
  note?: string;
  yes?: boolean;
  format?: string;
}

/** Thrown for a failure the command reports and exits 1 on. */
class ForgetError extends Error {}

const DISPLAY_REF = /^M-\d+$/i;

/** How long to wait for the propagation before reporting. */
const PROPAGATION_WAIT_MS = 10_000;

export function tombstoneURL(sp: V2.Space, ref: string): string {
  const base = appBaseURL();
  return base
    ? `${base}/${encodeURIComponent(sp.slug)}/memories/${encodeURIComponent(ref)}`
    : "";
}

function plural(n: number, one: string, many: string): string {
  return `${n} ${n === 1 ? one : many}`;
}

const CARRY_WORDS: Record<V2.CarryReason, string> = {
  folded: "a proposal folded into it",
  updates: "a proposal that would change it",
  cites: "cites it",
  space: "in the space",
};

/** What Forget removes, as the Cli board says it. */
export function removesLine(p: V2.ForgetPreview): string {
  const files = p.files.filter((f) => f.delivery !== "copy").length;
  const copies = p.files.length - files;
  const parts = ["Memax"];
  if (files) parts.push(plural(files, "compiled file", "compiled files"));
  if (copies) parts.push(plural(copies, "copy-out", "copy-outs"));
  if (p.agents) parts.push(plural(p.agents, "agent", "agents"));
  const list =
    parts.length === 1
      ? parts[0]
      : `${parts.slice(0, -1).join(", ")} and ${parts[parts.length - 1]}`;
  return `This removes it from ${list}.`;
}

/** The memories that go with it, in one line. */
export function carriesLine(carries: V2.ForgetCarry[]): string {
  return `It takes ${carries.map((c) => `${c.ref} (${CARRY_WORDS[c.reason]})`).join(", ")} with it.`;
}

/** What the tombstone says, once the propagation has run (or not yet). */
export function reportLines(t: V2.Tombstone, sp: V2.Space): string[] {
  const lines = [
    chalk.gray(`  ${MARK.forgotten}`) + chalk.bold(` Forgotten ${t.ref}`),
  ];
  const targets = t.steps.filter((s) => s.kind === "target");
  const rewritten = targets.filter((s) => s.status === "done").length;
  if (rewritten)
    lines.push(
      chalk.green(`  ${MARK.done} `) +
        plural(rewritten, "file rewritten", "files rewritten"),
    );
  for (const s of targets) {
    const label = s.target?.label ?? "a target";
    if (s.status === "held")
      lines.push(
        chalk.yellow(`  ${MARK.waiting} `) +
          `${label} has a hand edit, which Memax never writes over. Take the line out yourself.`,
      );
    else if (s.status === "waiting" && s.reason === "delivery")
      lines.push(
        chalk.gray(`  ${MARK.working} `) +
          `${label} is compiled without it; the daemon writes it next.`,
      );
    else if (s.status === "waiting" && s.reason === "copy")
      lines.push(
        chalk.gray(`  ${MARK.working} `) +
          `${label} is compiled without it. Paste it again where you use it.`,
      );
    else if (s.status === "waiting")
      lines.push(chalk.gray(`  ${MARK.working} `) + `${label} is compiling.`);
    else if (s.status === "failed")
      lines.push(
        chalk.red(`  ${MARK.waiting} `) +
          `${label} didn't compile. Memax tries again.`,
      );
  }
  const agents = t.steps.filter((s) => s.kind === "agent");
  if (agents.length) {
    const paused = agents
      .filter((s) => s.reason === "paused")
      .map((s) => s.agent?.display_name ?? s.agent?.agent ?? "an agent");
    lines.push(
      chalk.gray(
        `  ${plural(agents.length, "agent", "agents")} will be told on their next read` +
          (paused.length
            ? ` · ${paused.join(", ")} ${paused.length === 1 ? "is" : "are"} paused`
            : ""),
      ),
    );
  }
  const url = tombstoneURL(sp, t.ref);
  if (url) lines.push(chalk.gray(`  The tombstone: ${url}`));
  return lines;
}

async function v2Spaces(only?: string): Promise<V2.Space[]> {
  const { items } = await getClient().v2.spaces.list();
  const spaces = items.filter((sp) => sp.v2_enabled_at);
  if (!only) return spaces;
  const match = spaces.filter((sp) => sp.id === only || sp.slug === only);
  if (!match.length)
    throw new ForgetError(
      `No space on the V2 record called ${only}. Check the slug with memax hub list.`,
    );
  return match;
}

/** Finds the memory on the V2 record, or undefined when it is a V1 one. */
async function findMemory(
  id: string,
  only?: string,
): Promise<{ memory: V2.Memory; space: V2.Space } | undefined> {
  const client = getClient();
  const spaces = await v2Spaces(only);
  if (!DISPLAY_REF.test(id)) {
    try {
      const { memory } = await client.v2.memories.get(id);
      const space = spaces.find((sp) => sp.id === memory.space_id);
      return space ? { memory, space } : undefined;
    } catch (err) {
      if (
        err instanceof MemaxError &&
        (err.status === 404 || err.status === 400)
      )
        return undefined;
      throw err;
    }
  }
  const found: { memory: V2.Memory; space: V2.Space }[] = [];
  for (const sp of spaces) {
    try {
      const { memory } = await client.v2.memories.get(id, { space: sp.id });
      found.push({ memory, space: sp });
    } catch (err) {
      if (!(err instanceof MemaxError) || err.status !== 404) throw err;
    }
  }
  if (!found.length) throw new ForgetError(`Memory not found: ${id}.`);
  if (found.length > 1)
    throw new ForgetError(
      `${id.toUpperCase()} is in more than one of your spaces (${found.map((f) => f.space.slug).join(", ")}). Say which with --space.`,
    );
  return found[0];
}

async function awaitPropagation(t: V2.Tombstone): Promise<V2.Tombstone> {
  const client = getClient();
  const until = Date.now() + PROPAGATION_WAIT_MS;
  let current = t;
  while (current.status !== "done" && Date.now() < until) {
    await new Promise((r) => setTimeout(r, 250));
    current = await client.v2.memories
      .tombstone(t.object_id)
      .catch(() => current);
  }
  return current;
}

async function forgetV2(
  memory: V2.Memory,
  space: V2.Space,
  options: ForgetOptions,
): Promise<void> {
  const client = getClient();
  if (memory.lifecycle === "forgotten") {
    const url = tombstoneURL(space, memory.ref);
    console.log(
      chalk.gray(
        `\n  ${memory.ref} is already forgotten.${url ? ` The tombstone: ${url}` : ""}\n`,
      ),
    );
    return;
  }
  const preview = await client.v2.memories.previewForget(memory.id);
  if (!preview.allowed)
    throw new ForgetError(
      preview.policy?.message ??
        `You can't forget ${memory.ref} in ${space.name}.`,
    );

  const json = options.format === "json";
  if (!json) {
    console.log(
      `\n  ${chalk.bold(memory.ref)} ${chalk.gray(`· ${space.name} · ${memory.state}`)}`,
    );
    console.log(`  ${memory.statement}\n`);
    console.log(`  ${removesLine(preview)}`);
    if (preview.carries.length)
      console.log(`  ${carriesLine(preview.carries)}`);
    console.log("  It cannot be undone. A tombstone stays.");
  }
  // --yes never stands in for the ID: a Forget is confirmed by naming it.
  const typed = options.confirm;
  let answer = typed;
  if (answer === undefined) {
    if (!process.stdin.isTTY)
      throw new ForgetError(
        `Confirm with --confirm ${memory.ref}: Forget can't be undone, so it needs the ID.`,
      );
    answer = await ask("  Type the ID to confirm: ");
  }
  if (answer.trim().toUpperCase() !== memory.ref.toUpperCase()) {
    if (typed !== undefined)
      throw new ForgetError(
        `--confirm ${typed} isn't ${memory.ref}. Nothing was forgotten.`,
      );
    console.log(
      chalk.gray(`  That isn't ${memory.ref}. Nothing was forgotten.\n`),
    );
    return;
  }

  let res: V2.ForgetResult;
  try {
    res = await client.v2.memories.forget(
      memory.id,
      {
        carries: preview.carries.map((c) => c.ref),
        ...(options.note?.trim() ? { note: options.note.trim() } : {}),
      },
      { ifMatch: preview.version, idempotencyKey: randomUUID(), via: "cli" },
    );
  } catch (err) {
    const carries = forgetCarriesOf(err);
    if (carries)
      throw new ForgetError(
        `What goes with ${memory.ref} changed while you confirmed (${carries.map((c) => c.ref).join(", ") || "nothing now"}). Nothing was forgotten; run it again.`,
      );
    if (err instanceof MemaxError && err.code === "edit_clash")
      throw new ForgetError(
        `${memory.ref} changed while you confirmed. Nothing was forgotten; run it again.`,
      );
    const refusal = refusalOf(err);
    if (refusal?.message) throw new ForgetError(refusal.message);
    if (err instanceof MemaxError) throw new ForgetError(err.message);
    throw err;
  }
  const tombstone = await awaitPropagation(res.tombstone);
  if (json) {
    console.log(JSON.stringify({ ...res, tombstone }, null, 2));
    return;
  }
  console.log();
  for (const line of reportLines(tombstone, space)) console.log(line);
  console.log();
}

export async function forgetCommand(
  id: string,
  options: ForgetOptions,
): Promise<void> {
  try {
    const found = await findMemory(id.trim(), options.space);
    if (!found) {
      // A V1 memory: V1's delete, as before.
      await deleteCommand(id, { yes: options.yes });
      return;
    }
    await forgetV2(found.memory, found.space, options);
  } catch (err) {
    const msg =
      err instanceof ForgetError
        ? err.message
        : `Forget failed: ${(err as Error).message}`;
    console.error(chalk.red(`  ${msg}`));
    process.exit(1);
  }
}

export function registerForgetCommand(program: Command): void {
  program
    .command("forget <id>")
    .description(
      "Forget a memory everywhere (M-0201): Memax, every compiled file and every agent. You type the ID to confirm",
    )
    .option("--space <space>", "The space, by slug or ID")
    .option("--confirm <id>", "Confirm without the prompt, by repeating the ID")
    .option("--note <note>", "Your own note on why; it stays on the tombstone")
    .option(
      "-y, --yes",
      "Skip the confirmation for a V1 memory (V2 needs --confirm <id>)",
    )
    .option("--format <format>", "Output format: text, json", "text")
    .action(forgetCommand);
}
