import {
  DEMO_AGENTS_MD,
  DEMO_CHATGPT,
  DEMO_CLAUDE_MD,
  DEMO_CURSOR_CHANGES,
  DEMO_CURSOR_DELIVERED,
  DEMO_CURSOR_FILE,
  DEMO_CURSOR_OBSERVED,
  type DemoCompiled,
} from "./demo-compiled";
import {
  DEMO_CODEX_CHANGES,
  DEMO_CODEX_DELIVERED,
  DEMO_CODEX_OBSERVED,
  DEMO_HOME_CANONICAL,
  DEMO_HOME_CLAUDE,
  DEMO_HOME_CODEX,
} from "./demo-home-compiled";
import { DEMO_MACHINE_IDS } from "./demo-machines-data";
import type {
  CompileSummary,
  DriftChangeView,
  DriftItemView,
  TargetKind,
  TargetMachineRef,
  TargetSettingsView,
  TargetView,
} from "./targets";

/**
 * The demo's compile targets (screens/INDEX.md › Demo data, adapted to
 * D2/D3): memax-v2 compiles to the canonical AGENTS.md, the CLAUDE.md
 * shim, one scoped Cursor rule (drifted after a hand edit on Oct 4) and
 * the ChatGPT project instructions; Personal compiles to its home files,
 * one set per linked machine. The content is what the compiler wrote
 * (demo-compiled.ts, demo-home-compiled.ts). Data, not copy.
 */

const at = (time: string, day = "2026-10-05") => `${day}T${time}:00-07:00`;

/** When C-0881…C-0884 ran: the boards' "Compiled at 14:31". */
export const DEMO_COMPILED_AT = at("14:31");

export const DEMO_SETTINGS: TargetSettingsView = {
  include: "kept_and_open",
  stale: "mark",
  sizeBudget: 25 * 1024,
};

export function demoRun(
  ref: string,
  compiled: DemoCompiled,
  files: string[],
  when = DEMO_COMPILED_AT,
): CompileSummary {
  return {
    ref,
    status: "delivered",
    at: when,
    bytes: compiled.bytes,
    lines: compiled.lines,
    refs: compiled.refs,
    cites: compiled.cites,
    dropped: compiled.dropped,
    files,
  };
}

const id = (n: number) =>
  `0192a7c0-0000-7000-8000-0000000000${(0xb0 + n).toString(16)}`;

/** A repository's file: no machine of its own. */
const REPO = {
  root: "repo",
  machine: null,
  readBy: [],
} as const satisfies Pick<TargetView, "root" | "machine" | "readBy">;

/** When Personal last compiled: just after the morning's review. */
export const DEMO_PERSONAL_COMPILED_AT = at("09:42");

/** A home file's budget: it loads in every session of every agent (6 KiB). */
export const DEMO_HOME_SETTINGS: TargetSettingsView = {
  include: "kept_and_open",
  stale: "mark",
  sizeBudget: 6 * 1024,
};

const LAPTOP: TargetMachineRef = {
  id: DEMO_MACHINE_IDS.laptop,
  label: "machine-1",
  name: "ziyang-mbp",
};
const DESKTOP: TargetMachineRef = {
  id: DEMO_MACHINE_IDS.desktop,
  label: "machine-2",
  name: "home-linux",
};

function personalRun(
  ref: string,
  compiled: DemoCompiled,
  when = DEMO_PERSONAL_COMPILED_AT,
): CompileSummary {
  return demoRun(ref, compiled, [], when);
}

/** One machine's copy of a home file, in sync unless `over` says otherwise. */
function home(
  n: number,
  machine: TargetMachineRef,
  kind: TargetKind,
  path: string,
  over: Partial<TargetView>,
): TargetView {
  const target: TargetView = {
    id: id(n),
    slug: `${kind.replace(/_/g, "-")}-${machine.label}`,
    kind,
    label: `~/${path}`,
    path,
    root: "home",
    machine,
    readBy: [],
    reads: null,
    syncState: "in_sync",
    delivery: "local",
    settings: DEMO_HOME_SETTINGS,
    version: 1,
    openDrift: 0,
    holding: [],
    lastCompile: null,
    ...over,
  };
  // The run wrote this one file.
  return target.lastCompile
    ? {
        ...target,
        lastCompile: { ...target.lastCompile, files: [path] },
      }
    : target;
}

export const DEMO_TARGETS: Readonly<Record<string, readonly TargetView[]>> = {
  "memax-v2": [
    {
      id: id(1),
      slug: "agents-md",
      kind: "agents_md",
      label: "AGENTS.md",
      path: "AGENTS.md",
      ...REPO,
      reads: null,
      syncState: "in_sync",
      delivery: "local",
      settings: DEMO_SETTINGS,
      version: 4,
      openDrift: 0,
      holding: [],
      lastCompile: demoRun("C-0881", DEMO_AGENTS_MD["kept_and_open:mark"], [
        "AGENTS.md",
      ]),
    },
    {
      id: id(2),
      slug: "claude-md",
      kind: "claude_md",
      label: "CLAUDE.md",
      path: "CLAUDE.md",
      ...REPO,
      reads: "AGENTS.md",
      syncState: "in_sync",
      delivery: "local",
      settings: DEMO_SETTINGS,
      version: 2,
      openDrift: 0,
      holding: [],
      lastCompile: demoRun("C-0882", DEMO_CLAUDE_MD, ["CLAUDE.md"]),
    },
    {
      id: id(3),
      slug: "cursor-mdc",
      kind: "cursor_mdc",
      label: ".cursor/rules",
      path: ".cursor/rules",
      ...REPO,
      reads: "AGENTS.md",
      syncState: "drifted",
      delivery: "local",
      settings: DEMO_SETTINGS,
      version: 2,
      openDrift: 1,
      holding: [],
      lastCompile: {
        ...demoRun("C-0883", DEMO_CURSOR_FILE, [DEMO_CURSOR_FILE.path]),
        // It compiled, but Memax won't write over the hand edit.
        status: "compiled",
      },
    },
    {
      id: id(4),
      slug: "chatgpt",
      kind: "chatgpt",
      label: "ChatGPT project",
      path: null,
      ...REPO,
      reads: null,
      syncState: "in_sync",
      delivery: "copy",
      settings: DEMO_SETTINGS,
      version: 1,
      openDrift: 0,
      holding: [],
      lastCompile: demoRun("C-0884", DEMO_CHATGPT["kept_and_open:mark"], []),
    },
  ],
  // Personal is written into each agent's own files on two machines
  // (demo-machines-data.ts): on ziyang-mbp, Codex's block has a hand edit
  // from this morning; home-linux has been offline since Friday, so the
  // compile after this morning's review hasn't reached it, and its
  // canonical copy was stopped there.
  personal: [
    home(5, LAPTOP, "claude_user", ".claude/rules/memax-personal.md", {
      readBy: ["claude-code"],
      lastCompile: personalRun("C-0875", DEMO_HOME_CLAUDE),
    }),
    home(6, LAPTOP, "codex_user", ".codex/AGENTS.md", {
      readBy: ["codex"],
      syncState: "drifted",
      openDrift: 1,
      lastCompile: personalRun("C-0876", DEMO_HOME_CODEX),
    }),
    home(7, LAPTOP, "agents_md", ".memax/personal/AGENTS.md", {
      lastCompile: personalRun("C-0877", DEMO_HOME_CANONICAL),
    }),
    home(8, DESKTOP, "claude_user", ".claude/rules/memax-personal.md", {
      readBy: ["claude-code"],
      syncState: "pending_delivery",
      // Compiled, and waiting for home-linux to come online.
      lastCompile: {
        ...personalRun("C-0878", DEMO_HOME_CLAUDE),
        status: "compiled",
      },
    }),
    home(9, DESKTOP, "codex_user", ".codex/AGENTS.md", {
      readBy: ["codex"],
      syncState: "pending_delivery",
      lastCompile: {
        ...personalRun("C-0879", DEMO_HOME_CODEX),
        status: "compiled",
      },
    }),
    home(10, DESKTOP, "agents_md", ".memax/personal/AGENTS.md", {
      syncState: "off",
      version: 2,
      lastCompile: personalRun(
        "C-0861",
        DEMO_HOME_CANONICAL,
        "2026-10-01T08:12:00-07:00",
      ),
    }),
  ],
};

type DemoChange =
  | {
      kind: "edit";
      ref: string;
      old_text: string;
      new_text: string;
      old_line: number;
      new_line: number;
    }
  | { kind: "new"; text: string; line: number; section: string | null };

/** A change as the spec writes it, as the screens read it. */
function changeOf(change: DemoChange): DriftChangeView {
  return change.kind === "edit"
    ? {
        kind: "edit",
        ref: change.ref,
        oldText: change.old_text,
        newText: change.new_text,
        oldLine: change.old_line,
        newLine: change.new_line,
      }
    : {
        kind: "new",
        text: change.text,
        line: change.line,
        section: change.section,
      };
}

/**
 * DriftResolve.png: the hand edit to Cursor's rule, adapted to D2's
 * scoped rule; and the one to Codex's block on ziyang-mbp, of which the
 * server holds the block alone.
 */
export const DEMO_DRIFT: Readonly<Record<string, DriftItemView[]>> = {
  [id(3)]: [
    {
      observationId: "0192a7c0-0000-7000-8000-0000000000c1",
      path: DEMO_CURSOR_FILE.path,
      observedAt: at("16:40", "2026-10-04"),
      commit: "a41e9c2",
      compiled: DEMO_CURSOR_DELIVERED,
      compiledAt: at("09:10", "2026-10-04"),
      observed: DEMO_CURSOR_OBSERVED,
      changes: DEMO_CURSOR_CHANGES.map(changeOf),
      hiddenCharacters: 0,
    },
  ],
  [id(6)]: [
    {
      observationId: "0192a7c0-0000-7000-8000-0000000000c2",
      path: ".codex/AGENTS.md",
      observedAt: at("11:20"),
      commit: null,
      compiled: DEMO_CODEX_DELIVERED,
      compiledAt: DEMO_PERSONAL_COMPILED_AT,
      observed: DEMO_CODEX_OBSERVED,
      changes: DEMO_CODEX_CHANGES.map(changeOf),
      hiddenCharacters: 0,
    },
  ],
};
