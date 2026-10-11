/**
 * Rule 6 for every kind that manages one block in a file the person owns,
 * over 2,000 random files each (seeded, so failures reproduce):
 *
 * - every byte outside the markers stays as it was: line endings, a BOM,
 *   trailing spaces, a missing final newline, lines that look like Memax's;
 * - a file with no block gets one at the end, after at most a blank line;
 * - the server compiles the block alone (it never holds the person's lines),
 *   and splicing that block into the file on a machine, as the daemon does,
 *   gives the bytes compiling against the whole file would, with the same
 *   drift hash;
 * - compiling over its own output changes nothing;
 * - broken markers are refused, never guessed at.
 */
import { describe, expect, it } from "vitest";
import {
  compile,
  CompileInputError,
  extractManagedBlock,
  findManagedBlock,
  isDrifted,
  MANAGED_END,
  MANAGED_START,
  upsertManagedBlock,
  type SpaceKind,
  type TargetSettings,
} from "../src/index.js";
import { input, memory, rng } from "./helpers.js";

const FILES = 2_000;

const BLOCK_KINDS: {
  name: string;
  space: SpaceKind;
  target: (current: string) => TargetSettings;
}[] = [
  {
    name: "claude_md, a CLAUDE.md the person owns",
    space: "project",
    target: (current) => ({ kind: "claude_md", user_owned: true, current }),
  },
  {
    name: "gemini_md, a GEMINI.md the person owns",
    space: "project",
    target: (current) => ({ kind: "gemini_md", user_owned: true, current }),
  },
  {
    name: "claude_user in ~/.claude/CLAUDE.md",
    space: "personal",
    target: (current) => ({
      kind: "claude_user",
      path: ".claude/CLAUDE.md",
      current,
    }),
  },
  {
    name: "codex_user in ~/.codex/AGENTS.md",
    space: "personal",
    target: (current) => ({ kind: "codex_user", current }),
  },
  {
    name: "codex_user in ~/.codex/AGENTS.override.md",
    space: "personal",
    target: (current) => ({
      kind: "codex_user",
      path: ".codex/AGENTS.override.md",
      current,
    }),
  },
];

/** Random text with what matters here: markers, line endings, look-alikes. */
function* personFiles(seed: number): Generator<string> {
  const random = rng(seed);
  const pick = <T>(items: readonly T[]) =>
    items[Math.floor(random() * items.length)];
  const parts = [
    "# My notes",
    "- my own line [M-0001]",
    "- (Being verified) looks like ours [M-0002]",
    "",
    "  ",
    "\t",
    "@AGENTS.md",
    "<!-- Compiled by Memax from ana at 2026-10-05 14:31 UTC (C-0001). Edit it at https://memax.app/ana/brief; edits here come back as proposals. -->",
    "<!-- a comment of mine -->",
    "部署 café",
    "\u{202E}hidden",
    "trailing spaces   ",
    MANAGED_START,
    MANAGED_END,
    `  ${MANAGED_START}  `,
  ];
  const eols = ["\n", "\n", "\r\n", "\r", ""];
  for (let i = 0; i < FILES; i++) {
    const lines = Array.from({ length: Math.floor(random() * 10) }, () =>
      pick(parts),
    );
    // A third get a well-formed block somewhere: V1's, or an older V2 one.
    if (random() < 0.35) {
      const block = [
        MANAGED_START,
        pick(["V1's guidance", "- old [M-0001]"]),
        MANAGED_END,
      ];
      lines.splice(Math.floor(random() * (lines.length + 1)), 0, ...block);
    }
    let s = random() < 0.05 ? "\u{FEFF}" : "";
    for (const line of lines) s += line + pick(eols);
    yield s;
  }
}

/** A file's raw lines, each with its own terminator (managed-block.ts's split). */
function rawLines(content: string): { text: string; eol: string }[] {
  const out: { text: string; eol: string }[] = [];
  const re = /([^\r\n]*)(\r\n|\r|\n|$)/g;
  let m: RegExpExecArray | null;
  while ((m = re.exec(content)) !== null) {
    if (m[0] === "") break;
    out.push({ text: m[1], eol: m[2] });
  }
  return out;
}

/** The raw bytes before the start marker's line and after the end marker's. */
function around(content: string): { before: string; after: string } | null {
  const at = findManagedBlock(content);
  if (!at) return null;
  const lines = rawLines(content);
  const join = (ls: typeof lines) => ls.map((l) => l.text + l.eol).join("");
  return {
    before: join(lines.slice(0, at.start - 1)),
    after: join(lines.slice(at.end)),
  };
}

/** The lines inside a block, markers excluded: what the daemon splices in. */
function inner(content: string): string[] {
  const block = extractManagedBlock(content);
  if (block === null) throw new Error("no managed block");
  return block.slice(0, -1).split("\n").slice(1, -1);
}

describe.each(BLOCK_KINDS)("the person's own lines: $name", (spec) => {
  const compileWith = (current: string) => {
    const i = input(
      [
        memory("M-0001", "Answer in British English."),
        memory("M-0002", "Commit messages are imperative.", {
          flags: ["stale"],
        }),
        memory("M-0003", "Only for this agent.", {
          scope: { agents: ["claude-code", "codex", "gemini"] },
        }),
      ],
      [spec.target(current)],
    );
    i.space.kind = spec.space;
    return compile(i).files[0];
  };

  it(`stay byte for byte over ${FILES} random files, and the machine's splice matches`, () => {
    const server = compileWith("");
    expect(server.user_owned).toBe(true);
    const seen = { none: 0, present: 0, broken: 0 };

    for (const mine of personFiles(spec.name.length * 7919)) {
      let hadBlock: boolean;
      try {
        hadBlock = findManagedBlock(mine) !== null;
      } catch {
        seen.broken += 1;
        expect(() => compileWith(mine)).toThrow(CompileInputError);
        continue;
      }
      const there = compileWith(mine);

      if (hadBlock) {
        seen.present += 1;
        expect(around(there.content)).toEqual(around(mine));
      } else {
        seen.none += 1;
        expect(there.content.startsWith(mine)).toBe(true);
        const parts = around(there.content);
        expect(parts?.after).toBe("");
        expect(parts?.before.slice(mine.length)).toMatch(
          /^(?:\r\n|\r|\n){0,2}$/,
        );
      }

      // The server's block, spliced in on the machine, is the same file.
      expect(upsertManagedBlock(mine, inner(server.content))).toBe(
        there.content,
      );
      expect(there.drift_sha256).toBe(server.drift_sha256);
      expect(isDrifted(server.drift_sha256, there.content)).toBe(false);
      // Idempotent: compiling over its own output changes nothing.
      expect(compileWith(there.content).content).toBe(there.content);
    }
    for (const [what, n] of Object.entries(seen))
      expect(n, what).toBeGreaterThan(100);
  });
});
