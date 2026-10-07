// What memax init prints, in the board's words and marks (FirstRun,
// Cleanup, CompileDone): ● kept or in sync, ○ waiting on you, ✓ done,
// ◐ a disagreement.
import chalk from "chalk";
import type { V2 } from "memax-sdk";
import { agentMark, pad } from "../../commands/v2-output.js";
import { describeHidden, totalHidden, type HiddenCounts } from "./hidden.js";
import type { ScannedFile } from "./scan.js";
import type { InitReport, StepTiming } from "./types.js";

function plural(n: number, one: string, many = `${one}s`): string {
  return `${n} ${n === 1 ? one : many}`;
}

export function renderAgents(rows: InitReport["agents"]): string[] {
  const lines = ["", chalk.white("  Agents on this machine")];
  if (rows.filter((r) => r.found).length === 0) {
    lines.push(
      chalk.gray("    None found. Install one, then run memax init again."),
    );
  }
  for (const r of rows) {
    const mark = agentMark(r.kind, r.name);
    if (r.kind === "chatgpt") {
      lines.push(
        `  ${chalk.yellow("○")} ${pad(mark, 4)}${pad(r.name, 13)}${chalk.gray("connector · finish in your browser")}`,
      );
      continue;
    }
    const note =
      r.mcp === "written"
        ? "connects on its next start"
        : r.mcp === "present"
          ? "set up"
          : r.mcp === "not written"
            ? "not set up"
            : r.mcp;
    lines.push(
      `  ${chalk.green("✓")} ${pad(mark, 4)}${pad(r.name, 13)}${pad(r.where, 22)}${pad(r.autonomy ?? "", 9)}${chalk.gray(note)}`,
    );
  }
  return lines;
}

/** One line per file (folders of rules or notes grouped), and what came of it. */
export function renderFiles(
  files: ScannedFile[],
  proposals: Map<string, number>,
): string[] {
  const lines = ["", chalk.white("  Reading what they already know")];
  const groups = new Map<string, ScannedFile[]>();
  for (const f of files) {
    const label = groupLabel(f);
    groups.set(label, [...(groups.get(label) ?? []), f]);
  }
  const width = Math.max(24, ...[...groups.keys()].map((k) => k.length + 2));
  for (const [label, fs] of groups) {
    if (fs.every((f) => f.compiled)) {
      lines.push(
        `    ${pad(label, width)}${chalk.gray("compiled by Memax: left as it is")}`,
      );
      continue;
    }
    if (fs.every((f) => f.unreadable)) {
      lines.push(
        `    ${pad(label, width)}${chalk.gray("not text Memax can read: left alone")}`,
      );
      continue;
    }
    const statements = fs.reduce((n, f) => n + f.statements, 0);
    const sent = fs.reduce(
      (n, f) => n + (proposals.get(f.file.label) ?? f.sent),
      0,
    );
    const what =
      fs.length > 1
        ? plural(fs.length, "file")
        : plural(statements, "statement");
    lines.push(
      `    ${pad(label, width)}${pad(what, 15)}→ ${String(sent).padStart(3)}`,
    );
  }
  return lines;
}

function groupLabel(f: ScannedFile): string {
  const l = f.file.label;
  if (f.file.kind === "cursor_rule") return ".cursor/rules/*.mdc";
  if (f.file.kind === "claude_memory") return "~/.claude/projects/…/memory";
  if (f.file.kind === "codex_memory") return "~/.codex/memories";
  if (f.file.kind === "copilot_scoped") return ".github/instructions";
  if (f.file.kind === "claude_rule") return ".claude/rules";
  return l;
}

/** "7 duplicates folded · 2 secrets skipped · 3 hidden characters removed". */
export function renderHandled(o: {
  folded: number;
  existing: number;
  secrets: number;
  hidden: HiddenCounts;
  branchOnly: number;
  otherSecrets: number;
}): string[] {
  const parts: string[] = [];
  if (o.folded > 0) parts.push(`${plural(o.folded, "duplicate")} folded`);
  if (o.existing > 0) parts.push(`${o.existing} already in the record`);
  if (o.secrets > 0) parts.push(`${plural(o.secrets, "secret")} skipped`);
  const lines =
    parts.length > 0 ? [chalk.gray(`    ${parts.join(" · ")}`)] : [];
  if (totalHidden(o.hidden) > 0) {
    lines.push(
      chalk.yellow(
        `    Removed ${describeHidden(o.hidden)}. Those lines wait for you in Review.`,
      ),
    );
  }
  if (o.branchOnly > 0) {
    lines.push(
      chalk.yellow(
        `    ${plural(o.branchOnly, "statement")} only on this branch, not the default one: they wait in Review as outside content.`,
      ),
    );
  }
  if (o.otherSecrets > 0) {
    lines.push(
      chalk.yellow(
        `    ${plural(o.otherSecrets, "line")} in code blocks look like credentials. Memax didn't read them; check the files.`,
      ),
    );
  }
  return lines;
}

export function renderTimings(timings: StepTiming[]): string[] {
  const lines = ["", chalk.white("  Timing")];
  for (const t of timings) {
    const budget =
      t.budgetMs === null
        ? ""
        : t.ms <= t.budgetMs
          ? chalk.green(`under ${fmt(t.budgetMs)}`)
          : chalk.yellow(`over ${fmt(t.budgetMs)}`);
    lines.push(`    ${pad(t.step, 12)}${pad(fmt(t.ms), 9)}${budget}`);
  }
  const total = timings.reduce((n, t) => n + t.ms, 0);
  lines.push(`    ${pad("total", 12)}${fmt(total)}`);
  return lines;
}

function fmt(ms: number): string {
  return ms < 1000 ? `${Math.round(ms)} ms` : `${(ms / 1000).toFixed(1)} s`;
}

/** CompileDone: what was written where, git status, and the three next steps. */
export function renderCompiled(o: {
  space: V2.Space;
  targets: Array<{ label: string; sync_state: V2.SyncState }>;
  status: string[];
  kept: number;
  settled: number;
  writer: "daemon" | "compile" | null;
  appUrl: string;
}): string[] {
  const live = o.targets.filter((t) => t.sync_state !== "off");
  const lines = [""];
  lines.push(
    `  ${chalk.green("●")} ${chalk.bold(o.space.slug)} is compiled into ${plural(live.length, "file")}.`,
  );
  const did: string[] = [];
  if (o.kept > 0) did.push(`You kept ${plural(o.kept, "memory", "memories")}`);
  if (o.settled > 0) did.push(`settled ${plural(o.settled, "disagreement")}`);
  if (did.length > 0)
    lines.push(
      chalk.gray(
        `    ${did.join(" and ")}. From the next session on, every agent reads the same context.`,
      ),
    );
  lines.push("");
  for (const t of live) {
    const mark =
      t.sync_state === "in_sync"
        ? chalk.green("●")
        : t.sync_state === "drifted" || t.sync_state === "held"
          ? chalk.yellow("○")
          : chalk.gray("◌");
    const word = {
      in_sync: "in sync",
      drifted: "drifted: written by hand",
      held: "held",
      compiling: "compiling",
      pending_delivery: "not written yet",
      off: "off",
    }[t.sync_state];
    lines.push(`  ${mark} ${pad(t.label, 28)}${chalk.gray(word)}`);
  }
  if (o.status.length > 0) {
    lines.push("", chalk.gray("  › git status --short"));
    for (const s of o.status) lines.push(`    ${s}`);
    lines.push(
      chalk.gray(
        "  Commit these to share them with your team and your cloud agents.",
      ),
    );
  }
  if (o.writer === "compile")
    lines.push(
      chalk.gray(
        "  Written by memax init. Start the daemon so each Keep reaches them on its own: memax daemon start",
      ),
    );
  const app = (p: string) =>
    o.appUrl
      ? `${o.appUrl}/${o.space.slug}/${p}`
      : `memax.app/${o.space.slug}/${p}`;
  lines.push("", chalk.white("  Next"));
  lines.push(`    Open Today            ${chalk.gray(app("today"))}`);
  lines.push(`    Connect a cloud agent ${chalk.gray(app("agents"))}`);
  lines.push(
    `    Bring your team       ${chalk.gray("make it a team space, so decisions reach everyone's agents")}`,
  );
  return lines;
}
