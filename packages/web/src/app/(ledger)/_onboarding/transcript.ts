/**
 * The terminals the setup screens draw (FirstRun, CompileDone), built
 * from what the record knows. The web can't see the person's terminal,
 * so these are what `npx memax-cli init` printed (or prints) as far as
 * the import, the agents and the targets tell, in the CLI's own layout
 * (packages/cli/src/lib/init/render.ts). Pure: catalogue and data in,
 * lines out.
 */
import { AGENTS, type TerminalLine } from "@memaxlabs/ledger";
import { interpolate } from "@/i18n/interpolate";
import type { Translations } from "@/i18n/locales/en";
import type { Autonomy } from "@/lib/v2/data/agents";
import {
  fileOfRef,
  importFileOf,
  skippedSecrets,
  type ImportView,
} from "@/lib/v2/data/imports";
import type { TargetView } from "@/lib/v2/data/targets";
import { cliCommand } from "@/lib/v2/cli";

type FirstRunCopy = Translations["ledger"]["onboarding"]["firstRun"];

/** Where each agent keeps its things, as init prints it. */
export const AGENT_WHERE: Readonly<Record<string, string>> = {
  "claude-code": "~/.claude",
  codex: "~/.codex",
  cursor: ".cursor/",
  gemini: "~/.gemini",
  copilot: "~/.copilot",
  opencode: "~/.config/opencode",
};

/** The level each agent starts at in a new space (init, plan §7.3 step 3). */
export const AGENT_START: Readonly<Record<string, Autonomy>> = {
  "claude-code": "propose",
  codex: "propose",
  cursor: "read",
  gemini: "read",
  copilot: "propose",
  opencode: "propose",
};

/** One agent on the machine, as the transcript lists it. */
export interface TranscriptAgent {
  /** A Ledger registry key. */
  agent: string;
  autonomy: Autonomy;
}

const pad = (text: string, width: number) =>
  text.length >= width ? `${text} ` : text.padEnd(width);

function agentName(key: string): string {
  return (AGENTS as Record<string, { name: string }>)[key]?.name ?? key;
}

function mono(key: string): string {
  return (AGENTS as Record<string, { mono: string }>)[key]?.mono ?? "··";
}

/** The proposals each file's statements became (folded repeats count to their proposal). */
export function proposalsByFile(view: ImportView): Map<string, number> {
  const out = new Map<string, number>();
  const paths = view.summary.files.map((f) => f.path);
  for (const m of view.memories) {
    if (m.outcome !== "proposed") continue;
    for (const ref of m.refs) {
      const file = importFileOf(fileOfRef(ref), paths) ?? fileOfRef(ref);
      out.set(file, (out.get(file) ?? 0) + 1);
    }
  }
  return out;
}

/**
 * FirstRun's terminal: `npx memax-cli init` waiting, or what it printed
 * once its import landed (agents, files, what was handled, the summary).
 */
export function firstRunLines(
  copy: FirstRunCopy,
  input: {
    viewer: string | null;
    space: string | null;
    agents: TranscriptAgent[];
    /** ChatGPT isn't connected yet: init says to finish it in the browser. */
    chatgptPending: boolean;
    view: ImportView | null;
    /** The web app's host, for the Review link ("memax.app"). */
    host: string;
  },
): TerminalLine[] {
  const t = copy.t;
  const lines: TerminalLine[] = [{ kind: "cmd", text: cliCommand("init") }];
  const { view } = input;
  if (!view || !input.space) {
    lines.push({ kind: "dim", text: t.waiting });
    return lines;
  }
  if (input.viewer) {
    lines.push({
      kind: "dim",
      text: interpolate(t.signedIn, { name: input.viewer, space: input.space }),
    });
  }
  if (input.agents.length > 0 || input.chatgptPending) {
    lines.push({ kind: "blank" }, { kind: "dim", text: t.agents });
    for (const a of input.agents) {
      lines.push({
        kind: "ok",
        text: `${pad(mono(a.agent), 4)}${pad(agentName(a.agent), 14)}${pad(AGENT_WHERE[a.agent] ?? "", 15)}${a.autonomy}`,
      });
    }
    if (input.chatgptPending) {
      lines.push({
        kind: "proposed",
        text: `${pad(mono("chatgpt"), 4)}${pad(agentName("chatgpt"), 14)}${t.connector}`,
      });
    }
  }
  const proposals = proposalsByFile(view);
  const files = view.summary.files.filter((f) => f.statements > 0);
  if (files.length > 0) {
    lines.push({ kind: "blank" }, { kind: "dim", text: t.reading });
    const width = Math.max(...files.map((f) => f.path.length)) + 2;
    for (const f of files) {
      lines.push({
        kind: "out",
        text: `  ${interpolate(t.file, {
          path: pad(f.path, width),
          statements: String(f.statements).padStart(3),
          proposals: String(proposals.get(f.path) ?? 0).padStart(3),
        })}`,
      });
    }
    lines.push({
      kind: "dim",
      text: `  ${interpolate(t.handled, {
        folded: view.summary.counts.folded,
        secrets: skippedSecrets(view.summary).length,
      })}`,
    });
  }
  lines.push({ kind: "blank" });
  if (!view.progress.ready) {
    lines.push({
      kind: "dim",
      text: interpolate(t.checking, { n: view.progress.proposals }),
    });
    return lines;
  }
  const conflicts = view.conflicts.filter((c) => c.state === "open").length;
  lines.push({
    kind: "warn",
    text: interpolate(conflicts === 1 ? t.summaryOne : t.summary, {
      files: files.length,
      conflicts,
    }),
  });
  lines.push({
    kind: "proposed",
    text: interpolate(t.proposed, { n: view.summary.counts.proposed }),
  });
  lines.push({
    kind: "out",
    text: `  ${interpolate(t.next, { host: input.host, space: input.space })}`,
  });
  return lines;
}

/**
 * CompileDone's `git status --short`, as far as Memax can tell without the
 * repository: a file it wrote that init read before (the person's
 * CLAUDE.md, an AGENTS.md) shows as modified, any other as new. Memax
 * never removes a person's own files, so nothing shows as deleted.
 */
export function gitStatusLines(
  targets: readonly TargetView[],
  readBefore: readonly string[],
): string[] {
  const before = new Set(readBefore);
  const out: string[] = [];
  for (const t of targets) {
    if (t.delivery !== "local" || t.syncState === "off") continue;
    const files = t.lastCompile?.files.length
      ? t.lastCompile.files
      : t.path && t.kind !== "cursor_mdc"
        ? [t.path]
        : [];
    for (const file of files) {
      if (out.some((l) => l.endsWith(` ${file}`))) continue;
      out.push(`${before.has(file) ? " M" : "??"} ${file}`);
    }
  }
  return out;
}
