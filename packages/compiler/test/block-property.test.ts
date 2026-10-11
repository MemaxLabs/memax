/**
 * Rule 6 for every kind that manages one block in a file the person owns,
 * over 2,000 random files each (seeded, so failures reproduce):
 *
 * - every byte outside the markers stays as it was: line endings, a BOM,
 *   trailing spaces, a missing final newline, lines that look like Memax's;
 * - a file with no block gets one right after its last line, with at most
 *   the line break that line lacked;
 * - the server compiles the block alone (it never holds the person's lines),
 *   and splicing that block into the file on a machine, as the daemon does,
 *   gives the bytes compiling against the whole file would, with the same
 *   drift hash;
 * - compiling over its own output changes nothing;
 * - broken markers are refused, never guessed at;
 * - taking the block out (a target stopped, a machine unlinked or signed
 *   out) gives back the person's file byte for byte as it was before the
 *   first delivery, however many deliveries came between and whatever the
 *   person wrote above the block meanwhile.
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
  removeManagedBlock,
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
    name: "copilot_user in ~/.copilot/copilot-instructions.md",
    space: "personal",
    target: (current) => ({ kind: "copilot_user", current }),
  },
  {
    name: "gemini_user in ~/.gemini/AGENTS.md",
    space: "personal",
    target: (current) => ({ kind: "gemini_user", current }),
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
  /** The record's compile over `current`; `later`, after a Keep and a Forget. */
  const compileWith = (current: string, later = false) => {
    const i = input(
      [
        memory("M-0001", "Answer in British English."),
        ...(later
          ? [memory("M-0004", "Prefer small pull requests.")]
          : [
              memory("M-0002", "Commit messages are imperative.", {
                flags: ["stale"],
              }),
            ]),
        memory("M-0003", "Only for this agent.", {
          scope: { agents: ["claude-code", "codex", "gemini"] },
        }),
      ],
      [spec.target(current)],
    );
    i.space.kind = spec.space;
    return compile(i).files[0];
  };

  // 2,000 compiles take half a second here but passed vitest's 5 s default
  // on a busy CI runner: the property is the point, so it gets the time.
  it(
    `stay byte for byte over ${FILES} random files, and the machine's splice matches`,
    { timeout: 60_000 },
    () => {
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
          expect(parts?.before.slice(mine.length)).toMatch(/^(?:\r\n|\r|\n)?$/);
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
    },
  );

  // As above: about 8,000 compiles, well within a second here, given the
  // time a busy CI runner may need.
  it(
    `come back byte for byte when the block is taken out, over ${FILES} random files`,
    { timeout: 60_000 },
    () => {
      const server = compileWith("");
      const later = compileWith("", true);
      const random = rng(spec.name.length * 104_729);
      const above = ["# Added later\n", "\n", "- one more of mine\r\n", "x\r"];
      const seen = { none: 0, present: 0 };

      for (const mine of personFiles(spec.name.length * 7_919 + 1)) {
        let hadBlock: boolean;
        try {
          hadBlock = findManagedBlock(mine) !== null;
        } catch {
          continue; // refused, never delivered (the property above)
        }
        // The first delivery, on the server's side and on the machine's.
        const first = compileWith(mine).content;
        expect(upsertManagedBlock(mine, inner(server.content))).toBe(first);
        // A later delivery over it, after the record changed.
        const second = compileWith(first, true).content;
        expect(upsertManagedBlock(first, inner(later.content))).toBe(second);

        if (hadBlock) {
          // V1's block or an older one: what the person had outside it.
          seen.present += 1;
          expect(removeManagedBlock(second)).toBe(removeManagedBlock(mine));
          continue;
        }
        seen.none += 1;
        expect(removeManagedBlock(first)).toBe(mine);
        expect(removeManagedBlock(second)).toBe(mine);
        // A line the person wrote above the block meanwhile stays, alone.
        const theirs = above[Math.floor(random() * above.length)];
        expect(removeManagedBlock(theirs + second)).toBe(theirs + mine);
      }
      for (const [what, n] of Object.entries(seen))
        expect(n, what).toBeGreaterThan(100);
    },
  );
});
