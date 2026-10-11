// memax link --home / memax unlink --home: write the Personal space into
// this machine's agents' own files, and take it off again (personal-layer
// design §6.1, §6.6).
import { randomUUID } from "node:crypto";
import { hostname } from "node:os";
import chalk from "chalk";
import type { Memax, V2 } from "memax-sdk";
import { controlRequest, type HomeOffReason } from "../lib/daemon/control.js";
import {
  readHomeLink,
  removeHomeLink,
  writeHomeLink,
  type HomeLink,
} from "../lib/daemon/home-link.js";
import { tilde } from "../lib/daemon/home-place.js";
import { daemonUnsupported, type DaemonPaths } from "../lib/daemon/paths.js";
import type { OneShotDelivery } from "../lib/daemon/oneshot.js";
import type { HomeSnapshot } from "../lib/daemon/snapshot.js";
import {
  CONSENT_QUESTION,
  consentLines,
  planHomeFiles,
} from "../lib/home/files.js";
import { compile } from "./compile.js";
import { apiFailureMessage, MARK } from "./v2-output.js";

export interface LinkHomeOptions {
  name?: string;
  dryRun?: boolean;
  yes?: boolean;
  timeout?: string;
}

export interface HomeDeps {
  memax: Memax;
  paths: DaemonPaths;
  cwd: string;
  out: (line: string) => void;
  /** Whether a person is at a terminal to answer. */
  interactive: boolean;
  /** The home folder, a real path. */
  home: string;
  env: Record<string, string | undefined>;
  platform: NodeJS.Platform;
  deviceId: string;
  /** Who is signed in (the session's `sub`), recorded in home.json. */
  personId: string | null;
  hostname?: string;
  /** Asks the consent's question (tests answer it). */
  confirm(question: string): Promise<boolean>;
  /** In-process delivery when no daemon runs (tests swap it). */
  oneShot?: () => Promise<OneShotDelivery | null>;
  /** A fixed wait between checks (tests). */
  pollMs?: number;
}

/** The short host name, as a machine is called by default. */
export function machineName(host = hostname()): string {
  const short = host.split(".")[0]?.trim() ?? "";
  return (short || "this machine").slice(0, 80);
}

/** Why this machine can't take a home link at all, or null. */
export function homeLinkRefusal(
  o: LinkHomeOptions,
  d: Pick<HomeDeps, "env" | "platform" | "interactive">,
): string | null {
  const windows = daemonUnsupported(d.platform);
  if (windows) return windows;
  if (d.env.CI) {
    return "memax link --home doesn't run under CI (CI is set): on a shared or CI machine, a home link hands your Personal space to whoever uses it.";
  }
  if (!d.interactive && !o.yes && !o.dryRun) {
    return "Run memax link --home in a terminal, so you see which files it writes, or add --yes to link without asking.";
  }
  return null;
}

/** The person's Personal space on the V2 record, or why not. */
async function personal(memax: Memax): Promise<V2.Space | string> {
  const spaces = (await memax.v2.spaces.list()).items;
  const sp = spaces.find((s) => s.kind === "personal" && s.role === "owner");
  if (!sp)
    return "You have no Personal space yet. Sign in on memax.app once, then run memax link --home again.";
  if (sp.v2_enabled_at) return sp;
  const status = await memax.v2.spaces.switchStatus(sp.id);
  if (status.preview.notes.total > 0) {
    return `Your Personal space (${sp.slug}) is still on Memax V1. Switch it first: memax switch --space ${sp.slug}, then memax link --home.`;
  }
  // Empty on V1: nothing moves, so it moves to V2 here, as memax init does.
  return (
    await memax.v2.spaces.switchToV2(sp.id, {
      idempotencyKey: randomUUID(),
      via: "cli",
    })
  ).space;
}

export async function linkHome(
  o: LinkHomeOptions,
  d: HomeDeps,
): Promise<number> {
  const refusal = homeLinkRefusal(o, d);
  if (refusal) {
    d.out(chalk.red(`  ${refusal}`));
    return 1;
  }
  const files = planHomeFiles({ home: d.home, env: d.env });
  if (files.length === 0) {
    d.out(
      chalk.yellow(
        "  Neither Claude Code (~/.claude) nor Codex (~/.codex) is set up on this machine, so there's nothing to write yet.",
      ),
    );
    return 1;
  }
  const refused = files.filter((f) => f.refusal);
  const agents = files.filter((f) => !f.refusal && f.kind !== "agents_md");
  for (const f of refused) {
    d.out(
      `  ${chalk.yellow("○")} ${tilde(d.home, f.abs)}: ${f.refusal}. ${chalk.gray("Memax leaves it alone.")}`,
    );
  }
  if (agents.length === 0) {
    d.out(chalk.red("  Nothing to link: every file was refused."));
    return 1;
  }
  const space = await personal(d.memax);
  if (typeof space === "string") {
    d.out(chalk.red(`  ${space}`));
    return 1;
  }
  const approved = files.filter((f) => !f.refusal);
  for (const line of consentLines(d.home, approved)) d.out(line);
  if (o.dryRun) {
    d.out("  proposals, and memax unlink --home takes them off.");
    d.out("");
    d.out(chalk.gray("  Nothing was linked or written (--dry-run)."));
    d.out("");
    return 0;
  }
  if (o.yes) d.out("  proposals, and memax unlink --home takes them off.");
  else if (!(await d.confirm(CONSENT_QUESTION))) {
    d.out("");
    d.out(chalk.gray("  Nothing was linked or written."));
    d.out("");
    return 0;
  }

  const res = await d.memax.v2.machines.link(
    space.id,
    {
      device_id: d.deviceId,
      name: o.name?.trim().slice(0, 80) || machineName(d.hostname),
      platform: d.platform,
      files: approved.map((f) => ({ kind: f.kind, path: f.path })),
    },
    { idempotencyKey: randomUUID(), via: "cli" },
  );
  const link = writeHomeLink(d.paths, {
    space_id: space.id,
    space_slug: space.slug,
    machine_id: res.machine.id,
    ...(d.personId ? { person_id: d.personId } : {}),
    home: d.home,
    files: approved.flatMap((f) => {
      const t = res.targets.find((x) => x.kind === f.kind && x.path === f.path);
      return t
        ? [{ target_id: t.id, kind: f.kind, path: f.path, abs: f.abs }]
        : [];
    }),
  });
  d.out("");
  d.out(
    `  ${chalk.green(MARK.done)} Linked this machine (${res.machine.label}, ${res.machine.name}) to ${chalk.bold(space.slug)}.`,
  );

  // Start or wake the daemon, and wait for the first delivery, as memax
  // compile does.
  await controlRequest(d.paths, { cmd: "reload" }, 1_500);
  return compile(
    { space: space.id, pending: true, timeout: o.timeout },
    {
      memax: d.memax,
      paths: d.paths,
      cwd: d.home,
      out: d.out,
      oneShot: d.oneShot,
      pollMs: d.pollMs,
      onlyHome: link.machine_id,
    },
  );
}

/** What taking the layer off did to each file, as people read it. */
export function offLines(
  home: string,
  snap: HomeSnapshot | null,
  link: HomeLink,
): string[] {
  const out: string[] = [];
  for (const f of link.files) {
    const t = snap?.targets.find((x) => x.id === f.target_id);
    const where = tilde(home, f.abs);
    const kept = t?.state === "off_file_kept" || t?.state === "off_block_kept";
    const yours =
      t?.state === "off_block_removed" && !t.detail?.includes("file it made");
    out.push(
      kept
        ? `    ${chalk.yellow("○")} ${where}  ${chalk.gray(`left as it is: ${t?.detail?.replace(/^stopped; /, "") ?? "it has hand edits"}`)}`
        : `    ${chalk.green(MARK.done)} ${where}  ${chalk.gray(yours ? "Memax's block taken out; your lines stay" : "taken out")}`,
    );
  }
  return out;
}

/** Asks the running daemon, else this process, to take the layer off. */
async function homeOffHere(
  d: Pick<UnlinkHomeDeps, "paths" | "oneShot">,
  reason: HomeOffReason,
): Promise<HomeSnapshot | null> {
  const ask = () =>
    controlRequest(
      d.paths,
      { cmd: "home_off", reason },
      10_000,
    ) as Promise<HomeSnapshot | null>;
  const snap = await ask();
  if (snap !== null) return snap;
  const local = d.oneShot ? await d.oneShot() : null;
  if (!local) return ask(); // a daemon started meanwhile
  try {
    return await local.homeOff(reason);
  } finally {
    await local.close();
  }
}

/**
 * memax logout signs this machine out, which takes the layer off it
 * (decision 3), as a refused refresh does in the daemon. home.json stays,
 * so memax login puts it back.
 */
export async function homeSignedOut(
  d: Pick<UnlinkHomeDeps, "paths" | "oneShot" | "out">,
): Promise<void> {
  const link = readHomeLink(d.paths);
  if (!link) return;
  const snap = await homeOffHere(d, "signed_out");
  d.out(
    `  Took your Personal space off this machine's home; memax login puts it back.`,
  );
  for (const line of offLines(link.home, snap, link)) d.out(line);
  d.out("");
}

/**
 * memax login as someone other than the person this machine's home was
 * linked for: their layer comes off before anything else (the same
 * take-off as logout), and their link goes, since it isn't the new
 * person's. The same person signing in again changes nothing. True when
 * it took the layer off.
 */
export async function homeSwitchedPerson(
  next: string | null,
  d: Pick<UnlinkHomeDeps, "paths" | "oneShot" | "out">,
): Promise<boolean> {
  const link = readHomeLink(d.paths);
  if (!link?.person_id || !next || next === link.person_id) return false;
  await homeOffHere(d, "other_person");
  if (readHomeLink(d.paths)) removeHomeLink(d.paths);
  d.out(
    "  Took the previous account's Personal space off this machine's home.",
  );
  return true;
}

export interface UnlinkHomeDeps {
  memax: Memax;
  paths: DaemonPaths;
  out: (line: string) => void;
  oneShot?: () => Promise<OneShotDelivery | null>;
}

/**
 * Unlinks this machine on the server (every home file stops), then takes
 * the layer off here: the files Memax owns that still hold what it wrote,
 * and its blocks; hand-edited ones stay. home.json goes.
 */
export async function unlinkHome(d: UnlinkHomeDeps): Promise<number> {
  const link = readHomeLink(d.paths);
  if (!link) {
    d.out(chalk.gray("  This machine's home isn't linked."));
    return 0;
  }
  let note = "";
  try {
    await d.memax.v2.machines.unlink(
      link.machine_id,
      {},
      { idempotencyKey: randomUUID(), via: "cli" },
    );
  } catch (err) {
    const status = (err as { status?: number }).status;
    if (status === 401) {
      note =
        "You're signed out, so Memax still lists this machine. Unlink it in Settings on memax.app.";
    } else if (status !== 404) {
      d.out(chalk.red(`  ${apiFailureMessage(err)} Nothing changed here.`));
      return 1;
    }
  }
  const snap = await homeOffHere(d, "unlinked");
  if (readHomeLink(d.paths)) removeHomeLink(d.paths);
  d.out("");
  d.out(
    `  ${chalk.green(MARK.done)} Unlinked this machine's home from ${chalk.bold(link.space_slug)}.`,
  );
  for (const line of offLines(link.home, snap, link)) d.out(line);
  if (note) d.out(chalk.yellow(`  ${note}`));
  d.out("");
  return 0;
}
