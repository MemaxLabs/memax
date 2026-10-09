// npx memax-cli init: first file in under five minutes (plan 25 §7.3).
// 1 detect · 2 sign in · 3 connect · 4 scan locally · 5 upload · 6 judge
// · 7 settle · 8 compile. Each step is timed against its budget
// (--timing). Running it again in a set-up repository is safe and quick:
// what the record already has is skipped, and it says what is done.
import { randomUUID } from "node:crypto";
import chalk from "chalk";
import { MemaxError, type V2 } from "memax-sdk";
import { apiFailureMessage } from "../../commands/v2-output.js";
import { gitRoot } from "../project-context.js";
import { connectAgents } from "./connect.js";
import { findFiles } from "./files.js";
import {
  ensureTargets,
  gitStatus,
  linkHere,
  offerOverwrite,
} from "./finish.js";
import { branchContext } from "./git.js";
import { totalHidden } from "./hidden.js";
import {
  renderAgents,
  renderCompiled,
  renderFiles,
  renderHandled,
  renderTimings,
} from "./render.js";
import { scanFiles, type ScanResult } from "./scan.js";
import {
  bulkCandidates,
  firstBrief,
  keepInBulk,
  openConflicts,
  settleConflicts,
  waiting,
  type Open,
} from "./settle.js";
import { InitError, personalSpace, projectSpace } from "./spaces.js";
import {
  emptyReport,
  type InitDeps,
  type InitOptions,
  type InitReport,
} from "./types.js";
import { upload, waitForJudge, type Uploaded } from "./upload.js";

/** Runs memax init; the exit code. */
export async function runInit(o: InitOptions, d0: InitDeps): Promise<number> {
  const json = o.format === "json";
  const d: InitDeps = json ? { ...d0, out: () => {}, interactive: false } : d0;
  const report = emptyReport();
  let step: { name: string; budget: number | null; at: number } | null = null;
  const begin = (name: string, budget: number | null) => {
    end();
    step = { name, budget, at: d.now() };
  };
  const end = () => {
    if (step)
      report.timings.push({
        step: step.name,
        ms: Math.round(d.now() - step.at),
        budgetMs: step.budget,
      });
    step = null;
  };
  const finish = (code: number) => {
    end();
    if (o.timing && !json)
      for (const l of renderTimings(report.timings)) d.out(l);
    if (json) d0.out(JSON.stringify(report, null, 2));
    return code;
  };
  try {
    return finish(await flow(o, d, report, begin, end));
  } catch (err) {
    end();
    // Init is safe to run again, so a rate limit says to wait and do that.
    const msg =
      err instanceof MemaxError && err.isRateLimited
        ? apiFailureMessage(err, "memax init")
        : err instanceof InitError || err instanceof MemaxError
          ? err.message
          : String(err);
    if (json) d0.out(JSON.stringify({ ...report, error: msg }, null, 2));
    else d.out(chalk.red(`  ${msg}`));
    return 1;
  }
}

type Begin = (name: string, budget: number | null) => void;

async function flow(
  o: InitOptions,
  d: InitDeps,
  report: InitReport,
  begin: Begin,
  end: () => void,
): Promise<number> {
  const run = `init-${randomUUID().slice(0, 8)}`;

  // 1. Detect: the repository, its branch, and every agent file.
  begin("detect", 2_000);
  const root = gitRoot(d.cwd);
  const branch = root ? branchContext(root, d.git) : null;
  const remote = root
    ? d.git(["remote", "get-url", "origin"], root)?.trim() || null
    : null;
  const found = findFiles({
    home: d.home,
    root,
    personal: o.personal !== false,
  });
  d.out("");
  if (!root)
    d.out(
      chalk.yellow(
        "  Not in a git repository: only this machine's memory is read. Run memax init in a repository to compile its files.",
      ),
    );

  if (o.dryRun) {
    begin("scan", 5_000);
    const scanned = await scanFiles(found.files, { root, git: d.git, branch });
    end();
    printScan(d, scanned, new Map());
    d.out("");
    d.out(
      chalk.gray(
        `  A dry run: nothing left this machine. ${countItems(scanned)} statements would be uploaded as proposals.`,
      ),
    );
    fillFiles(report, scanned);
    return 0;
  }

  // 2. Sign in.
  begin("sign in", 30_000);
  const spaces = await signedIn(d);
  const project = root ? await projectSpace(d, o, root, remote, spaces) : null;
  const machine = found.files.some((f) => f.location === "home")
    ? await personalSpace(d, spaces)
    : null;
  if (project) {
    report.space = { slug: project.space.slug, created: project.created };
    if (!project.created)
      report.done.push(`The repository's space is ${project.space.slug}.`);
  }
  if (machine?.space)
    report.personal = { slug: machine.space.slug, switched: machine.switched };
  const me = await d.memax.auth
    .me()
    .then((m) => m.user.display_name || m.user.name)
    .catch(() => "");
  d.out(
    `  Signed in${me ? ` as ${me}` : ""}${project ? ` · space ${chalk.bold(project.space.slug)}${project.created ? chalk.gray(" (new project space)") : ""}` : ""}`,
  );

  // 3. Connect the agents.
  begin("connect", 30_000);
  const targetSpaces = [project?.space, machine?.space].filter(
    (s): s is V2.Space => !!s,
  );
  let declined = false;
  const agents = await connectAgents(
    d,
    root,
    targetSpaces,
    async (need) => {
      if (o.connect === false) return false;
      const yes =
        o.yes ||
        (d.interactive &&
          (await d.prompt.confirm(
            `  Connect ${need.map((a) => a.name).join(", ")} (writes their MCP settings and session-start hooks)? [Y/n] `,
            true,
          )));
      declined = !yes;
      return yes;
    },
    { hooks: o.connect !== false },
  );
  report.agents = agents.rows;
  for (const l of renderAgents(agents.rows)) d.out(l);
  if (declined)
    d.out(
      chalk.gray(
        "    Their MCP settings weren't changed. Run memax init --yes, or memax setup --mcp, to connect them.",
      ),
    );
  if (agents.rows.some((r) => r.mcp === "present"))
    report.done.push("The agents' MCP settings name Memax.");

  // 4. Scan locally: split, secrets, hidden characters, trust.
  begin("scan", 5_000);
  const scanned = await scanFiles(found.files, { root, git: d.git, branch });
  fillFiles(report, scanned);
  end();

  // 5. Upload: the project's files to the project space, machine-local memory to Personal.
  let personalOk =
    !!machine?.space &&
    scanned.personal.items.length + scanned.personal.skipped.length > 0;
  // Once the person has brought machine-local memory in, init does it again
  // without asking: only what's new is proposed.
  const before =
    personalOk &&
    (await d.memax.v2.imports
      .list(machine!.space!.id, { limit: 1 })
      .then((p) => p.items.length > 0)
      .catch(() => false));
  if (before)
    report.done.push(`Machine-local memory goes to ${machine!.space!.slug}.`);
  if (personalOk && !o.yes && !before) {
    personalOk =
      d.interactive &&
      (await d.prompt.confirm(
        `  Bring ${scanned.personal.items.length} notes from this machine (${homeKinds(scanned)}) into ${machine!.space!.slug}? [Y/n] `,
        true,
      ));
  }
  if (machine?.note && scanned.personal.items.length > 0)
    d.out(chalk.gray(`  ${machine.note}`));
  begin("upload", null);
  const ups: Uploaded[] = [];
  if (project) ups.push(await upload(d, project.space, scanned.project, run));
  if (personalOk)
    ups.push(await upload(d, machine!.space!, scanned.personal, run));
  const proposals = perFile(ups, scanned);
  printScan(d, scanned, proposals);
  for (const u of ups) {
    for (const r of u.imports) {
      const c = r.import.counts;
      report.imports.push({
        space: u.space.slug,
        id: r.import.id,
        proposed: c.proposed,
        folded: c.folded,
        existing: c.existing,
        refused: c.refused,
        conflicts: 0,
      });
    }
  }
  const counts = report.imports.reduce(
    (a, i) => ({
      folded: a.folded + i.folded,
      existing: a.existing + i.existing,
      proposed: a.proposed + i.proposed,
    }),
    { folded: 0, existing: 0, proposed: 0 },
  );
  for (const l of renderHandled({
    ...counts,
    secrets: report.secrets,
    hidden: scanned.hidden,
    branchOnly: scanned.files.reduce((n, f) => n + f.branchOnly, 0),
    otherSecrets: scanned.files.reduce((n, f) => n + f.otherSecrets, 0),
  }))
    d.out(l);
  if (counts.existing > 0 && counts.proposed === 0)
    report.done.push(
      `Every statement is already in the record (${counts.existing}).`,
    );

  // 6. The judge: every proposal judged, and each import checked for disagreements.
  begin("judge", 20_000);
  const timeout = Math.max(1, Number(o.wait ?? 20)) * 1000;
  const judged = await Promise.all(
    ups
      .filter((u) => u.imports.length > 0)
      .map(async (u) => ({
        u,
        j: await waitForJudge(
          d,
          u.space,
          u.imports.map((r) => r.import.id),
          timeout,
        ),
      })),
  );
  // Disagreements an earlier run found and nobody settled yet.
  const opens: Open[] = [];
  for (const { u, j } of judged) {
    opens.push(...openConflicts(u.space, j.views));
    if (!j.ready)
      d.out(
        chalk.gray(
          `  The judge is still looking at some of ${u.space.slug}'s proposals; they show as being checked in Review.`,
        ),
      );
  }
  if (project)
    opens.push(
      ...(await earlierConflicts(
        d,
        project.space,
        judged.flatMap(({ j }) => j.views.map((v) => v.import.id)),
      )),
    );
  report.imports.forEach(
    (i) => (i.conflicts = opens.filter((op) => op.importId === i.id).length),
  );

  // 7. Settle: disagreements, then the ones that agree, then the first Brief.
  begin("settle", null);
  const files = scanned.files.filter((f) => !f.compiled && f.sent > 0).length;
  if (files > 0 || opens.length > 0) {
    d.out("");
    d.out(
      `  ${chalk.yellow("○")} ${files} ${files === 1 ? "file" : "files"}, ${opens.length} ${opens.length === 1 ? "conflict" : "conflicts"}, 1 Brief.`,
    );
  }
  const settled = await settleConflicts(d, o, opens);
  report.settled = settled.settled;
  const views = await Promise.all(
    judged.map(async ({ u, j }) => ({
      u,
      views: await Promise.all(
        j.views.map((v) => d.memax.v2.imports.get(u.space.id, v.import.id)),
      ),
    })),
  );
  const kept = new Set<string>();
  for (const { u, views: vs } of views) {
    const cands = bulkCandidates(u.space, vs);
    if (cands.length === 0) continue;
    const sample = cands
      .slice(0, 5)
      .map((c) => `      ${chalk.italic(c.memory.statement)}`);
    d.out("");
    d.out(
      `  ${cands.length} ${cands.length === 1 ? "statement" : "statements"} in ${u.space.slug} ${cands.length === 1 ? "agrees and comes" : "agree and come"} from your own files:`,
    );
    for (const s of sample) d.out(s);
    if (cands.length > sample.length)
      d.out(chalk.gray(`      and ${cands.length - sample.length} more`));
    const yes =
      o.yes ||
      (d.interactive &&
        (await d.prompt.confirm(
          `  Keep ${cands.length === 1 ? "it" : `the ${cands.length}`}? [Y/n] `,
          true,
        )));
    if (!yes) continue;
    for (const id of await keepInBulk(d, u.space, cands, run)) kept.add(id);
  }
  report.kept = kept.size;
  report.waiting = views.reduce((n, { views: vs }) => n + waiting(vs, kept), 0);
  if (report.waiting > 0 && project) {
    report.review = d.appUrl
      ? `${d.appUrl}/${project.space.slug}/review`
      : null;
    d.out(
      `  ${chalk.yellow("○")} ${report.waiting} ${report.waiting === 1 ? "proposal waits" : "proposals wait"} for you. Nothing is kept until you say so.`,
    );
    if (report.review) d.out(chalk.gray(`    Review them: ${report.review}`));
  }
  if (!project || !root) return 0;
  const brief = await firstBrief(
    d,
    project.space,
    project.space.repository ?? null,
  );
  if (brief) report.brief = brief.ref;
  end();

  // 8. Compile: targets, the link, the daemon or one delivery, git status.
  begin("compile", 10_000);
  let hasBrief = !!brief;
  if (!hasBrief)
    hasBrief = await d.memax.v2.briefs.get(project.space.id).then(
      () => true,
      () => false,
    );
  if (!hasBrief) {
    d.out("");
    d.out(
      chalk.gray(
        `  Nothing is kept in ${project.space.slug} yet, so there is nothing to compile. Keep what's true in Review, then run memax compile.`,
      ),
    );
    return 0;
  }
  await ensureTargets(d, o, project.space, root);
  if (!linkHere(d, project.space, root).changed)
    report.done.push(`This repository is linked to ${project.space.slug}.`);
  const daemon =
    o.daemon !== false &&
    (o.yes ||
      (d.interactive &&
        (await d.prompt.confirm(
          "  Start the Memax daemon, so every Keep reaches these files on its own? [Y/n] ",
          true,
        ))));
  if (daemon) await d.startDaemon();
  // Creating the targets, keeping and replacing a file each asked for a
  // compile already: wait for those, rather than compile everything again.
  let out = await d.compile(project.space.slug, 30, { pending: true });
  for (const t of (await d.memax.v2.targets.list(project.space.id)).items) {
    if (await offerOverwrite(d, t))
      out = await d.compile(project.space.slug, 30, { pending: true });
  }
  end();
  if (out) report.targets = out.targets;
  const status = gitStatus(
    d,
    root,
    (out?.targets ?? []).flatMap((t) => t.files).filter(Boolean),
  );
  for (const l of renderCompiled({
    space: project.space,
    targets: report.targets,
    status,
    kept: report.kept,
    settled: report.settled,
    writer: out?.writer ?? null,
    appUrl: d.appUrl,
  }))
    d.out(l);
  if (report.done.length > 0) {
    d.out("");
    d.out(chalk.white("  Already done"));
    for (const l of report.done) d.out(chalk.gray(`    ✓ ${l}`));
  }
  d.out("");
  return out?.timedOut ? 1 : 0;
}

/** The spaces, signing in first when the CLI has no working credentials. */
async function signedIn(d: InitDeps): Promise<V2.Space[]> {
  const list = () => d.memax.v2.spaces.list().then((r) => r.items);
  if (d.hasCredentials()) {
    try {
      return await list();
    } catch (err) {
      if (!(err instanceof MemaxError) || err.status !== 401) throw err;
    }
  }
  if (!(await d.signIn()))
    throw new InitError(
      "Not signed in. Run memax init again to sign in, or memax login first.",
    );
  return list();
}

async function earlierConflicts(
  d: InitDeps,
  space: V2.Space,
  these: string[],
): Promise<Open[]> {
  const page = await d.memax.v2.imports.list(space.id, { limit: 10 });
  const out: Open[] = [];
  for (const imp of page.items) {
    if (these.includes(imp.id) || imp.counts.conflicts === 0) continue;
    out.push(
      ...openConflicts(space, [await d.memax.v2.imports.get(space.id, imp.id)]),
    );
  }
  return out;
}

function countItems(s: ScanResult): number {
  return s.project.items.length + s.personal.items.length;
}

function homeKinds(s: ScanResult): string {
  const labels = [
    ...new Set(
      s.files
        .filter((f) => f.file.location === "home" && f.sent > 0)
        .map((f) => agentName(f.file.agent)),
    ),
  ];
  return labels.join(", ");
}

function agentName(kind: V2.AgentKind): string {
  return (
    (
      {
        "claude-code": "Claude Code",
        codex: "Codex",
        "gemini-cli": "Gemini CLI",
      } as Record<string, string>
    )[kind] ?? kind
  );
}

/** Statements each file sent that are now proposals (folded ones included). */
function perFile(ups: Uploaded[], scanned: ScanResult): Map<string, number> {
  const byKey = new Map<string, string>();
  scanned.files.forEach((f, fi) => byKey.set(`f${fi}`, f.file.label));
  const out = new Map<string, number>();
  for (const u of ups) {
    for (const r of u.imports) {
      for (const it of r.items) {
        if (it.outcome !== "proposed" && it.outcome !== "folded") continue;
        const label = byKey.get(it.key.split("l")[0]);
        if (label) out.set(label, (out.get(label) ?? 0) + 1);
      }
    }
  }
  return out;
}

function printScan(
  d: InitDeps,
  scanned: ScanResult,
  proposals: Map<string, number>,
): void {
  const read = scanned.files.filter(
    (f) => f.statements > 0 || f.compiled || f.unreadable,
  );
  if (read.length === 0) {
    d.out("");
    d.out(
      chalk.gray(
        "  No agent files found here or on this machine. Start the Brief in the app, or remember what's true with memax remember.",
      ),
    );
    return;
  }
  for (const l of renderFiles(read, proposals)) d.out(l);
}

function fillFiles(report: InitReport, scanned: ScanResult): void {
  report.files = scanned.files.map((f) => ({
    path: f.file.label,
    kind: f.file.kind,
    location: f.file.location,
    statements: f.statements,
    sent: f.sent,
    skipped: f.skipped,
    hidden: totalHidden(f.hidden),
    compiled: f.compiled,
  }));
  report.secrets = [
    ...scanned.project.skipped,
    ...scanned.personal.skipped,
  ].filter((s) => s.reason === "secret").length;
  report.hidden = totalHidden(scanned.hidden);
}
