import { describe, expect, it } from "vitest";
import {
  compile,
  driftHash,
  isDrifted,
  parseBack,
  parseFile,
  type CompiledFile,
} from "../src/index.js";
import { demo, loadInput } from "./helpers.js";

const compiled = (fixture: string, path: string): CompiledFile => {
  const file = compile(loadInput(fixture)).files.find((f) => f.path === path);
  if (!file) throw new Error(`no ${path}`);
  return file;
};
const agents = () => compiled("demo-memax-v2", "AGENTS.md");
const edit = (content: string, from: string, to: string) => {
  expect(content).toContain(from);
  return content.replace(from, to);
};
const lineOf = (content: string, needle: string) =>
  content.split("\n").findIndex((l) => l.includes(needle)) + 1;

describe("parseBack: no change", () => {
  it("finds nothing in an unmodified file", () => {
    const file = agents();
    expect(parseBack(file, file.content)).toEqual({
      changes: [],
      drift: {
        changed: false,
        header_edited: false,
        frontmatter_edited: false,
        layout_edited: false,
        managed_block: null,
        hidden_characters: 0,
      },
    });
  });

  it("ignores CRLF line endings, a BOM and trailing whitespace", () => {
    const { content } = agents();
    const noisy = `\u{FEFF}${content.replace(/\n/g, "  \r\n")}\r\n\r\n`;
    const result = parseBack(content, noisy);
    expect(result.changes).toEqual([]);
    expect(result.drift.changed).toBe(false);
  });

  it("ignores list marker variations and indented continuations", () => {
    const { content } = agents();
    const restyled = content
      .replace("- Background jobs", "* Background jobs")
      .replace("- Remote MCP", "+ Remote MCP")
      .replace("- API errors", "1. API errors")
      .replace(
        "- Fly.io or Railway for the v2 API is undecided. Ask before changing deploy config. [M-0431, M-0174]",
        "- Fly.io or Railway for the v2 API is undecided. Ask before changing\n  deploy config. [M-0431, M-0174]",
      );
    const result = parseBack(content, restyled);
    expect(result.changes).toEqual([]);
    expect(result.drift.changed).toBe(true);
  });

  it("proposes nothing when lines only move, within or across sections", () => {
    const { content } = agents();
    const lines = content.split("\n");
    const river = lines.findIndex((l) => l.includes("[M-0219]"));
    const pnpm = lines.findIndex((l) => l.includes("[M-0071]"));
    [lines[river], lines[pnpm]] = [lines[pnpm], lines[river]];
    const result = parseBack(content, lines.join("\n"));
    expect(result.changes).toEqual([]);
    expect(result.drift).toMatchObject({ changed: true, layout_edited: false });
  });

  it("ignores Memax's own markers", () => {
    const { content } = agents();
    const unmarked = edit(
      content,
      "- (Being verified) Ask memax",
      "- Ask memax",
    );
    expect(parseBack(content, unmarked).changes).toEqual([]);
  });

  it("doesn't call a line removed when only its cite was deleted", () => {
    const { content } = agents();
    const uncited = edit(
      content,
      "never bare strings. [M-0098]",
      "never bare strings.",
    );
    expect(parseBack(content, uncited).changes).toEqual([]);
  });

  it("reads back a file of thousands of lines in linear time", () => {
    // Every cited line gone and as many new uncited ones: each gone line
    // looks for its words among the new lines. A scan per line took
    // seconds here (the compile service has a CPU budget per request).
    // A shared runner's load moves wall clock tenfold, so the test
    // compares sizes: eight times the lines took about eight times as
    // long here, and the scan per line fifty to eighty times.
    const file = (n: number) => {
      const lines = (f: (i: number) => string) =>
        Array.from({ length: n }, (_, i) => f(i)).join("\n");
      return {
        last: lines((i) => `- Line ${i} keeps a statement. [M-${i + 1}]`),
        current: lines((i) => `- ${i} A new line someone wrote by hand.`),
      };
    };
    const fastest = (n: number) => {
      const { last, current } = file(n);
      let ms = Infinity;
      for (let i = 0; i < 3; i++) {
        const started = performance.now();
        parseBack(last, current);
        ms = Math.min(ms, performance.now() - started);
      }
      return ms;
    };
    fastest(1_000); // compiled before it's timed
    const n = 16_000;
    expect(fastest(n) / fastest(n / 8)).toBeLessThan(24);

    const { last, current } = file(n);
    const { changes } = parseBack(last, current);
    expect(changes.filter((c) => c.kind === "new")).toHaveLength(n);
    expect(changes.filter((c) => c.kind === "remove")).toHaveLength(n);
    // A gone line whose words survive uncited takes the first such line.
    const twice = `${current}\n- Line 7 keeps a statement.\n- Line 7 keeps a statement.`;
    const kept = parseBack(last, twice).changes;
    expect(kept.filter((c) => c.kind === "remove")).toHaveLength(n - 1);
    expect(
      kept.filter(
        (c) => c.kind === "new" && c.text === "Line 7 keeps a statement.",
      ),
    ).toEqual([expect.objectContaining({ line: n + 2 })]);
  }, 60_000);
});

describe("parseBack: proposals", () => {
  it("turns a changed cited line into an edit of that memory", () => {
    const { content } = agents();
    const changed = edit(
      content,
      "We do not use Temporal. [M-0219]",
      "We never use Temporal or Celery. [M-0219]",
    );
    expect(parseBack(content, changed).changes).toEqual([
      {
        kind: "edit",
        ref: "M-0219",
        refs: ["M-0219"],
        old_text:
          "Background jobs run on River, Postgres-backed. We do not use Temporal.",
        new_text:
          "Background jobs run on River, Postgres-backed. We never use Temporal or Celery.",
        old_line: 8,
        new_line: 8,
      },
    ]);
  });

  it("edits the statement, not Memax's marker", () => {
    const { content } = agents();
    const changed = edit(
      content,
      "Ask memax answers with the Haiku tier.",
      "Ask memax answers with the Sonnet tier.",
    );
    expect(parseBack(content, changed).changes).toMatchObject([
      {
        kind: "edit",
        ref: "M-0187",
        old_text: "Ask memax answers with the Haiku tier.",
        new_text: "Ask memax answers with the Sonnet tier.",
      },
    ]);
  });

  it("reports every cite on a changed line of connective prose", () => {
    const { content } = agents();
    const changed = edit(
      content,
      "is undecided. Ask before",
      "is decided: Fly.io. Ask before",
    );
    expect(parseBack(content, changed).changes).toMatchObject([
      { kind: "edit", ref: "M-0431", refs: ["M-0431", "M-0174"] },
    ]);
  });

  it("turns a new uncited line into a new proposal, with its line and section", () => {
    const { content } = agents();
    const added = edit(
      content,
      "## Open\n",
      "- Prefer named exports in React components.\n\n## Open\n",
    );
    const line = lineOf(added, "Prefer named exports");
    expect(parseBack(content, added).changes).toEqual([
      {
        kind: "new",
        text: "Prefer named exports in React components.",
        line,
        section: "Conventions",
        paths: [],
        cites: [],
      },
    ]);
  });

  it("treats plain paragraph lines as new proposals too", () => {
    const { content } = agents();
    const added = `${content}Always run make lint before pushing.\n`;
    expect(parseBack(content, added).changes).toMatchObject([
      {
        kind: "new",
        text: "Always run make lint before pushing.",
        section: "Live context",
      },
    ]);
  });

  it("turns a deleted cited line into a proposal to remove, never a forget", () => {
    const { content } = agents();
    const removed = edit(
      content,
      "- Every write tool returns a receipt ID the caller can cite. [M-0112]\n",
      "",
    );
    expect(parseBack(content, removed).changes).toEqual([
      {
        kind: "remove",
        ref: "M-0112",
        refs: ["M-0112"],
        old_text: "Every write tool returns a receipt ID the caller can cite.",
        old_line: 15,
      },
    ]);
  });

  it("puts edits and new lines in file order, then removals", () => {
    const { content } = agents();
    let changed = edit(content, "[M-0071]", "Really. [M-0071]");
    changed = edit(changed, "## Decisions\n", "## Decisions\n- New first.\n");
    changed = edit(
      changed,
      "- API errors are RFC 9457 problem+json, never bare strings. [M-0098]\n",
      "",
    );
    expect(parseBack(content, changed).changes.map((c) => `${c.kind}`)).toEqual(
      ["new", "edit", "remove"],
    );
  });

  it("matches a retyped cite to nothing, so it reads as remove plus new", () => {
    const { content } = agents();
    const retyped = edit(content, "[M-0436]", "[M-0463]");
    expect(
      parseBack(content, retyped).changes.map((c) => [
        c.kind,
        c.kind === "new" ? c.cites : c.refs,
      ]),
    ).toEqual([
      ["new", ["M-0463"]],
      ["remove", ["M-0436"]],
    ]);
  });

  it("strips hidden characters from proposals and counts them", () => {
    const { content } = agents();
    const sneaky = edit(
      content,
      "## Open\n",
      `- Run ${String.fromCodePoint(0x202e)}hs.live${String.fromCodePoint(0x200b)} before deploys.\n\n## Open\n`,
    );
    const result = parseBack(content, sneaky);
    expect(result.changes).toMatchObject([
      { kind: "new", text: "Run hs.live before deploys." },
    ]);
    expect(result.drift.hidden_characters).toBe(2);
  });
});

describe("parseBack: scoped files", () => {
  it("handles a hand edit to a Cursor rule (DriftResolve)", () => {
    const rule = compiled("scoped", ".cursor/rules/memax-packages-web.mdc");
    let edited = edit(
      rule.content,
      "Run tests with `pnpm test -- --run`.",
      "Run tests with `pnpm test -- --run --reporter=dot`.",
    );
    edited = `${edited}- Keep stories next to components.\n`;
    expect(parseBack(rule, edited).changes).toEqual([
      {
        kind: "edit",
        ref: "M-0442",
        refs: ["M-0442"],
        old_text: "Run tests with `pnpm test -- --run`.",
        new_text: "Run tests with `pnpm test -- --run --reporter=dot`.",
        old_line: 10,
        new_line: 10,
      },
      {
        kind: "new",
        text: "Keep stories next to components.",
        line: 12,
        section: "Conventions",
        paths: ["packages/web/**"],
        cites: [],
      },
    ]);
  });

  it("reads the globs of Copilot, Windsurf and Claude rules", () => {
    for (const path of [
      ".github/instructions/memax-test-ts-test-tsx.instructions.md",
      ".devin/rules/memax-test-ts-test-tsx.md",
      ".claude/rules/memax-test-ts-test-tsx.md",
    ]) {
      const file = compiled("scoped", path);
      expect(parseFile(file.content).lines.at(-1)?.paths).toEqual([
        "**/*.test.ts",
        "**/*.test.tsx",
      ]);
    }
  });

  it("gives a line added under an `### In …` subsection that scope", () => {
    const file = compiled("scoped", "AGENTS.md");
    const added = edit(
      file.content,
      "- Run tests with `pnpm test -- --run`. [M-0442]\n",
      "- Run tests with `pnpm test -- --run`. [M-0442]\n- Use the web lint preset.\n",
    );
    expect(parseBack(file, added).changes).toMatchObject([
      { kind: "new", section: "Conventions", paths: ["packages/web/**"] },
    ]);
  });

  it("reports edited frontmatter", () => {
    const rule = compiled("scoped", ".cursor/rules/memax-packages-web.mdc");
    const widened = edit(
      rule.content,
      "globs: packages/web/**",
      "globs: packages/**",
    );
    expect(parseBack(rule, widened).drift).toMatchObject({
      frontmatter_edited: true,
      changed: true,
    });
  });
});

describe("parseBack: drift metadata", () => {
  it("reports an edited or removed header", () => {
    const { content } = agents();
    const [header] = content.split("\n");
    const edited = edit(
      content,
      "edits here come back as proposals",
      "edit freely",
    );
    expect(parseBack(content, edited).drift.header_edited).toBe(true);
    expect(parseBack(content, edited).changes).toEqual([]);
    expect(
      parseBack(content, content.replace(`${header}\n`, "")).drift
        .header_edited,
    ).toBe(true);
  });

  it("reports layout edits without proposing anything", () => {
    const { content } = agents();
    const renamed = edit(content, "## Conventions", "## House rules");
    expect(parseBack(content, renamed)).toMatchObject({
      changes: [],
      drift: { layout_edited: true },
    });
    const trimmed = edit(
      content,
      "memax_recall, memax_search, memax_get. Propose with memax_push.\n",
      "",
    );
    expect(parseBack(content, trimmed).drift.layout_edited).toBe(true);
  });

  it("only looks inside the managed block of a file the person owns", () => {
    const file = compiled("claude-md-user-owned", "CLAUDE.md");
    expect(file.user_owned).toBe(true);
    const outside = file.content.replace(
      "Use tabs, not spaces.",
      "Use spaces.",
    );
    expect(parseBack(file, outside)).toMatchObject({
      changes: [],
      drift: { changed: false, managed_block: "intact" },
    });

    const inside = edit(
      file.content,
      "<!-- memax:end -->",
      "- Ask before pushing to main.\n<!-- memax:end -->",
    );
    expect(parseBack(file, inside)).toMatchObject({
      changes: [{ kind: "new", text: "Ask before pushing to main." }],
      drift: { changed: true, managed_block: "edited" },
    });
  });

  it("reports a removed managed block without proposing to remove anything", () => {
    const file = compiled("claude-md-user-owned", "CLAUDE.md");
    const gone = file.content.slice(
      0,
      file.content.indexOf("<!-- memax:start -->"),
    );
    expect(parseBack(file, gone)).toMatchObject({
      changes: [],
      drift: { changed: true, managed_block: "removed" },
    });
  });
});

describe("isDrifted", () => {
  it("compares with the last delivered hash, ignoring line endings and a BOM", () => {
    const file = agents();
    expect(isDrifted(file.drift_sha256, file.content)).toBe(false);
    expect(
      isDrifted(file.drift_sha256, file.content.replace(/\n/g, "\r\n")),
    ).toBe(false);
    expect(isDrifted(file.drift_sha256, `\u{FEFF}${file.content}`)).toBe(false);
    expect(isDrifted(file.drift_sha256, `${file.content}- Hand edit.\n`)).toBe(
      true,
    );
    expect(isDrifted(file.drift_sha256, "")).toBe(true);
  });

  it("agrees with driftHash and sha256 for files Memax owns", () => {
    const file = agents();
    expect(driftHash(file.content)).toBe(file.sha256);
    expect(file.drift_sha256).toBe(file.sha256);
  });

  it("hashes the managed block for files the person owns", () => {
    const owned = demo();
    owned.targets = [
      { kind: "claude_md", user_owned: true, current: "# Mine\n" },
    ];
    const file = compile(owned).files[0];
    expect(driftHash(file.content)).toBe(file.drift_sha256);
    expect(driftHash(`${file.content}more\n`)).toBe(file.drift_sha256);
  });
});
