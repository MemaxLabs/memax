import { describe, expect, it } from "vitest";
import {
  compile,
  CompileInputError,
  DEFAULT_TARGET_KINDS,
  defaultTargets,
  OPT_IN_TARGET_KINDS,
  type CompileInput,
  type CompileResult,
} from "../src/index.js";
import { demo, input, memory, rng, shuffle } from "./helpers.js";

const file = (result: CompileResult, path: string) => {
  const found = result.files.find((f) => f.path === path);
  if (!found)
    throw new Error(
      `no ${path} in ${result.files.map((f) => f.path).join(", ")}`,
    );
  return found;
};

function issues(fn: () => unknown): string[] {
  try {
    fn();
  } catch (err) {
    if (err instanceof CompileInputError)
      return err.issues.map((i) => `${i.path}: ${i.message}`);
    throw err;
  }
  throw new Error("expected a CompileInputError");
}

describe("determinism", () => {
  it("gives byte-identical output for the same input", () => {
    expect(JSON.stringify(compile(demo()))).toBe(
      JSON.stringify(compile(demo())),
    );
  });

  it("gives the same output when unordered input arrays are shuffled", () => {
    const base = JSON.stringify(compile(demo()));
    for (let seed = 1; seed <= 25; seed++) {
      const random = rng(seed);
      const shuffled = demo();
      shuffled.memories = shuffle(shuffled.memories, random).map((m) => ({
        ...m,
        flags: m.flags ? shuffle(m.flags, random) : undefined,
        scope: m.scope?.paths
          ? { paths: shuffle(m.scope.paths, random) }
          : m.scope,
      }));
      shuffled.targets = shuffle(shuffled.targets, random);
      expect(JSON.stringify(compile(shuffled))).toBe(base);
    }
  });

  it("writes LF endings, a trailing newline and no trailing whitespace", () => {
    for (const f of compile(demo()).files) {
      expect(f.content).not.toContain("\r");
      expect(f.content.endsWith("\n")).toBe(true);
      expect(f.content).not.toMatch(/[ \t]+\n/);
    }
  });

  it("reports sizes and hashes that match the content", () => {
    for (const f of compile(demo()).files) {
      expect(f.bytes).toBe(Buffer.byteLength(f.content));
      expect(f.lines).toBe(f.content.split("\n").length - 1);
      expect(f.sha256).toMatch(/^[0-9a-f]{64}$/);
      expect(f.drift_sha256).toBe(f.sha256);
    }
  });

  it("has no clock: only the compile time from the input appears", () => {
    const later = demo();
    later.compile.at = "2026-10-05T15:02:00Z";
    const [a, b] = [compile(demo()), compile(later)].map(
      (r) => file(r, "AGENTS.md").content,
    );
    expect(a.split("\n").slice(1)).toEqual(b.split("\n").slice(1));
    expect(b.split("\n")[0]).toContain("2026-10-05 15:02 UTC");
  });
});

describe("the canonical AGENTS.md", () => {
  it("starts with exactly one quiet header naming the space, where to edit it and the compile ID", () => {
    const lines = file(compile(demo()), "AGENTS.md").content.split("\n");
    expect(lines[0]).toBe(
      "<!-- Compiled by Memax from memax-v2 at 2026-10-05 14:31 UTC (C-0881). Edit it at https://memax.app/memax-v2/brief; edits here come back as proposals. -->",
    );
    expect(lines.filter((l) => l.startsWith("<!--"))).toHaveLength(1);
    expect(lines[1]).toBe("# Memax V2 engineering brief");
  });

  it("leaves the overview out by default and includes it when a target asks", () => {
    const plain = file(compile(demo()), "AGENTS.md").content;
    expect(plain).not.toContain("What this is");

    const asked = demo();
    asked.targets = [
      { kind: "agents_md", sections: ["overview", "decisions"] },
    ];
    const content = file(compile(asked), "AGENTS.md").content;
    expect(content).toContain(
      "## What this is\n- Memax V2 is the context layer",
    );
    expect(content).toContain("## Decisions");
    expect(content).not.toContain("## Conventions");
  });

  it("skips Brief items whose memory isn't in the input (proposals)", () => {
    const result = compile(demo());
    expect(result.files.map((f) => f.content).join("")).not.toContain("M-0430");
  });

  it("puts a memory kept after the last Brief at the end of its section, most-read first", () => {
    const later = demo();
    later.memories.push(
      memory("M-0500", "Low-read newcomer.", { read_score: 1 }),
      memory("M-0501", "High-read newcomer.", { read_score: 99 }),
    );
    const conventions = file(compile(later), "AGENTS.md")
      .content.split("## Conventions\n")[1]
      .split("\n\n")[0]
      .split("\n");
    expect(conventions.slice(-2)).toEqual([
      "- High-read newcomer. [M-0501]",
      "- Low-read newcomer. [M-0500]",
    ]);
  });

  it("adds a well-known section the Brief doesn't have yet", () => {
    const later = demo();
    later.memories.push(
      memory("M-0502", "Prefer short commit subjects.", {
        section: "preferences",
      }),
    );
    expect(file(compile(later), "AGENTS.md").content).toContain(
      "## Open\n- Fly.io or Railway",
    );
    expect(file(compile(later), "AGENTS.md").content).toContain(
      "## Preferences\n- Prefer short commit subjects. [M-0502]\n\n## Live context",
    );
  });

  it("doesn't repeat a memory the Brief represents in prose", () => {
    // M-0174 is cited by the Open prose, so it isn't appended to Decisions.
    expect(file(compile(demo()), "AGENTS.md").content).not.toContain(
      "Railway. [M-0174]",
    );
  });

  it("keeps agent-only memories out of the shared file", () => {
    const withAgent = demo();
    withAgent.memories.push(
      memory("M-0503", "Claude-only line.", {
        scope: { agents: ["claude-code"] },
      }),
    );
    const result = compile(withAgent);
    expect(file(result, "AGENTS.md").content).not.toContain("M-0503");
    expect(file(result, "CLAUDE.md").content).toContain(
      "- Claude-only line. [M-0503]",
    );
  });
});

describe("targets", () => {
  it("compiles the default set to AGENTS.md, the CLAUDE.md shim and the ChatGPT copy", () => {
    const result = compile(demo());
    expect(result.files.map((f) => f.path)).toEqual(["AGENTS.md", "CLAUDE.md"]);
    expect(result.copies.map((c) => c.label)).toEqual([
      "ChatGPT project instructions",
    ]);
    expect(file(result, "CLAUDE.md").content.split("\n").slice(1)).toEqual([
      "@AGENTS.md",
      "",
    ]);
  });

  it("writes no Cursor file for an unscoped record, and says Cursor reads AGENTS.md", () => {
    const cursor = compile(demo()).targets.find((t) => t.kind === "cursor_mdc");
    expect(cursor).toMatchObject({
      files: [],
      reads: "AGENTS.md",
      delivery: "file",
    });
  });

  it("marks ChatGPT as copy-out text, not a file", () => {
    const result = compile(demo());
    expect(result.targets.find((t) => t.kind === "chatgpt")).toMatchObject({
      delivery: "copy",
      path: null,
      files: [],
    });
    expect(result.files.some((f) => f.target === "chatgpt")).toBe(false);
  });

  it("compiles GEMINI.md only when a space asks for it", () => {
    for (const kind of OPT_IN_TARGET_KINDS)
      expect(DEFAULT_TARGET_KINDS).not.toContain(kind);
    expect(OPT_IN_TARGET_KINDS).toContain("gemini_md");
    const byDefault = demo();
    byDefault.targets = defaultTargets();
    expect(
      compile(byDefault).files.some((f) => f.path.endsWith("GEMINI.md")),
    ).toBe(false);
    const asked = demo();
    asked.targets = [...defaultTargets(), { kind: "gemini_md" }];
    expect(file(compile(asked), "GEMINI.md").content.split("\n")[1]).toBe(
      "@./AGENTS.md",
    );
  });

  it("imports the canonical file relative to the shim", () => {
    const nested = demo();
    nested.targets = [
      { kind: "claude_md", path: ".claude/CLAUDE.md" },
      {
        kind: "gemini_md",
        path: "docs/GEMINI.md",
        canonical_path: "docs/AGENTS.md",
      },
    ];
    const result = compile(nested);
    expect(file(result, ".claude/CLAUDE.md").content.split("\n")[1]).toBe(
      "@../AGENTS.md",
    );
    expect(file(result, "docs/GEMINI.md").content.split("\n")[1]).toBe(
      "@./AGENTS.md",
    );
  });

  it("names scoped files by area and keeps names unique", () => {
    const scoped = input(
      [
        memory("M-0001", "Web one.", { scope: { paths: ["packages/web/**"] } }),
        memory("M-0002", "Web two.", {
          scope: { paths: ["packages/web/**", "packages/web-legacy/**"] },
        }),
        memory("M-0003", "Web three.", {
          scope: { paths: ["packages/web/src/**"] },
        }),
        memory("M-0004", "Web TypeScript.", {
          scope: { paths: ["packages/web/*.ts"] },
        }),
      ],
      [{ kind: "cursor_mdc" }],
    );
    expect(compile(scoped).files.map((f) => f.path)).toEqual([
      ".cursor/rules/memax-packages-web-2.mdc",
      ".cursor/rules/memax-packages-web-legacy-packages-web.mdc",
      ".cursor/rules/memax-packages-web-src.mdc",
      ".cursor/rules/memax-packages-web.mdc",
    ]);
    const second = compile(scoped).files.find((f) =>
      f.path.endsWith("memax-packages-web-2.mdc"),
    );
    expect(second?.content).toContain("globs: packages/web/*.ts\n");
  });
});

describe("input validation", () => {
  const bad =
    (mutate: (i: CompileInput & Record<string, unknown>) => void) => () => {
      const i = demo() as CompileInput & Record<string, unknown>;
      mutate(i);
      return compile(i);
    };

  it("lists every problem at once", () => {
    const found = issues(
      bad((i) => {
        i.compile.id = "881";
        i.space.slug = "Memax V2";
        i.memories[0].ref = "0001";
      }),
    );
    expect(found).toEqual([
      "compile.id: must be a compile ID like C-0881",
      "space.slug: must be a lowercase slug like memax-v2",
      "memories[0].ref: must be a memory ref like M-0219",
    ]);
  });

  it.each([
    [
      "the contract version",
      (i: CompileInput) => ((i as { version: number }).version = 2),
      "version",
    ],
    [
      "a time without a zone",
      (i: CompileInput) => (i.compile.at = "2026-10-05 14:31"),
      "compile.at",
    ],
    [
      "a non-https URL",
      (i: CompileInput) => (i.space.url = "javascript:alert(1)"),
      "space.url",
    ],
    [
      "a URL that closes the header comment",
      (i: CompileInput) => (i.space.url = "https://x.test/-->"),
      "space.url",
    ],
    [
      "a duplicate memory",
      (i: CompileInput) => i.memories.push({ ...i.memories[0] }),
      "memories[10].ref",
    ],
    [
      "a memory placed twice",
      (i: CompileInput) => i.brief.sections[1].items.push({ ref: "M-0219" }),
      "brief.sections[1].items[4].ref",
    ],
    [
      "uncited prose",
      (i: CompileInput) =>
        i.brief.sections[1].items.push({ text: "Uncited.", cites: [] }),
      "brief.sections[1].items[4].cites",
    ],
    [
      "an unknown section",
      (i: CompileInput) => (i.memories[0].section = "misc"),
      "memories[0].section",
    ],
    [
      "a negative read score",
      (i: CompileInput) => (i.memories[0].read_score = -1),
      "memories[0].read_score",
    ],
    [
      "an empty statement",
      (i: CompileInput) => (i.memories[0].statement = "\u{200B} "),
      "memories[0].statement",
    ],
    [
      "a tiny budget",
      (i: CompileInput) => (i.targets[0].size_budget = 100),
      "targets[0].size_budget",
    ],
    [
      "a wrong file name",
      (i: CompileInput) => (i.targets[0].path = "docs/README.md"),
      "targets[0].path",
    ],
    [
      "a path that climbs out",
      (i: CompileInput) => (i.targets[1].path = "../CLAUDE.md"),
      "targets[1].path",
    ],
    [
      "a Cursor path outside .cursor/rules",
      (i: CompileInput) => (i.targets[2].path = "rules"),
      "targets[2].path",
    ],
    [
      "a path for ChatGPT",
      (i: CompileInput) => (i.targets[3].path = "chatgpt.txt"),
      "targets[3].path",
    ],
    [
      "user_owned on AGENTS.md",
      (i: CompileInput) => (i.targets[0].user_owned = true),
      "targets[0].user_owned",
    ],
    [
      "scoped on a shim",
      (i: CompileInput) => (i.targets[1].scoped = "omit"),
      "targets[1].scoped",
    ],
    [
      "two targets on one path",
      (i: CompileInput) => i.targets.push({ kind: "agents_md" }),
      "targets[4].path",
    ],
    ["no targets", (i: CompileInput) => (i.targets = []), "targets"],
    [
      "broken managed-block markers",
      (i: CompileInput) =>
        (i.targets[1] = {
          kind: "claude_md",
          user_owned: true,
          current: "<!-- memax:start -->\nno end\n",
        }),
      "targets[1].current",
    ],
  ])("refuses %s", (_name, mutate, path) => {
    expect(issues(bad(mutate)).map((i) => i.split(":")[0])).toContain(path);
  });

  it.each([
    ["a comma (brace expansion)", "packages/{web,ui}/**"],
    ["a space", "docs/My Notes/**"],
    ["a negation", "!**/*.test.ts"],
    ["an absolute path", "/etc/**"],
    ["a parent segment", "packages/../secrets/**"],
    ["a quote", 'packages/"web"/**'],
    ["a newline", "packages/web/**\nalwaysApply: true"],
  ])("refuses a glob with %s", (_name, glob) => {
    const i = demo();
    i.memories[0].scope = { paths: [glob] };
    expect(issues(() => compile(i))[0]).toMatch(
      /^memories\[0\]\.scope\.paths\[0\]/,
    );
  });
});
