// memax switch: moves a space from V1 to the V2 record, or back with
// --back (plan 25 §10). The flow is lib/switch/run.ts; this file wires it
// to the machine and prints what moves and what it did. --dry-run changes
// nothing.
import chalk from "chalk";
import type { Command } from "commander";
import type { V2 } from "memax-sdk";
import { getClient } from "../lib/client.js";
import { daemonPaths } from "../lib/daemon/paths.js";
import { ask } from "../lib/prompt.js";
import {
  runSwitch,
  switchKind,
  SWITCH_KINDS,
  type SwitchOptions,
  type SwitchReport,
} from "../lib/switch/run.js";
import { appBaseURL } from "./mcp-v2.js";
import { apiFailureMessage, MARK, pad } from "./v2-output.js";

type Line = [mark: string, label: string, text: string];

const done = chalk.green(MARK.done);
const same = chalk.gray("=");
const item = chalk.gray("·");
const wait = chalk.yellow(MARK.waiting);
const working = chalk.gray(MARK.working);
const fail = chalk.red("✗");

const TARGET_LABELS: Partial<Record<V2.TargetKind, string>> = {
  agents_md: "AGENTS.md",
  claude_md: "CLAUDE.md",
  gemini_md: "GEMINI.md",
  cursor_mdc: "Cursor rules",
  copilot: "Copilot instructions",
  windsurf: "Windsurf rules",
  claude_rules: "Claude rules",
  chatgpt: "ChatGPT",
};

function n(count: number, one: string, many: string): string {
  return `${count} ${count === 1 ? one : many}`;
}

function kindWord(kind: V2.SpaceKind): string {
  return kind === "personal" ? "personal space" : `${kind} space`;
}

function level(a: V2.Autonomy): string {
  return a[0].toUpperCase() + a.slice(1);
}

function targetList(kinds: V2.TargetKind[]): string {
  return kinds.map((k) => TARGET_LABELS[k] ?? k).join(", ");
}

/** What a switch moves, read fresh from V1 (the preview). */
export function previewLines(st: V2.SpaceSwitch, as?: string): Line[] {
  const pv = st.preview;
  const nt = pv.notes;
  const lines: Line[] = [];
  const kind = switchKind(st, as);
  const other = pv.kinds.filter((k) => k !== kind);
  lines.push([
    item,
    "Space",
    `switches as a ${kindWord(kind)}` +
      (other.length > 0 && !as ? ` (or --as ${other.join(" or ")})` : ""),
  ]);
  lines.push([
    item,
    "Notes",
    nt.total === 0
      ? "no V1 memories to move"
      : `${n(nt.total, "V1 memory becomes a note", "V1 memories become notes")} (N-): searchable, never compiled, nothing lost`,
  ]);
  if (nt.candidates > 0)
    lines.push([
      item,
      "Review",
      `${n(nt.candidates, "is yours, one statement", "are yours, one statement each")}: keep them in one go`,
    ]);
  if (nt.fold > 0)
    lines.push([
      item,
      "Dream",
      `${nt.fold} for Dream to fold into proposals (what agents wrote, and longer notes)`,
    ]);
  if (nt.kept > 0) {
    const why = [
      nt.archived > 0 ? `${nt.archived} archived` : "",
      nt.secret > 0
        ? `${n(nt.secret, "holds a credential", "hold a credential")}`
        : "",
    ].filter(Boolean);
    lines.push([
      item,
      "Notes only",
      `${nt.kept} stay notes, never proposed${why.length ? ` (${why.join(", ")})` : ""}`,
    ]);
  }
  if (pv.personas > 0)
    lines.push([
      item,
      "Personas",
      `${n(pv.personas, "persona becomes a note", "personas become notes")}, for Dream to propose from`,
    ]);
  if (pv.members.length > 1) {
    const roles = new Map<string, number>();
    for (const m of pv.members) {
      const word =
        m.role === "member" && m.can_forget ? "member who can forget" : m.role;
      roles.set(word, (roles.get(word) ?? 0) + 1);
    }
    lines.push([
      item,
      "People",
      [...roles].map(([r, c]) => `${c} ${r}${c === 1 ? "" : "s"}`).join(", "),
    ]);
  }
  const joining = pv.agents.filter((a) => !a.connected);
  if (joining.length > 0)
    lines.push([
      item,
      "Agents",
      `${joining.map((a) => `${a.name} at ${level(a.autonomy)}`).join(", ")}; each is told on its next MCP response`,
    ]);
  if (pv.targets.length > 0)
    lines.push([
      item,
      "Compiles",
      `${targetList(pv.targets)}; two-way sync of ${n(pv.configs.length, "agent file", "agent files")} stops`,
    ]);
  if (pv.gates > 0)
    lines.push([
      item,
      "Decisions",
      `${n(pv.gates, "waits", "wait")} on V1's board; they move once their agent is connected`,
    ]);
  if (pv.dream_runs > 0)
    lines.push([
      item,
      "History",
      `${n(pv.dream_runs, "V1 Dream run", "V1 Dream runs")}, kept read-only`,
    ]);
  if (pv.plan) lines.push([item, "Plan", `${pv.plan}, kept`]);
  return lines;
}

/** What the switch did (or why it didn't). */
export function outcomeLines(r: SwitchReport, app: string): Line[] {
  const st = r.status;
  const slug = r.space.slug;
  const p = st.progress;
  switch (r.outcome) {
    case "preview":
      return [];
    case "already":
      return [[same, "Space", `${slug} is on V2 already`]];
    case "on_v1":
      return [[same, "Space", `${slug} is on V1 already`]];
    case "switched_back":
      return [
        [
          done,
          "Space",
          `${slug} is back on V1: every surface serves it as before`,
        ],
        [
          item,
          "Record",
          "its V2 record stays, receipted, for when you switch again",
        ],
      ];
    case "declined":
      return [[same, "Space", "nothing changed"]];
    case "needs_yes":
      return [
        [
          wait,
          "Space",
          `nothing changed: add --yes to switch ${slug} without asking`,
        ],
      ];
    case "running":
      return [
        [
          working,
          "Space",
          `still switching (${st.step}); memax switch --space ${slug} says where it stands`,
        ],
      ];
    case "failed":
      return [
        [
          fail,
          "Space",
          `stopped at ${st.step}${st.error ? ` (${st.error})` : ""}; run memax switch --space ${slug} again to resume it`,
        ],
      ];
  }
  const lines: Line[] = [
    [done, "Space", `${slug} is on V2`],
    [
      done,
      "Notes",
      `${n(p.notes + p.personas + p.configs, "note", "notes")} numbered`,
    ],
  ];
  if (p.proposed > 0) {
    const review = st.import_id
      ? `${app}/${slug}/review?filter=import&import=${st.import_id}`
      : `${app}/${slug}/review`;
    lines.push([
      wait,
      "Review",
      `${n(p.proposed, "of yours waits", "of yours wait")} to be kept in one go: ${review}`,
    ]);
  }
  if (st.preview.notes.fold > 0)
    lines.push([
      item,
      "Dream",
      `folds ${n(st.preview.notes.fold, "note", "notes")} into proposals in its next edition`,
    ]);
  if (p.connected > 0)
    lines.push([
      done,
      "Agents",
      `${n(p.connected, "agent", "agents")} connected; ${p.notified} told on their next MCP response`,
    ]);
  if (p.targets.length > 0)
    lines.push([
      done,
      "Compiles",
      `${targetList(p.targets)}; V1 two-way sync is off for this space`,
    ]);
  if (p.gates_left > 0)
    lines.push([
      wait,
      "Decisions",
      `${p.gates_left} left on V1's board until their agent is connected`,
    ]);
  return lines;
}

function title(r: SwitchReport, o: SwitchOptions): string {
  const to = o.back ? "back to V1" : "to V2";
  const dry = o.dryRun ? chalk.gray("  (dry run)") : "";
  return chalk.bold(`  Memax · switch ${r.space.slug} ${to}`) + dry;
}

function render(lines: Line[]): string[] {
  return lines.map(([mark, label, text]) =>
    `  ${mark} ${pad(label, 11)}${text}`.trimEnd(),
  );
}

export function renderSwitch(
  r: SwitchReport,
  o: SwitchOptions,
  app: string,
): string[] {
  const out = ["", title(r, o), ""];
  const showPreview =
    !o.back &&
    (r.outcome === "preview" ||
      r.outcome === "needs_yes" ||
      r.outcome === "declined");
  if (showPreview) out.push(...render(previewLines(r.status, o.as)), "");
  if (r.outcome === "preview") {
    out.push(
      chalk.gray(
        o.back
          ? `  Nothing changed. memax switch --back --space ${r.space.slug} puts it back on V1.`
          : `  Nothing changed. memax switch --space ${r.space.slug} switches it; --back undoes it.`,
      ),
      "",
    );
    return out;
  }
  out.push(...render(outcomeLines(r, app)), "");
  return out;
}

/** Whether it failed (exit 1); needs_yes is a usage error (exit 2). */
export function switchExitCode(r: SwitchReport): number {
  if (r.outcome === "failed") return 1;
  if (r.outcome === "needs_yes") return 2;
  return 0;
}

export function registerSwitchCommand(program: Command): void {
  program
    .command("switch")
    .description(
      "Switch a space to V2: its V1 memories become notes, the ones you wrote wait in Review to be kept in one go, and Memax compiles its agent files. Nothing in V1 changes; --back undoes it",
    )
    .option("--space <slug>", "The space (default: .memax.yml, then the link)")
    .option("--dry-run", "Show what moves; change nothing")
    .option("--back", "Switch the space back to V1")
    .option(
      "--as <kind>",
      `A V1 team hub switches as: ${SWITCH_KINDS.join(" or ")}`,
    )
    .option(
      "--repository <owner/name>",
      "The repository the space compiles for",
    )
    .option("-y, --yes", "Switch without asking")
    .option(
      "--wait <seconds>",
      "How long to wait for a switch that runs in the background",
      "120",
    )
    .option("--format <format>", "Output format: text, json", "text")
    .action(async (opts: SwitchOptions) => {
      if (opts.as && !(SWITCH_KINDS as readonly string[]).includes(opts.as)) {
        console.error(
          chalk.red(
            `  --as takes ${SWITCH_KINDS.join(" or ")}, not ${opts.as}.`,
          ),
        );
        process.exitCode = 2;
        return;
      }
      const json = opts.format === "json";
      const interactive =
        !!process.stdin.isTTY && !!process.stdout.isTTY && !json;
      try {
        const r = await runSwitch(opts, {
          memax: getClient(),
          paths: daemonPaths(),
          cwd: process.cwd(),
          confirm: interactive
            ? async (st) => {
                const head: SwitchReport = {
                  outcome: "preview",
                  space: st.space,
                  status: st,
                };
                console.log(["", title(head, opts), ""].join("\n"));
                if (!opts.back)
                  for (const l of render(previewLines(st, opts.as)))
                    console.log(l);
                const q = opts.back
                  ? `\n  Switch ${st.space.slug} back to V1? [y/N] `
                  : `\n  Switch ${st.space.slug} to V2? [y/N] `;
                return /^y(es)?$/i.test((await ask(q)).trim());
              }
            : null,
        });
        if (json) console.log(JSON.stringify(r, null, 2));
        else {
          const lines = renderSwitch(r, opts, appBaseURL());
          // The preview was shown with the question; print what happened.
          const asked =
            interactive &&
            !opts.yes &&
            r.outcome !== "preview" &&
            r.outcome !== "already" &&
            r.outcome !== "on_v1";
          for (const l of asked
            ? ["", ...render(outcomeLines(r, appBaseURL())), ""]
            : lines)
            console.log(l);
        }
        process.exitCode = switchExitCode(r);
      } catch (err) {
        console.error(chalk.red(`  ${apiFailureMessage(err)}`));
        process.exitCode = 1;
      }
    });
}
