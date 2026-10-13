// memax verify <M-id>: answer a stale memory from the terminal (the
// stale-detection design §3.1). It says why the memory is stale, shows the
// change in this clone when a check found a condition failing (none of it
// left the machine, so only the machine can show it), and takes the
// answer: still true, or new words, kept as yours from the CLI
// (client-attested). Forget is the web's, which shows what it removes
// first, so it sends you there; so does a decision that needs a person on
// the web.
import { randomUUID } from "node:crypto";
import chalk from "chalk";
import type { Command } from "commander";
import {
  MemaxError,
  refusalOf,
  renameSuggestion,
  type Memax,
  type V2,
} from "memax-sdk";
import { getClient } from "../lib/client.js";
import { repoKeyOf } from "../lib/daemon/checks.js";
import { gitDirs, originUrl } from "../lib/daemon/git-tree.js";
import { daemonPaths, type DaemonPaths } from "../lib/daemon/paths.js";
import { findLinkedRepo } from "../lib/daemon/registry.js";
import { gitRoot } from "../lib/project-context.js";
import { ask, askPrefilled } from "../lib/prompt.js";
import { localChange, type LocalChange } from "../lib/stale/local-change.js";
import { conditionWords, evidenceWords, ownPath } from "../lib/stale/words.js";
import { resolveSpace, SpaceChoiceError } from "../lib/v2-space.js";
import { appBaseURL } from "./mcp-v2.js";
import { apiFailureMessage, MARK } from "./v2-output.js";

export interface VerifyOptions {
  space?: string;
  stillTrue?: boolean;
  update?: string;
  forget?: boolean;
  reason?: string;
  format?: string;
}

export interface VerifyDeps {
  memax: Memax;
  paths: DaemonPaths;
  cwd: string;
  out(...lines: string[]): void;
  /** Asks in the terminal, the answer's line holding `prefill`; absent without one. */
  ask?: (question: string, prefill?: string) => Promise<string>;
  /** The app's address, for what only the web does (tests set it). */
  appUrl?: string;
}

/** Thrown for a failure the command reports and exits 1 on. */
export class VerifyError extends Error {}

type Answer = "still_true" | "update" | "forget";

export async function verify(
  ref: string,
  o: VerifyOptions,
  d: VerifyDeps,
): Promise<number> {
  const asked = [o.stillTrue, o.update !== undefined, o.forget].filter(
    Boolean,
  ).length;
  if (asked > 1) {
    d.out(chalk.red("  Give one answer: --still-true, --update or --forget."));
    return 2;
  }
  if (o.update !== undefined && !o.update.trim()) {
    d.out(chalk.red('  --update takes what\'s true now: --update "<words>".'));
    return 2;
  }
  // A display ID in any case (m-0187); a memory id as it is.
  const id = /^[a-z]-\d+$/i.test(ref) ? ref.toUpperCase() : ref;
  const { report, space } = await findStale(id, o.space, d);
  const web = webLink(d.appUrl ?? appBaseURL(), space, report.ref);
  if (!report.stale) {
    if (o.format === "json") d.out(JSON.stringify({ report }, null, 2));
    else
      d.out(
        `  ${report.ref} isn't stale any more. Someone verified it, or it changed.`,
      );
    return 0;
  }

  const answer: Answer | null = o.stillTrue
    ? "still_true"
    : o.update !== undefined
      ? "update"
      : o.forget
        ? "forget"
        : null;

  if (o.format === "json" && (!answer || answer === "forget")) {
    d.out(
      JSON.stringify(
        {
          report,
          ...(answer === "forget" && web ? { forget_on_web: web } : {}),
        },
        null,
        2,
      ),
    );
    return 0;
  }
  if (o.format !== "json") {
    const root = cloneOf(space, d);
    const changes = root
      ? await Promise.all(report.causes.map((c) => changeFor(root, c)))
      : report.causes.map(() => null);
    d.out(...renderStale(report, changes));
  }

  let chosen = answer;
  if (!chosen) {
    if (!d.ask) {
      d.out(
        chalk.gray(
          `  Answer with --still-true, --update "<what's true now>" or --forget.`,
        ),
        "",
      );
      return 0;
    }
    chosen = await choose(report, d);
    if (!chosen) {
      d.out(chalk.gray("  Left as it is: still being verified."), "");
      return 0;
    }
  }

  if (chosen === "forget") {
    d.out(
      web
        ? `  Forget it on the web, which shows what forgetting it removes first: ${chalk.bold(web)}`
        : "  Forget it from its page in the app, which shows what forgetting it removes first.",
      "",
    );
    return 0;
  }

  let statement: string | undefined;
  if (chosen === "update") {
    statement = o.update?.trim();
    if (statement === undefined) {
      const suggested = suggestion(report);
      if (suggested)
        d.out(
          chalk.gray(
            "  Memax suggests new words from the rename. Edit them, then press Enter.",
          ),
        );
      statement = (
        await d.ask!("  What's true now: ", suggested ?? report.statement)
      ).trim();
    }
    if (!statement || statement === report.statement.trim()) {
      d.out(
        chalk.yellow("  Change the words first, or answer still true."),
        "",
      );
      return 1;
    }
  }

  const body: V2.VerifyInput =
    chosen === "update"
      ? { answer: "update", statement }
      : { answer: "still_true" };
  if (o.reason?.trim()) body.reason = o.reason.trim();
  let res: V2.VerifyResult;
  try {
    res = await d.memax.v2.memories.verify(report.ref, body, {
      space: space.id,
      ifMatch: report.version,
      idempotencyKey: randomUUID(),
      via: "cli",
    });
  } catch (err) {
    throw new VerifyError(failureWords(err, report.ref, web));
  }
  if (o.format === "json") {
    d.out(JSON.stringify(res, null, 2));
    return 0;
  }
  if (res.outcome !== "applied") {
    // A decision that needs a person on the web, say: nothing changed.
    d.out(
      chalk.yellow(
        `  ${res.policy.message ?? "Memax didn't apply it."}${web ? ` Verify it on the web: ${web}` : ""}`,
      ),
      "",
    );
    return 1;
  }
  d.out(
    chalk.green(
      `  ${MARK.done} ${
        chosen === "update"
          ? `Updated ${report.ref} and verified it.`
          : `Verified ${report.ref}. It's kept as it is.`
      }`,
    ),
    chalk.gray("  You can undo it in the app for 10 minutes."),
    "",
  );
  return 0;
}

/** The memory's stale report, in the space given, this repository's, or whichever of yours holds it. */
async function findStale(
  ref: string,
  only: string | undefined,
  d: VerifyDeps,
): Promise<{ report: V2.StaleReport; space: V2.Space }> {
  let candidates: V2.Space[];
  try {
    const { space } = await resolveSpace(d.memax, {
      space: only,
      cwd: d.cwd,
      paths: d.paths,
    });
    candidates = [space];
  } catch (err) {
    if (!(err instanceof SpaceChoiceError) || only) throw err;
    candidates = (await d.memax.v2.spaces.list()).items.filter(
      (s) => s.v2_enabled_at,
    );
  }
  const found: { report: V2.StaleReport; space: V2.Space }[] = [];
  for (const space of candidates) {
    try {
      const report = await d.memax.v2.memories.stale(ref, { space: space.id });
      found.push({ report, space });
    } catch (err) {
      if (!(err instanceof MemaxError) || err.status !== 404) throw err;
    }
  }
  if (found.length === 0)
    throw new VerifyError(
      candidates.length === 1
        ? `${ref} isn't in ${candidates[0].slug}. It may have been forgotten, or it's in another space: --space <slug>.`
        : `${ref} isn't in any of your spaces on V2. It may have been forgotten.`,
    );
  if (found.length > 1)
    throw new VerifyError(
      `${ref} is in more than one of your spaces (${found.map((f) => f.space.slug).join(", ")}). Say which with --space.`,
    );
  return found[0];
}

/** This repository, when it is a clone of the space's (linked or not). */
function cloneOf(space: V2.Space, d: VerifyDeps): string | null {
  const root = gitRoot(d.cwd);
  if (!root) return null;
  if (findLinkedRepo(d.paths, root)?.space_id === space.id) return root;
  const dirs = gitDirs(root);
  const url = dirs ? originUrl(dirs) : null;
  return url &&
    space.repository &&
    repoKeyOf(url) === repoKeyOf(space.repository)
    ? root
    : null;
}

/** The change in this clone behind a check's cause, when git can say. */
async function changeFor(
  root: string,
  cause: V2.StaleCause,
): Promise<LocalChange | null> {
  if (cause.kind !== "check") return null;
  const e = cause.evidence;
  const own = ownPath(cause.condition);
  if (e?.change === "no_match")
    return localChange(root, cause.commit, [own], true);
  const path = e?.path ?? own;
  const paths = e?.renamed_to ? [path, e.renamed_to] : [path];
  return localChange(root, cause.commit, paths);
}

/** Why it's stale, and what changed here, as the terminal shows it. */
export function renderStale(
  r: V2.StaleReport,
  changes: Array<LocalChange | null>,
): string[] {
  const since = r.flagged ? ` · stale since ${day(r.flagged.occurred_at)}` : "";
  const lines = [
    "",
    `  ${chalk.bold(`Verify ${r.ref}`)}${chalk.gray(since)}`,
    "",
  ];
  lines.push(`  ${r.statement}`);
  const kept = r.kept
    ? `${r.kept.action === "verified" ? "verified" : "kept"} · ${day(r.kept.occurred_at)} · `
    : "";
  const reads =
    r.reads_since === 0
      ? "not read since"
      : r.reads_since === 1
        ? "read once since"
        : `read ${r.reads_since} times since`;
  lines.push(chalk.gray(`  ${kept}${reads}`), "", "  What changed");
  if (r.causes.length === 0)
    lines.push(
      `  ${chalk.yellow(MARK.waiting)} It was flagged as stale. Check it against its sources before you answer.`,
    );
  r.causes.forEach((cause, i) => {
    if (cause.kind === "date") {
      lines.push(
        `  ${chalk.yellow(MARK.waiting)} Its date to check again, ${day(cause.stale_after)}, has passed.`,
      );
      return;
    }
    lines.push(
      `  ${chalk.yellow(MARK.waiting)} ${evidenceWords(cause.condition, cause.evidence, cause.branch)}`,
      chalk.gray(
        `    It relies on: ${conditionWords(cause.condition)} · in ${cause.commit.slice(0, 7)}${cause.checked_at ? ` · checked ${day(cause.checked_at)}` : ""}`,
      ),
    );
    const c = changes[i];
    if (c?.kind === "missing") {
      lines.push(
        chalk.gray(
          `    ${cause.commit.slice(0, 7)} isn't in this clone yet: git fetch, then run memax verify ${r.ref} again to see the change.`,
        ),
      );
    } else if (c) {
      lines.push(
        "",
        `    The change here: ${chalk.bold(c.commit)} ${safe(c.subject)}${chalk.gray(` · ${safe(c.author)}, ${c.date}`)}`,
        ...c.lines.map((l) => `    ${diffLine(safe(l))}`),
      );
      if (c.more > 0)
        lines.push(
          chalk.gray(
            `    … ${c.more} more lines: git show ${c.commit} -- ${ownPath(cause.condition)}`,
          ),
        );
    }
  });
  lines.push("");
  return lines;
}

/** The three answers, picked by number. */
async function choose(
  r: V2.StaleReport,
  d: VerifyDeps,
): Promise<Answer | null> {
  const byDate =
    r.causes.length > 0 && r.causes.every((c) => c.kind === "date");
  d.out(
    "  What's true now?",
    `    1  Still true, keep it  ${chalk.gray(
      byDate
        ? "Its date to check again goes. Nothing else changes."
        : "It's checked again when the source next changes.",
    )}`,
    `    2  Update it            ${chalk.gray("Write what's true now. It's kept as a new version.")}`,
    `    3  Forget it            ${chalk.gray("On the web, which shows what it removes first.")}`,
    "",
  );
  for (;;) {
    const raw = (
      await d.ask!("  Answer (1-3), or press Enter to leave it: ")
    ).trim();
    if (!raw) return null;
    if (raw === "1") return "still_true";
    if (raw === "2") return "update";
    if (raw === "3") return "forget";
    d.out(chalk.yellow("  Choose what's true now: 1, 2 or 3."));
  }
}

/** New words a rename suggests without a model: the old path replaced by the new one. */
function suggestion(r: V2.StaleReport): string | null {
  for (const c of r.causes) {
    if (c.kind !== "check" || c.evidence?.change !== "renamed") continue;
    const from = c.evidence.path ?? ownPath(c.condition);
    const s = renameSuggestion(r.statement, from, c.evidence.renamed_to ?? "");
    if (s) return s;
  }
  return null;
}

function failureWords(err: unknown, ref: string, web: string): string {
  const refusal = refusalOf(err);
  if (refusal) {
    const onWeb =
      refusal.code?.endsWith("_needs_web") || refusal.code === "needs_passkey";
    return `${refusal.message ?? "Memax refused it."}${onWeb && web ? ` Verify it on the web: ${web}` : ""}`;
  }
  if (err instanceof MemaxError && err.status === 412)
    return `${ref} changed since you read it. Run memax verify ${ref} again to see it as it is.`;
  return apiFailureMessage(err, `memax verify ${ref}`);
}

/** The memory's page with VerifyStale open. */
function webLink(base: string, space: V2.Space, ref: string): string {
  return base
    ? `${base}/${encodeURIComponent(space.slug)}/memories/${encodeURIComponent(ref)}?overlay=verify`
    : "";
}

/** "Sep 11". */
function day(iso: string): string {
  const d = new Date(iso);
  return Number.isNaN(d.getTime())
    ? ""
    : d.toLocaleDateString("en-US", { month: "short", day: "numeric" });
}

/** A repository's text, without the control characters a terminal would act on. */
function safe(s: string): string {
  // eslint-disable-next-line no-control-regex
  return s.replace(/[\u0000-\u0008\u000b-\u001f\u007f-\u009f]/g, "�");
}

function diffLine(l: string): string {
  if (l.startsWith("+") && !l.startsWith("+++")) return chalk.green(l);
  if (l.startsWith("-") && !l.startsWith("---")) return chalk.red(l);
  if (l.startsWith("@@")) return chalk.cyan(l);
  return chalk.gray(l);
}

export function registerVerifyCommand(program: Command): void {
  program
    .command("verify <id>")
    .description(
      "Answer a memory marked stale (M-0187): see why, and keep it, update it or forget it",
    )
    .option("--space <slug>", "The space (default: this repository's)")
    .option("--still-true", "It's still true: keep it as it is")
    .option("--update <words>", "It's true with these words: keep them")
    .option("--forget", "It isn't true: forget it (on the web)")
    .option("--reason <why>", "Why, for the receipt")
    .option("--format <format>", "Output format: text, json", "text")
    .action(async (id: string, opts: VerifyOptions) => {
      const tty = process.stdin.isTTY && process.stdout.isTTY;
      try {
        process.exitCode = await verify(id, opts, {
          memax: getClient(),
          paths: daemonPaths(),
          cwd: process.cwd(),
          out: (...l: string[]) => {
            for (const line of l) console.log(line);
          },
          ...(tty
            ? {
                ask: (q: string, prefill?: string) =>
                  prefill === undefined ? ask(q) : askPrefilled(q, prefill),
              }
            : {}),
        });
      } catch (err) {
        const msg =
          err instanceof VerifyError
            ? err.message
            : apiFailureMessage(err, `memax verify ${id}`);
        console.error(chalk.red(`  ${msg}`));
        process.exitCode = 1;
      }
    });
}
