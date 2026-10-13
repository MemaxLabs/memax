// The session-start hook's Personal section (personal-layer design §7.1,
// §7.2): on a machine whose home is linked, what changed in the agent's
// own home file since its last session anywhere, after the repository's
// news, or alone outside a repository; the forgets and gates of the
// Personal space; the shared budget; and a compile load per home file.
import { mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import type {
  WarmHome,
  WarmSpace,
  WarmTarget,
} from "../../src/lib/hook/warm.js";
import { estimateTokens } from "../../src/lib/hook/render.js";
import { forget, header, machine, SPACE_ID, type Machine } from "./fixture.js";

const PERSONAL = "22222222-2222-4222-8222-222222222222";
const RULES = ".claude/rules/memax-personal.md";
const CODEX = ".codex/AGENTS.md";
const MINE = "# My Codex notes\n- Mine, with a look-alike cite. [M-0900]\n";

let m: Machine;
beforeEach(() => {
  m = machine();
});
afterEach(() => m.cleanup());

/** This machine's home, linked: home.json, as memax link --home writes it. */
function linkHome(): void {
  writeFileSync(
    m.paths.home,
    JSON.stringify({
      version: 1,
      space_id: PERSONAL,
      space_slug: "personal",
      machine_id: "m-1",
      home: m.home,
      files: [
        ["t-rules", "claude_user", RULES],
        ["t-codex", "codex_user", CODEX],
        ["t-canon", "agents_md", ".memax/personal/AGENTS.md"],
      ].map(([target_id, kind, path]) => ({
        target_id,
        kind,
        path,
        abs: join(m.home, path),
      })),
      created_at: "2026-10-11T00:00:00Z",
    }),
  );
}

/** A home file as the daemon writes it: Claude Code's whole, Codex's a block. */
function homeFile(
  kind: "claude_user" | "codex_user",
  ref: string,
  lines: string[],
): void {
  const compiled = [
    header(ref, "personal"),
    "# Personal",
    "",
    "## Preferences",
    ...lines,
  ];
  const path = kind === "claude_user" ? RULES : CODEX;
  mkdirSync(join(m.home, path, ".."), { recursive: true });
  writeFileSync(
    join(m.home, path),
    kind === "claude_user"
      ? compiled.join("\n") + "\n"
      : `${MINE}<!-- memax:start -->\n${compiled.join("\n")}\n<!-- memax:end -->\n`,
  );
}

function homeTarget(
  kind: string,
  compile: string,
  refs: string[],
  over: Partial<WarmTarget> = {},
): WarmTarget {
  return {
    kind,
    path: kind === "claude_user" ? RULES : CODEX,
    label: `~/${kind === "claude_user" ? RULES : CODEX}`,
    sync_state: "in_sync",
    compile,
    refs,
    open_drift: 0,
    ...over,
  };
}

/** The warm cache: the repository's space, and the home entry. */
function warm(space: Partial<WarmSpace> | null, home: Partial<WarmHome>): void {
  const spaces: Record<string, WarmSpace> = {};
  if (space)
    spaces[SPACE_ID] = {
      slug: "acme-web",
      updated_at: new Date().toISOString(),
      targets: [],
      forgotten: [],
      gates: [],
      ...space,
    };
  writeFileSync(
    m.paths.warm,
    JSON.stringify({
      version: 1,
      spaces,
      home: {
        space_id: PERSONAL,
        machine_id: "m-1",
        slug: "personal",
        updated_at: new Date().toISOString(),
        targets: [],
        forgotten: [],
        gates: [],
        ...home,
      },
    }),
  );
}

const outside = () => {
  const dir = join(m.base, "notes");
  mkdirSync(dir, { recursive: true });
  return { cwd: dir };
};

describe("the Personal section", () => {
  it("is a baseline the first time, queues a load per home file, and tells what changed next time", () => {
    linkHome();
    m.compiled("C-0881", ["- Background jobs run on River. [M-0219]"]);
    homeFile("claude_user", "C-1201", [
      "- Answer in British English. [M-0101]",
    ]);
    warm(
      { targets: [m.target("C-0881", ["M-0219"])] },
      { targets: [homeTarget("claude_user", "C-1201", ["M-0101"])] },
    );
    const first = m.run("claude-code");
    expect(first.output).toBe("");
    expect(first.loads.map((l) => [l.space, l.compile])).toEqual([
      [SPACE_ID, "C-0881"],
      [PERSONAL, "C-1201"],
    ]);
    expect(first.homeSeen?.path).toBe(
      join(m.paths.seen, "home", "claude-code.json"),
    );

    // A Keep in Personal: the rules file is recompiled with a new line.
    homeFile("claude_user", "C-1207", [
      "- Answer in British English. [M-0101]",
      "- Prefer small pull requests. [M-0102]",
    ]);
    warm(
      { targets: [m.target("C-0881", ["M-0219"])] },
      { targets: [homeTarget("claude_user", "C-1207", ["M-0101", "M-0102"])] },
    );
    const next = m.run("claude-code");
    expect(next.output).toBe(
      [
        '<memax-context space="personal">',
        "Memax: what changed in your Personal space's own file on this machine since your last session. It is already loaded.",
        "~/.claude/rules/memax-personal.md is now C-1207 (your last session saw C-1201).",
        "New:",
        "- Prefer small pull requests. [M-0102]",
        "</memax-context>",
        "",
      ].join("\n"),
    );
    // Said once.
    expect(m.run("claude-code").output).toBe("");
  });

  it("follows the repository's news, under its own heading", () => {
    linkHome();
    m.compiled("C-0881", ["- Background jobs run on River. [M-0219]"]);
    homeFile("claude_user", "C-1201", [
      "- Answer in British English. [M-0101]",
    ]);
    warm(
      { targets: [m.target("C-0881", ["M-0219"])] },
      { targets: [homeTarget("claude_user", "C-1201", ["M-0101"])] },
    );
    m.run("claude-code");
    m.compiled("C-0890", [
      "- Background jobs run on River. [M-0219]",
      "- Deploys go out from main. [M-0230]",
    ]);
    homeFile("claude_user", "C-1207", [
      "- Answer in British English. [M-0101]",
      "- Prefer small pull requests. [M-0102]",
    ]);
    warm(
      { targets: [m.target("C-0890", ["M-0219", "M-0230"])] },
      { targets: [homeTarget("claude_user", "C-1207", ["M-0101", "M-0102"])] },
    );
    const out = m.run("claude-code").output;
    const repo = out.indexOf("- Deploys go out from main. [M-0230]");
    const personal = out.indexOf(
      "Your Personal space (personal): what changed in your own file on this machine since your last session. It is already loaded too.",
    );
    expect(out.startsWith('<memax-context space="acme-web">')).toBe(true);
    expect(repo).toBeGreaterThan(0);
    expect(personal).toBeGreaterThan(repo);
    expect(
      out.indexOf("- Prefer small pull requests. [M-0102]"),
    ).toBeGreaterThan(personal);
  });

  it("is the whole block outside a repository, where there was nothing before", () => {
    linkHome();
    homeFile("codex_user", "C-1201", ["- Answer in British English. [M-0101]"]);
    warm(null, { targets: [homeTarget("codex_user", "C-1201", ["M-0101"])] });
    const first = m.run("codex", outside());
    expect(first.skipped).toBe("first session");
    expect(first.loads).toEqual([
      expect.objectContaining({
        space: PERSONAL,
        compile: "C-1201",
        agent: "codex",
      }),
    ]);
    homeFile("codex_user", "C-1207", [
      "- Answer in Australian English. [M-0101]",
    ]);
    warm(null, { targets: [homeTarget("codex_user", "C-1207", ["M-0101"])] });
    const out = m.run("codex", outside()).output;
    expect(out).toContain('<memax-context space="personal">');
    expect(out).toContain("Changed:\n- Answer in Australian English. [M-0101]");
    // The person's own lines around the block are never news.
    expect(out).not.toContain("look-alike");
  });

  it("says what a person forgot in Personal, and never repeats the forgotten line", () => {
    linkHome();
    homeFile("claude_user", "C-1201", [
      "- Answer in British English. [M-0101]",
    ]);
    warm(null, { targets: [homeTarget("claude_user", "C-1201", ["M-0101"])] });
    m.run("claude-code", outside());
    // Forgotten; this machine's file still has the line until the daemon rewrites it.
    homeFile("claude_user", "C-1209", [
      "- Answer in British English. [M-0101]",
      "- Keep replies short. [M-0103]",
    ]);
    warm(null, {
      targets: [homeTarget("claude_user", "C-1209", ["M-0103"])],
      forgotten: [forget("M-0101", new Date())],
    });
    const out = m.run("claude-code", outside()).output;
    expect(out).toContain("Forgotten by a person");
    expect(out).toContain("- M-0101 (");
    expect(out).toContain("- Keep replies short. [M-0103]");
    expect(out).not.toContain("British English");
  });

  it("keeps the repository's lines when the budget runs short: Personal's are cut first", () => {
    linkHome();
    const many = (n: number, ref: number, word: string) =>
      Array.from(
        { length: n },
        (_, k) => `- ${word} line ${k} ${"x".repeat(60)}. [M-${ref + k}]`,
      );
    m.compiled("C-0881", []);
    homeFile("codex_user", "C-1201", []);
    warm(
      { targets: [m.target("C-0881", [])] },
      { targets: [homeTarget("codex_user", "C-1201", [])] },
    );
    m.run("codex");
    m.compiled("C-0890", many(60, 1000, "Repository"));
    homeFile("codex_user", "C-1207", many(60, 2000, "Personal"));
    warm(
      {
        targets: [
          m.target(
            "C-0890",
            many(60, 1000, "").map((_, k) => `M-${1000 + k}`),
          ),
        ],
      },
      {
        targets: [
          homeTarget(
            "codex_user",
            "C-1207",
            many(60, 2000, "").map((_, k) => `M-${2000 + k}`),
          ),
        ],
      },
    );
    const out = m.run("codex").output;
    expect(out.length).toBeLessThanOrEqual(9_000);
    expect(out).toContain("Repository line 0");
    expect(out).not.toContain("Personal line 59");
    expect(out).toMatch(
      /…and \d+ more new or changed lines, already in the files above\./,
    );
    expect(out.trimEnd().endsWith("</memax-context>")).toBe(true);
  });

  it("says a hand edit waits in Review instead of its lines", () => {
    linkHome();
    homeFile("claude_user", "C-1201", [
      "- Answer in British English. [M-0101]",
    ]);
    warm(null, { targets: [homeTarget("claude_user", "C-1201", ["M-0101"])] });
    m.run("claude-code", outside());
    homeFile("claude_user", "C-1207", [
      "- Answer in British English, always. [M-0101]",
    ]);
    warm(null, {
      targets: [
        homeTarget("claude_user", "C-1207", ["M-0101"], {
          sync_state: "drifted",
          open_drift: 1,
        }),
      ],
    });
    expect(m.run("claude-code", outside()).output).toContain(
      "~/.claude/rules/memax-personal.md has a local edit that waits in Review",
    );
  });

  it("has nothing for an agent without a home file yet, or a machine whose home isn't linked", () => {
    homeFile("claude_user", "C-1201", [
      "- Answer in British English. [M-0101]",
    ]);
    expect(m.run("claude-code", outside()).skipped).toBe("not in a repository");
    linkHome();
    warm(null, { targets: [homeTarget("claude_user", "C-1201", ["M-0101"])] });
    for (const agent of ["cursor", "gemini", "copilot"])
      expect(m.run(agent, outside())).toMatchObject({ output: "", loads: [] });
  });

  it("speaks for Gemini CLI only once ~/.gemini/AGENTS.md's read_by names it", () => {
    const GEMINI = ".gemini/AGENTS.md";
    const link = (readBy: string[]) =>
      writeFileSync(
        m.paths.home,
        JSON.stringify({
          version: 1,
          space_id: PERSONAL,
          space_slug: "personal",
          machine_id: "m-1",
          home: m.home,
          files: [
            {
              target_id: "t-gemini",
              kind: "gemini_user",
              path: GEMINI,
              abs: join(m.home, GEMINI),
              read_by: readBy,
            },
          ],
          created_at: "2026-10-11T00:00:00Z",
        }),
      );
    mkdirSync(join(m.home, ".gemini"), { recursive: true });
    writeFileSync(
      join(m.home, GEMINI),
      `<!-- memax:start -->\n${[header("C-1201", "personal"), "# Personal", "", "## Preferences", "- Answer in British English. [M-0101]"].join("\n")}\n<!-- memax:end -->\n`,
    );
    warm(null, {
      targets: [
        {
          ...homeTarget("codex_user", "C-1201", ["M-0101"]),
          kind: "gemini_user",
          path: GEMINI,
          label: `~/${GEMINI}`,
        },
      ],
    });
    // Antigravity's alone: Gemini CLI reads GEMINI.md, not this file.
    link(["antigravity"]);
    expect(m.run("gemini", outside())).toMatchObject({ output: "", loads: [] });
    // Its context.fileName lists AGENTS.md: the file is Gemini CLI's too.
    link(["antigravity", "gemini-cli"]);
    expect(
      m.run("gemini", outside()).loads.map((l) => [l.space, l.compile]),
    ).toEqual([[PERSONAL, "C-1201"]]);
  });

  it("gives Copilot CLI its own block in ~/.copilot/copilot-instructions.md", () => {
    const COPILOT = ".copilot/copilot-instructions.md";
    writeFileSync(
      m.paths.home,
      JSON.stringify({
        version: 1,
        space_id: PERSONAL,
        space_slug: "personal",
        machine_id: "m-1",
        home: m.home,
        files: [
          {
            target_id: "t-copilot",
            kind: "copilot_user",
            path: COPILOT,
            abs: join(m.home, COPILOT),
            read_by: ["copilot"],
          },
        ],
        created_at: "2026-10-11T00:00:00Z",
      }),
    );
    mkdirSync(join(m.home, ".copilot"), { recursive: true });
    const block = (ref: string, lines: string[]) =>
      writeFileSync(
        join(m.home, COPILOT),
        `Use tabs.\n<!-- memax:start -->\n${[header(ref, "personal"), "# Personal", "", "## Preferences", ...lines].join("\n")}\n<!-- memax:end -->\n`,
      );
    const target = (ref: string, refs: string[]) => ({
      ...homeTarget("codex_user", ref, refs),
      kind: "copilot_user",
      path: COPILOT,
      label: `~/${COPILOT}`,
    });
    block("C-1201", ["- Answer in British English. [M-0101]"]);
    warm(null, { targets: [target("C-1201", ["M-0101"])] });
    expect(m.run("copilot", outside()).loads.map((l) => l.compile)).toEqual([
      "C-1201",
    ]);
    block("C-1207", [
      "- Answer in British English. [M-0101]",
      "- Prefer small pull requests. [M-0102]",
    ]);
    warm(null, { targets: [target("C-1207", ["M-0101", "M-0102"])] });
    const next = JSON.parse(m.run("copilot", outside()).output);
    expect(next.additionalContext).toContain(
      "- Prefer small pull requests. [M-0102]",
    );
    expect(next.additionalContext).not.toContain("Use tabs.");
  });

  it("remembers what it told per agent, wherever the session ran", () => {
    linkHome();
    homeFile("claude_user", "C-1201", [
      "- Answer in British English. [M-0101]",
    ]);
    warm(null, { targets: [homeTarget("claude_user", "C-1201", ["M-0101"])] });
    m.run("claude-code", outside());
    homeFile("claude_user", "C-1207", [
      "- Answer in British English. [M-0101]",
      "- Prefer small pull requests. [M-0102]",
    ]);
    warm(null, {
      targets: [homeTarget("claude_user", "C-1207", ["M-0101", "M-0102"])],
    });
    expect(m.run("claude-code", outside()).output).toContain("[M-0102]");
    // In a repository next: the Personal news was told already.
    m.compiled("C-0881", ["- Background jobs run on River. [M-0219]"]);
    expect(m.run("claude-code").output).not.toContain("[M-0102]");
    const seen = JSON.parse(
      readFileSync(join(m.paths.seen, "home", "claude-code.json"), "utf8"),
    );
    expect(Object.keys(seen.files)).toEqual([
      "~/.claude/rules/memax-personal.md",
    ]);
  });
});

describe("Cursor, which loads no file of the Personal space", () => {
  const CANON = ".memax/personal/AGENTS.md";

  /** The canonical copy as the daemon writes it. */
  function canonical(ref: string, sections: Record<string, string[]>): void {
    mkdirSync(join(m.home, ".memax", "personal"), { recursive: true });
    writeFileSync(
      join(m.home, CANON),
      [
        header(ref, "personal"),
        "# Personal",
        "",
        "The person you work with keeps these in Memax. Where a project's own files disagree, follow the project.",
        ...Object.entries(sections).flatMap(([h, lines]) => [
          "",
          `## ${h}`,
          ...lines,
        ]),
        "",
        "## Live context",
        "Use the memax MCP server for anything not here.",
        "",
      ].join("\n"),
    );
  }

  const canonTarget = (
    ref: string,
    refs: string[],
    over: Partial<WarmTarget> = {},
  ): WarmTarget => ({
    ...homeTarget("agents_md", ref, refs),
    path: CANON,
    label: `~/${CANON}`,
    ...over,
  });

  const context = (agent = "cursor", where = outside()) => {
    const res = m.run(agent, where);
    return {
      res,
      text: res.output
        ? (JSON.parse(res.output) as { additional_context: string })
            .additional_context
        : "",
    };
  };

  it("gets its kept lines in full at every session start, under their headings, and a load for the canonical's compile", () => {
    linkHome();
    canonical("C-1204", {
      Preferences: [
        "- Answer in British English. [M-0101]",
        "- Search with `rg`, not `grep`. [M-0111]",
      ],
      Decisions: ["- Use pnpm in every JavaScript project. [M-0106]"],
    });
    warm(null, {
      targets: [canonTarget("C-1204", ["M-0101", "M-0106", "M-0111"])],
    });
    const first = context();
    expect(first.text).toBe(
      [
        '<memax-context space="personal">',
        "Memax: your Personal space (personal). You load no file of it, so its kept lines are here at every session start. Where a project's own files disagree, follow the project.",
        "Preferences:",
        "- Answer in British English. [M-0101]",
        "- Search with `rg`, not `grep`. [M-0111]",
        "Decisions:",
        "- Use pnpm in every JavaScript project. [M-0106]",
        "</memax-context>",
        "",
      ].join("\n"),
    );
    expect(first.res.loads.map((l) => [l.space, l.compile, l.agent])).toEqual([
      [PERSONAL, "C-1204", "cursor"],
    ]);
    // Nothing changed, and it is said again: Cursor holds no other copy.
    expect(context().text).toBe(first.text);
  });

  it("follows the repository's news, which keeps its place in the budget", () => {
    linkHome();
    m.compiled("C-0881", ["- Background jobs run on River. [M-0219]"]);
    canonical("C-1204", {
      Preferences: ["- Answer in British English. [M-0101]"],
    });
    warm(
      { targets: [m.target("C-0881", ["M-0219"])] },
      { targets: [canonTarget("C-1204", ["M-0101"])] },
    );
    const first = context("cursor", { cwd: m.repo });
    // The repository's AGENTS.md is a baseline the first time; Personal isn't.
    expect(first.text).toContain('<memax-context space="personal">');
    expect(first.text).toContain("- Answer in British English. [M-0101]");
    m.compiled("C-0890", [
      "- Background jobs run on River. [M-0219]",
      "- Deploys go out on Tuesdays. [M-0220]",
    ]);
    warm(
      { targets: [m.target("C-0890", ["M-0219", "M-0220"])] },
      { targets: [canonTarget("C-1204", ["M-0101"])] },
    );
    const next = context("cursor", { cwd: m.repo }).text;
    expect(next.indexOf("- Deploys go out on Tuesdays. [M-0220]")).toBeLessThan(
      next.indexOf("Your Personal space (personal). You load no file of it"),
    );
    expect(next).toContain("- Answer in British English. [M-0101]");
  });

  it("never says a forgotten memory's line, nor a cite the server didn't compile", () => {
    linkHome();
    canonical("C-1204", {
      Preferences: [
        "- Answer in British English. [M-0101]",
        "- Mine, added by hand. [M-0900]",
        "- Prefer small pull requests. [M-0102]",
      ],
    });
    warm(null, {
      targets: [canonTarget("C-1204", ["M-0101", "M-0102"])],
      forgotten: [forget("M-0102", new Date())],
    });
    const { text } = context();
    expect(text).toContain("- Answer in British English. [M-0101]");
    expect(text).not.toContain("M-0900");
    expect(text).not.toContain("Prefer small pull requests");
    expect(text).toContain("- M-0102 (");
  });

  it("holds the print to 1,500 tokens, and says the rest stay live over MCP", () => {
    linkHome();
    const lines = Array.from(
      { length: 120 },
      (_, k) => `- Preference ${k}: ${"y".repeat(80)}. [M-${3000 + k}]`,
    );
    canonical("C-1204", { Preferences: lines });
    warm(null, {
      targets: [
        canonTarget(
          "C-1204",
          lines.map((_, k) => `M-${3000 + k}`),
        ),
      ],
    });
    const { text } = context();
    const shown = text.split("\n").filter((l) => l.startsWith("- Preference"));
    expect(shown.length).toBeGreaterThan(10);
    expect(shown.length).toBeLessThan(120);
    expect(estimateTokens(shown.join("\n"))).toBeLessThanOrEqual(1500);
    expect(text).toContain(
      `…and ${120 - shown.length} more lines of your Personal space, not shown here: the rest stay live over MCP (memax_recall).`,
    );
  });

  it("is cut before the repository's lines when the block runs long", () => {
    linkHome();
    const many = (n: number, ref: number, word: string) =>
      Array.from(
        { length: n },
        (_, k) => `- ${word} line ${k} ${"x".repeat(60)}. [M-${ref + k}]`,
      );
    const refs = (n: number, ref: number) =>
      Array.from({ length: n }, (_, k) => `M-${ref + k}`);
    m.compiled("C-0881", []);
    canonical("C-1204", { Preferences: many(60, 2000, "Personal") });
    warm(
      { targets: [m.target("C-0881", [])] },
      { targets: [canonTarget("C-1204", refs(60, 2000))] },
    );
    context("cursor", { cwd: m.repo });
    m.compiled("C-0890", many(60, 1000, "Repository"));
    warm(
      { targets: [m.target("C-0890", refs(60, 1000))] },
      { targets: [canonTarget("C-1204", refs(60, 2000))] },
    );
    const { text } = context("cursor", { cwd: m.repo });
    expect(estimateTokens(text)).toBeLessThanOrEqual(3000);
    expect(text).toContain("Repository line 0");
    expect(text).not.toContain("Personal line 59");
    expect(text).toMatch(
      /…and \d+ more lines of your Personal space, not shown here: the rest stay live over MCP \(memax_recall\)\./,
    );
  });

  it("is cut before the repository's verifying lines, which are never cut", () => {
    linkHome();
    const many = (n: number, ref: number, word: string) =>
      Array.from(
        { length: n },
        (_, k) => `- ${word} line ${k} ${"x".repeat(60)}. [M-${ref + k}]`,
      );
    const refs = (n: number, ref: number) =>
      Array.from({ length: n }, (_, k) => `M-${ref + k}`);
    const stale = [
      {
        ref: "M-0098",
        since: "2026-10-10T03:00:00Z",
        causes: [
          { kind: "date" as const, stale_after: "2026-10-01T00:00:00Z" },
        ],
      },
    ];
    m.compiled("C-0881", []);
    canonical("C-1204", { Preferences: many(60, 2000, "Personal") });
    warm(
      { targets: [m.target("C-0881", [])] },
      { targets: [canonTarget("C-1204", refs(60, 2000))] },
    );
    context("cursor", { cwd: m.repo });
    m.compiled("C-0890", many(60, 1000, "Repository"));
    warm(
      { targets: [m.target("C-0890", refs(60, 1000))], stale },
      { targets: [canonTarget("C-1204", refs(60, 2000))] },
    );
    const { text } = context("cursor", { cwd: m.repo });
    expect(estimateTokens(text)).toBeLessThanOrEqual(3000);
    expect(text).toContain(
      "- M-0098: its date to check again, 2026-10-01, has passed.",
    );
    expect(text).toContain("Repository line 0");
    expect(text).not.toContain("Personal line 59");
    expect(text.indexOf("M-0098: its date")).toBeLessThan(
      text.indexOf("Your Personal space (personal)"),
    );
  });

  it("says a hand edit, and prints nothing from a copy stopped on the web", () => {
    linkHome();
    canonical("C-1204", {
      Preferences: ["- Answer in British English. [M-0101]"],
    });
    warm(null, {
      targets: [
        canonTarget("C-1204", ["M-0101"], {
          sync_state: "drifted",
          open_drift: 1,
        }),
      ],
    });
    expect(context().text).toContain(
      "~/.memax/personal/AGENTS.md has a local edit that waits in Review, so these lines are as the file has them.",
    );
    warm(null, {
      targets: [canonTarget("C-1204", ["M-0101"], { sync_state: "off" })],
    });
    expect(context().res).toMatchObject({ output: "", loads: [] });
  });

  it("is Cursor's alone: the agents with a file of their own never get the print", () => {
    linkHome();
    canonical("C-1204", {
      Preferences: ["- Answer in British English. [M-0101]"],
    });
    warm(null, { targets: [canonTarget("C-1204", ["M-0101"])] });
    for (const agent of ["claude-code", "codex", "gemini", "copilot"])
      expect(m.run(agent, outside()).output).toBe("");
  });
});
