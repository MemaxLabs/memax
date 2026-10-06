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
import type {
  CompileSummary,
  DriftItemView,
  TargetSettingsView,
  TargetView,
} from "./targets";

/**
 * The demo's compile targets (screens/INDEX.md › Demo data, adapted to
 * D2/D3): memax-v2 compiles to the canonical AGENTS.md, the CLAUDE.md
 * shim, one scoped Cursor rule (drifted after a hand edit on Oct 4) and
 * the ChatGPT project instructions. The content is what the compiler
 * wrote (demo-compiled.ts). Data, not copy.
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

export const DEMO_TARGETS: Readonly<Record<string, readonly TargetView[]>> = {
  "memax-v2": [
    {
      id: id(1),
      slug: "agents-md",
      kind: "agents_md",
      label: "AGENTS.md",
      path: "AGENTS.md",
      reads: null,
      syncState: "in_sync",
      delivery: "local",
      settings: DEMO_SETTINGS,
      version: 4,
      openDrift: 0,
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
      reads: "AGENTS.md",
      syncState: "in_sync",
      delivery: "local",
      settings: DEMO_SETTINGS,
      version: 2,
      openDrift: 0,
      lastCompile: demoRun("C-0882", DEMO_CLAUDE_MD, ["CLAUDE.md"]),
    },
    {
      id: id(3),
      slug: "cursor-mdc",
      kind: "cursor_mdc",
      label: ".cursor/rules",
      path: ".cursor/rules",
      reads: "AGENTS.md",
      syncState: "drifted",
      delivery: "local",
      settings: DEMO_SETTINGS,
      version: 2,
      openDrift: 1,
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
      reads: null,
      syncState: "in_sync",
      delivery: "copy",
      settings: DEMO_SETTINGS,
      version: 1,
      openDrift: 0,
      lastCompile: demoRun("C-0884", DEMO_CHATGPT["kept_and_open:mark"], []),
    },
  ],
  // ChatGPT keeps Personal's preferences (M-0433 goes there).
  personal: [
    {
      id: id(5),
      slug: "agents-md",
      kind: "agents_md",
      label: "AGENTS.md",
      path: "AGENTS.md",
      reads: null,
      syncState: "in_sync",
      delivery: "local",
      settings: DEMO_SETTINGS,
      version: 1,
      openDrift: 0,
      lastCompile: null,
    },
    {
      id: id(6),
      slug: "claude-md",
      kind: "claude_md",
      label: "CLAUDE.md",
      path: "CLAUDE.md",
      reads: "AGENTS.md",
      syncState: "in_sync",
      delivery: "local",
      settings: DEMO_SETTINGS,
      version: 1,
      openDrift: 0,
      lastCompile: null,
    },
  ],
};

/** DriftResolve.png: the hand edit to Cursor's rule, adapted to D2's scoped rule. */
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
      changes: DEMO_CURSOR_CHANGES.map((change) =>
        change.kind === "edit"
          ? {
              kind: "edit" as const,
              ref: change.ref,
              oldText: change.old_text,
              newText: change.new_text,
              oldLine: change.old_line,
              newLine: change.new_line,
            }
          : {
              kind: "new" as const,
              text: change.text,
              line: change.line,
              section: change.section,
            },
      ),
      hiddenCharacters: 0,
    },
  ],
};
