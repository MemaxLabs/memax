// Step 8 of memax init: compile (plan 25 §7.3, CompileDone). The space
// gets its targets (AGENTS.md, the CLAUDE.md shim, scoped Cursor rules and
// the ChatGPT copy-out) once it has a Brief; the repository is linked; and
// the files are written here, by the daemon or once by this command.
//
// Memax never writes over a file a person wrote (rule 6): a CLAUDE.md that
// is already here is made the person's, with one Memax block in it, when
// they agree (as memax link offers); an AGENTS.md that is already here is
// imported, then reported as a hand edit and left as it is, unless the
// person chooses to replace it.
import { existsSync, readFileSync } from "node:fs";
import { join } from "node:path";
import { randomUUID } from "node:crypto";
import chalk from "chalk";
import { MemaxError, type V2 } from "memax-sdk";
import { linkRepo } from "../daemon/registry.js";
import { writeMemaxYmlSpace } from "../project-context.js";
import { MANAGED_START } from "../daemon/compiler/managed-block.js";
import { MEMAX_HEADER } from "./split.js";
import type { InitDeps, InitOptions } from "./types.js";

/** The targets a space starts with (ledger.DefaultTargetKinds). */
const DEFAULT_KINDS: V2.TargetKind[] = [
  "agents_md",
  "claude_md",
  "cursor_mdc",
  "chatgpt",
];

/** Whether a file here was written by someone other than Memax. */
export function handWritten(root: string, path: string): boolean {
  const abs = join(root, path);
  if (!existsSync(abs)) return false;
  let content = "";
  try {
    content = readFileSync(abs, "utf8");
  } catch {
    return true;
  }
  const compiled = content
    .split(/\r?\n/)
    .some((l) => MEMAX_HEADER.test(l.trim()));
  return !compiled || content.includes(MANAGED_START);
}

export interface Targets {
  targets: V2.Target[];
  created: boolean;
  /** CLAUDE.md was here: Memax manages one block in it. */
  claudeOwned: boolean;
}

/** The space's targets: as they are, or the defaults, created once the space has a Brief. */
export async function ensureTargets(
  d: InitDeps,
  o: InitOptions,
  space: V2.Space,
  root: string,
): Promise<Targets> {
  let targets = (await d.memax.v2.targets.list(space.id)).items;
  const claudeHere = handWritten(root, "CLAUDE.md");
  let claudeOwned = false;
  const ownClaude = async () =>
    claudeHere &&
    (o.yes ||
      (d.interactive &&
        (await d.prompt.confirm(
          "  CLAUDE.md is yours. Let Memax keep one block in it and leave the rest alone? [Y/n] ",
          true,
        ))));
  if (targets.length > 0) {
    const shim = targets.find(
      (t) => t.kind === "claude_md" && t.sync_state !== "off",
    );
    if (shim && !shim.settings.user_owned && (await ownClaude())) {
      await d.memax.v2.targets.update(
        shim.id,
        {
          settings: { user_owned: true },
          reason: "CLAUDE.md already existed in the repository",
        },
        { idempotencyKey: randomUUID(), via: "cli", ifMatch: shim.version },
      );
      claudeOwned = true;
      targets = (await d.memax.v2.targets.list(space.id)).items;
    }
    return { targets, created: false, claudeOwned };
  }
  claudeOwned = await ownClaude();
  for (const kind of DEFAULT_KINDS) {
    try {
      await d.memax.v2.targets.create(
        space.id,
        kind === "claude_md" && claudeOwned
          ? {
              kind,
              settings: { user_owned: true },
              reason: "CLAUDE.md already existed in the repository",
            }
          : { kind },
        { idempotencyKey: `init-target:${space.id}:${kind}`, via: "cli" },
      );
    } catch (err) {
      // A kind this server doesn't compile yet stays out; the rest go on.
      if (!(err instanceof MemaxError) || err.status !== 400) throw err;
    }
  }
  targets = (await d.memax.v2.targets.list(space.id)).items;
  return { targets, created: true, claudeOwned };
}

/** Links this repository to the space, as memax link does. */
export function linkHere(
  d: InitDeps,
  space: V2.Space,
  root: string,
): { changed: boolean } {
  const yml = writeMemaxYmlSpace(root, space.slug);
  const previous = linkRepo(d.paths, {
    root,
    space_id: space.id,
    space_slug: space.slug,
  });
  return { changed: yml || previous?.space_id !== space.id };
}

/** `git status --short` for the files Memax writes, as CompileDone shows it. */
export function gitStatus(
  d: InitDeps,
  root: string,
  paths: string[],
): string[] {
  if (paths.length === 0) return [];
  const out = d.git(
    ["status", "--short", "--untracked-files=all", "--", ...paths],
    root,
  );
  return (out ?? "").split("\n").filter((l) => l.trim() !== "");
}

/** Offers to replace a hand-written file the compile left alone; never without a yes. */
export async function offerOverwrite(
  d: InitDeps,
  t: V2.Target,
): Promise<boolean> {
  if (!d.interactive || t.sync_state !== "drifted" || !t.path) return false;
  d.out("");
  d.out(
    `  ${chalk.yellow("○")} ${t.path} was written by hand, so Memax left it as it is.`,
  );
  d.out(
    chalk.gray(
      `    Its statements are in the record now, kept or waiting in Review. Replace it with the compiled Brief?`,
    ),
  );
  const yes = await d.prompt.confirm(`    Replace ${t.path}? [y/N] `, false);
  if (!yes) return false;
  await d.memax.v2.targets.overwrite(
    t.id,
    { reason: `Replaced by memax init: its statements were imported first` },
    { idempotencyKey: randomUUID(), via: "cli" },
  );
  return true;
}
