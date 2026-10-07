// Step 7 of memax init: settle in the terminal (plan 25 §7.3, Cleanup and
// ReviewImport). Each disagreement is settled once, as a group; then the
// person may keep, in one go, the statements from their own files that
// the judge and the conflict check found nothing against. Everything
// else waits in Review on the web: what an agent wrote, what cites an
// outside source, lines from another branch, lines that had hidden
// characters, and (in team spaces) decisions, which need a person on the
// web (D15). A keep from the terminal is client-attested.
import { randomUUID } from "node:crypto";
import chalk from "chalk";
import { MemaxError, refusalOf, type V2 } from "memax-sdk";
import type { InitDeps, InitOptions } from "./types.js";

export interface Open {
  space: V2.Space;
  importId: string;
  conflict: V2.ImportConflict;
  members: V2.Memory[];
}

/** The open disagreements of the views, with their members' memories. */
export function openConflicts(space: V2.Space, views: V2.ImportView[]): Open[] {
  const out: Open[] = [];
  for (const v of views) {
    const byId = new Map(v.memories.map((m) => [m.memory.id, m.memory]));
    for (const c of v.conflicts) {
      if (c.state !== "open") continue;
      const members = c.members
        .map((p) => byId.get(p.id))
        .filter((m): m is V2.Memory => !!m);
      if (members.filter((m) => m.lifecycle === "proposed").length >= 2)
        out.push({ space, importId: v.import.id, conflict: c, members });
    }
  }
  return out;
}

function sourceOf(m: V2.Memory): string {
  return (
    (m.sources ?? [])
      .filter((s) => s.kind === "file")
      .map((s) => s.ref)
      .join(" · ") || m.ref
  );
}

/** Why a member can't be kept from the terminal, if it can't. */
function webOnly(space: V2.Space, m: V2.Memory): string | null {
  if (m.trust === "external") return "an outside source: keep it on the web";
  if (space.kind === "team" && m.kind === "decision")
    return "a decision: team spaces decide on the web";
  return null;
}

export interface Settled {
  settled: number;
  left: number;
}

/** Asks about each disagreement in turn; Enter leaves it for Review. */
export async function settleConflicts(
  d: InitDeps,
  o: InitOptions,
  opens: Open[],
): Promise<Settled> {
  const res: Settled = { settled: 0, left: 0 };
  for (const [i, op] of opens.entries()) {
    const c = op.conflict;
    const members = op.members.filter((m) => m.lifecycle === "proposed");
    d.out("");
    d.out(
      `  ${chalk.red("◐")} ${i + 1} of ${opens.length}  ${chalk.bold(c.subject || "They disagree")}${chalk.gray(`   ${members.length} statements disagree`)}`,
    );
    members.forEach((m, j) => {
      const why = webOnly(op.space, m);
      d.out(
        `    ${j + 1}  ${m.statement}${chalk.gray(`   ${sourceOf(m)}`)}${why ? chalk.yellow(`  (${why})`) : ""}`,
      );
    });
    const anyWebOnly = members.some((m) => webOnly(op.space, m));
    const choices = [`1–${members.length} keep that one`];
    if (!anyWebOnly) choices.push("a  all hold", "o  leave it open");
    if (c.suggestion && !anyWebOnly) {
      d.out(`    s  ${c.suggestion}${chalk.gray("   suggested")}`);
      choices.push("s  the suggestion");
    }
    if (!d.interactive || o.yes) {
      // Settling is a person's choice: never taken for them.
      res.left++;
      continue;
    }
    d.out(chalk.gray(`    ${choices.join(" · ")} · Enter  decide in Review`));
    const answer = (await d.prompt.ask("    Which holds? ")).toLowerCase();
    let input: V2.SettleImportConflictInput | null = null;
    const n = Number(answer);
    if (Number.isInteger(n) && n >= 1 && n <= members.length) {
      const m = members[n - 1];
      if (webOnly(op.space, m)) {
        d.out(
          chalk.yellow(
            `    ${m.ref} is ${webOnly(op.space, m)}. It waits for you in Review.`,
          ),
        );
        res.left++;
        continue;
      }
      input = { choice: "keep_one", keep: m.ref };
    } else if (answer === "a" && !anyWebOnly) input = { choice: "keep_all" };
    else if (answer === "o" && !anyWebOnly) input = { choice: "leave_open" };
    else if (answer === "s" && c.suggestion && !anyWebOnly)
      input = { choice: "keep_suggestion" };
    if (!input) {
      res.left++;
      continue;
    }
    try {
      await d.memax.v2.imports.settle(op.space.id, op.importId, c.n, input, {
        idempotencyKey: randomUUID(),
        via: "cli",
      });
      d.out(`    ${chalk.green("●")} ${settledWords(input, members, c)}`);
      res.settled++;
    } catch (err) {
      const why =
        refusalOf(err)?.message ??
        (err instanceof MemaxError ? err.message : String(err));
      d.out(chalk.yellow(`    Couldn't settle it here: ${why}`));
      res.left++;
    }
  }
  return res;
}

function settledWords(
  input: V2.SettleImportConflictInput,
  members: V2.Memory[],
  c: V2.ImportConflict,
): string {
  switch (input.choice) {
    case "keep_one":
      return `Kept ${input.keep}; the other ${members.length - 1 === 1 ? "one is" : `${members.length - 1} are`} rejected.`;
    case "keep_all":
      return `Kept all ${members.length}: they don't disagree after all.`;
    case "leave_open":
      return "Kept as open questions: agents read that it isn't decided.";
  }
  return `Kept the suggestion: ${c.suggestion}`;
}

/**
 * The proposals the terminal may keep in bulk: the ones that agree (the
 * server's `bulk`), from the person's own files (repository or person
 * trust), and not a team space's decisions.
 */
export function bulkCandidates(
  space: V2.Space,
  views: V2.ImportView[],
): V2.ImportMemory[] {
  const seen = new Set<string>();
  const out: V2.ImportMemory[] = [];
  for (const v of views) {
    for (const im of v.memories) {
      const m = im.memory;
      if (!im.bulk || seen.has(m.id)) continue;
      if (m.trust !== "person" && m.trust !== "repository") continue;
      if (space.kind === "team" && m.kind === "decision") continue;
      seen.add(m.id);
      out.push(im);
    }
  }
  return out;
}

/** What waits in Review after the bulk keep: proposals the views name that are still proposed. */
export function waiting(views: V2.ImportView[], kept: Set<string>): number {
  const ids = new Set<string>();
  for (const v of views)
    for (const im of v.memories)
      if (im.memory.lifecycle === "proposed" && !kept.has(im.memory.id))
        ids.add(im.memory.id);
  return ids.size;
}

/** Keeps the candidates, 200 a request; returns the ids kept. */
export async function keepInBulk(
  d: InitDeps,
  space: V2.Space,
  cands: V2.ImportMemory[],
  run: string,
): Promise<Set<string>> {
  const kept = new Set<string>();
  for (let i = 0; i < cands.length; i += 200) {
    const chunk = cands.slice(i, i + 200);
    const res = await d.memax.v2.memories.keepMany(
      space.id,
      {
        items: chunk.map((c) => ({
          memory: c.memory.id,
          version: c.memory.version,
        })),
        reason: "Kept with memax init: the files agree.",
      },
      { idempotencyKey: `${run}:keep:${space.id}:${i}`, via: "cli" },
    );
    res.items.forEach((it, j) => {
      if (it.outcome === "applied") kept.add(chunk[j].memory.id);
    });
  }
  return kept;
}

const SECTIONS: Array<{ section: V2.Section; key: string; heading: string }> = [
  { section: "decisions", key: "decisions", heading: "Decisions" },
  { section: "conventions", key: "conventions", heading: "Conventions" },
  { section: "preferences", key: "preferences", heading: "Preferences" },
  { section: "open_question", key: "open", heading: "Open questions" },
];

/** The space's first Brief, from what is kept, by section; null when it has one, or nothing is kept. */
export async function firstBrief(
  d: InitDeps,
  space: V2.Space,
  repository: string | null,
): Promise<V2.Brief | null> {
  try {
    await d.memax.v2.briefs.get(space.id);
    return null;
  } catch (err) {
    if (!(err instanceof MemaxError) || err.status !== 404) throw err;
  }
  const kept: V2.Memory[] = [];
  let cursor: string | undefined;
  do {
    const page = await d.memax.v2.memories.list(space.id, {
      state: "kept",
      limit: 200,
      cursor,
    });
    kept.push(...page.items);
    cursor = page.has_more ? page.next_cursor : undefined;
  } while (cursor && kept.length < 1600);
  if (kept.length === 0) return null;
  kept.reverse(); // oldest first: the files' own order
  const sections = SECTIONS.map((s) => ({
    key: s.key,
    heading: s.heading,
    items: kept
      .filter((m) => m.section === s.section)
      .slice(0, 400)
      .map((m) => ({ ref: m.ref })),
  })).filter((s) => s.items.length > 0);
  const res = await d.memax.v2.briefs.revise(
    space.id,
    {
      title: space.name,
      summary: repository
        ? `What every agent working on ${repository} should know.`
        : "What every agent working here should know.",
      sections,
      reason: "The first Brief, from the agent files memax init read.",
    },
    { idempotencyKey: `init-brief:${space.id}`, via: "cli" },
  );
  return res.brief;
}
