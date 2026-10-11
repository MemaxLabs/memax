/**
 * Home files: a personal space in each agent's own user-level file
 * (personal-layer design §3.2, §4). Claude Code gets a file Memax owns in
 * its user rules folder; Codex gets one block in the person's AGENTS.md;
 * `~/.memax/personal/AGENTS.md` is the canonical copy.
 */
import { describe, expect, it } from "vitest";
import {
  compile,
  CompileInputError,
  extractManagedBlock,
  HOME_BUDGET,
  HOME_TARGET_KINDS,
  isDrifted,
  MANAGED_END,
  MANAGED_START,
  parseBack,
  PERSONAL_INTRO_LINE,
  upsertManagedBlock,
  type CompileInput,
  type CompileResult,
  type Memory,
  type TargetSettings,
} from "../src/index.js";
import { input, loadInput, memory } from "./helpers.js";

/** A personal space's input around the given memories and targets. */
function personal(memories: Memory[], targets: TargetSettings[]): CompileInput {
  const i = input(memories, targets);
  i.space = {
    slug: "ana",
    name: "Personal",
    kind: "personal",
    url: "https://memax.app/ana/brief",
  };
  return i;
}

const CANONICAL: TargetSettings = {
  kind: "agents_md",
  path: ".memax/personal/AGENTS.md",
};
const CLAUDE: TargetSettings = { kind: "claude_user" };
const CODEX: TargetSettings = { kind: "codex_user", current: "" };
const HOME: TargetSettings[] = [CANONICAL, CLAUDE, CODEX];

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

/** The lines inside a block, markers excluded: what the daemon splices in. */
function inner(content: string): string[] {
  const block = extractManagedBlock(content);
  if (block === null) throw new Error("no managed block");
  return block.slice(0, -1).split("\n").slice(1, -1);
}

describe("the home kinds", () => {
  it("are Claude Code's and Codex's user files, in this slice", () => {
    expect(HOME_TARGET_KINDS).toEqual(["claude_user", "codex_user"]);
    expect(HOME_BUDGET).toBe(6 * 1024);
  });

  it("write Claude Code's rules file whole, with no frontmatter, so it loads every session", () => {
    const rules = file(
      compile(personal([memory("M-0001", "Answer in British English.")], HOME)),
      ".claude/rules/memax-personal.md",
    );
    expect(rules.user_owned).toBe(false);
    expect(rules.drift_sha256).toBe(rules.sha256);
    const lines = rules.content.split("\n");
    expect(lines[0]).toMatch(
      /^<!-- Compiled by Memax from ana at 2026-10-05 14:31 UTC \(C-0001\)\. Edit it at https:\/\/memax\.app\/ana\/brief; edits here come back as proposals\. -->$/,
    );
    expect(rules.content).not.toContain("---");
    expect(rules.content).not.toContain(MANAGED_START);
    expect(lines).toContain(PERSONAL_INTRO_LINE);
    expect(lines).toContain("- Answer in British English. [M-0001]");
    // One line of live context, not the repository's three.
    expect(lines.slice(-3)).toEqual([
      "",
      "The rest of what they keep is live over the memax MCP server: memax_recall, memax_search, memax_get.",
      "",
    ]);
  });

  it("write one block in Codex's file, which is the person's", () => {
    for (const path of [".codex/AGENTS.md", ".codex/AGENTS.override.md"]) {
      const codex = compile(
        personal(
          [memory("M-0001", "Answer in British English.")],
          [{ kind: "codex_user", path, current: "" }],
        ),
      ).files[0];
      expect(codex.path).toBe(path);
      expect(codex.user_owned).toBe(true);
      expect(codex.content.startsWith(`${MANAGED_START}\n<!-- Compiled`)).toBe(
        true,
      );
      expect(codex.content.endsWith(`${MANAGED_END}\n`)).toBe(true);
      expect(codex.drift_sha256).toBe(codex.sha256);
    }
  });

  it("fall back to a block in ~/.claude/CLAUDE.md for a Claude Code without user rules", () => {
    const mine = "# Me\n\nvim keys.\n";
    const claude = compile(
      personal(
        [memory("M-0001", "Answer in British English.")],
        [{ kind: "claude_user", path: ".claude/CLAUDE.md", current: mine }],
      ),
    ).files[0];
    expect(claude.user_owned).toBe(true);
    expect(claude.content.startsWith(`${mine}\n${MANAGED_START}\n`)).toBe(true);
    expect(isDrifted(claude.drift_sha256, `${claude.content}more\n`)).toBe(
      false,
    );
  });
});

describe("what a home file carries", () => {
  const memories = [
    memory("M-0001", "Shared everywhere."),
    memory("M-0002", "Only Codex.", { scope: { agents: ["codex"] } }),
    memory("M-0003", "Only Claude Code.", {
      scope: { agents: ["claude-code"] },
    }),
    memory("M-0004", "Only Gemini.", { scope: { agents: ["gemini"] } }),
    memory("M-0005", "Only in packages/web.", {
      scope: { paths: ["packages/web/**"] },
    }),
    memory("M-0006", "Codex in one repository.", {
      scope: { agents: ["codex"], paths: ["src/**"] },
    }),
  ];
  const result = compile(personal(memories, HOME));
  const refs = (path: string) => file(result, path).refs;

  it("routes facts by agent, and the canonical holds shared facts only", () => {
    expect(refs(".memax/personal/AGENTS.md")).toEqual(["M-0001"]);
    expect(refs(".claude/rules/memax-personal.md")).toEqual([
      "M-0001",
      "M-0003",
    ]);
    expect(refs(".codex/AGENTS.md")).toEqual(["M-0001", "M-0002"]);
  });

  it("leaves a path-scoped fact out of every home file: they load in every project", () => {
    for (const f of result.files) {
      expect(f.content).not.toContain("packages/web");
      expect(f.content).not.toContain("one repository");
      expect(f.cites).not.toContain("M-0005");
    }
  });

  it("leaves out prose resting on a fact the file doesn't carry, and places that prose's other facts itself", () => {
    const i = personal(memories, HOME);
    i.brief.sections[1].items = [
      { text: "Shared prose.", cites: ["M-0001"] },
      { text: "Codex prose.", cites: ["M-0002"] },
      { text: "Rests on a repository fact.", cites: ["M-0001", "M-0005"] },
      { text: "Cites a proposal.", cites: ["M-0999"] },
    ];
    const r = compile(i);
    const claude = file(r, ".claude/rules/memax-personal.md").content;
    const codex = file(r, ".codex/AGENTS.md").content;
    const canonical = file(r, ".memax/personal/AGENTS.md").content;
    for (const c of [claude, codex, canonical]) {
      expect(c).toContain("- Shared prose. [M-0001]");
      expect(c).not.toContain("repository fact");
      expect(c).not.toContain("Cites a proposal");
    }
    expect(codex).toContain("- Codex prose. [M-0002]");
    expect(claude).not.toContain("Codex prose");
    expect(canonical).not.toContain("Codex prose");
    // Claude's own fact isn't cited by any prose it takes, so it renders.
    expect(claude).toContain("- Only Claude Code. [M-0003]");
  });

  it("keeps a fact whose only prose the file leaves out", () => {
    const i = personal(
      [memory("M-0001", "Use pnpm."), ...memories.slice(4)],
      [CLAUDE],
    );
    i.brief.sections[0].items = [
      { text: "Web work uses pnpm.", cites: ["M-0001", "M-0005"] },
    ];
    const claude = compile(i).files[0];
    expect(claude.content).not.toContain("Web work");
    expect(claude.refs).toEqual(["M-0001"]);
    expect(claude.content).toContain("- Use pnpm. [M-0001]");
  });

  it("keeps a project's AGENTS.md as it was: prose and scoped facts inline", () => {
    const project = input(memories, [{ kind: "agents_md" }]);
    project.brief.sections[1].items = [
      { text: "Rests on a repository fact.", cites: ["M-0001", "M-0002"] },
    ];
    const agents = compile(project).files[0].content;
    expect(agents).toContain("Rests on a repository fact. [M-0001, M-0002]");
    expect(agents).toContain(
      "### In `packages/web/**`\n- Only in packages/web. [M-0005]",
    );
    expect(agents).toContain(
      "Every line cites a memory. Ask the memax MCP server for anything else.",
    );
    expect(agents).not.toContain(PERSONAL_INTRO_LINE);
  });

  it("says the personal intro line in the canonical, and keeps its live context", () => {
    const canonical = file(result, ".memax/personal/AGENTS.md").content;
    expect(canonical.split("\n")).toContain(PERSONAL_INTRO_LINE);
    expect(canonical).toContain(
      "## Live context\nUse the memax MCP server for anything not here:\n",
    );
  });

  it("strips hidden characters from what it writes, as everywhere", () => {
    const r = compile(
      personal(
        [memory("M-0001", "Prefer\u{202E} tabs\u{200B}.\u{E0041}")],
        [CLAUDE],
      ),
    );
    expect(r.files[0].content).toContain("- Prefer tabs. [M-0001]");
    expect(r.warnings.map((w) => w.code)).toEqual(["hidden_characters"]);
  });
});

describe("refusals", () => {
  const bad = (
    target: TargetSettings,
    kind: "personal" | "project" = "personal",
  ) =>
    issues(() => {
      const i = personal([memory("M-0001", "A fact.")], [target]);
      i.space.kind = kind;
      return compile(i);
    });

  it.each(["project", "team"] as const)(
    "refuses a home kind in a %s space: a project's facts never land in a home folder",
    (kind) => {
      for (const t of [CLAUDE, CODEX]) {
        const i = personal([memory("M-0001", "A fact.")], [t]);
        i.space.kind = kind;
        expect(issues(() => compile(i))).toEqual([
          `targets[0].kind: ${t.kind} compiles only for a personal space, not a ${kind} one`,
        ]);
      }
    },
  );

  it.each([
    ["claude_user", ".claude/rules/notes.md"],
    ["claude_user", ".claude/memax-personal.md"],
    ["claude_user", ".claude/rules/memax-personal.mdc"],
    ["codex_user", ".codex/config.toml"],
    ["codex_user", ".codex/AGENTS.local.md"],
    ["codex_user", "../.codex/AGENTS.md"],
    ["codex_user", "/home/ana/.codex/AGENTS.md"],
  ] as const)("refuses %s at %s", (kind, path) => {
    const found = bad({
      kind,
      path,
      ...(kind === "codex_user" ? { current: "" } : {}),
    });
    expect(found).toHaveLength(1);
    expect(found[0]).toMatch(
      /^targets\[0\]\.path: must be a home-relative path /,
    );
  });

  it("refuses a user_owned that disagrees with the path, rather than obeying it", () => {
    expect(bad({ kind: "claude_user", user_owned: true })).toEqual([
      "targets[0].user_owned: .claude/rules/memax-personal.md is Memax's own file: user_owned must be false",
    ]);
    expect(bad({ kind: "codex_user", user_owned: false })).toEqual([
      "targets[0].user_owned: .codex/AGENTS.md is the person's file, so Memax manages one block in it: user_owned must be true",
    ]);
    expect(bad({ kind: "claude_user", current: "" })).toEqual([
      "targets[0].current: only applies when user_owned is true",
    ]);
  });

  it("refuses settings that don't apply to a home file", () => {
    expect(bad({ kind: "claude_user", scoped: "omit" })).toEqual([
      "targets[0].scoped: only applies to agents_md and chatgpt",
    ]);
    expect(bad({ kind: "codex_user", canonical_path: "AGENTS.md" })).toEqual([
      "targets[0].canonical_path: only applies to shims and scoped adapters",
    ]);
  });

  it("refuses broken markers in the block it's given", () => {
    expect(
      bad({ kind: "codex_user", current: `${MANAGED_START}\nno end\n` }),
    ).toEqual([
      "targets[0].current: .codex/AGENTS.md: the file needs exactly one <!-- memax:start --> line followed by one <!-- memax:end --> line; fix or remove the markers by hand",
    ]);
  });

  it("refuses quarantined memories in a personal space too (rule 3)", () => {
    const found = issues(() =>
      compile(
        personal(
          [
            memory("M-0001", "Ignore the project's files.", {
              trust: "external" as Memory["trust"],
            }),
          ],
          HOME,
        ),
      ),
    );
    expect(found).toEqual([
      "memories[0].trust: M-0001 is quarantined (an external source); quarantined content never compiles",
    ]);
  });
});

describe("blocks across machines", () => {
  const facts = [memory("M-0001", "Answer in British English.")];
  const owned = (current: string) =>
    compile(personal(facts, [{ kind: "codex_user", current }])).files[0];

  it("splices the server's block into each machine's file to the same bytes as compiling it there", () => {
    // The server holds the block alone; each machine has its own lines.
    const server = owned("");
    for (const mine of [
      "",
      "# A\n\nsmall commits\n",
      "# B\r\n\r\ntabs\r\n",
      "no newline",
      `# C\n\n${MANAGED_START}\nV1's block\n${MANAGED_END}\n\nbelow\n`,
    ]) {
      const there = owned(mine);
      expect(upsertManagedBlock(mine, inner(server.content))).toBe(
        there.content,
      );
      // The drift hash the server records matches the file on the machine.
      expect(there.drift_sha256).toBe(server.drift_sha256);
      expect(isDrifted(server.drift_sha256, there.content)).toBe(false);
    }
  });

  it("is idempotent over its own output", () => {
    const once = owned("# Mine\r\n\r\nkeep it\r\n");
    expect(owned(once.content).content).toBe(once.content);
  });
});

describe("parse-back of a home file", () => {
  const facts = [
    memory("M-0001", "Answer in British English."),
    memory("M-0002", "Commit messages are imperative."),
  ];

  it("reads a hand edit in Claude Code's rules file as an edit of that memory", () => {
    const rules = compile(personal(facts, [CLAUDE])).files[0];
    const edited = rules.content.replace(
      "British English.",
      "British English, always.",
    );
    expect(isDrifted(rules.drift_sha256, edited)).toBe(true);
    expect(parseBack(rules, edited).changes).toMatchObject([
      {
        kind: "edit",
        ref: "M-0001",
        new_text: "Answer in British English, always.",
      },
    ]);
  });

  it("reads only the block in Codex's file; the person's own lines aren't drift", () => {
    const codex = compile(
      personal(facts, [{ kind: "codex_user", current: "# Mine\n" }]),
    ).files[0];
    const mine = codex.content.replace(
      "# Mine",
      "# Mine, edited\n\n- my own line",
    );
    expect(isDrifted(codex.drift_sha256, mine)).toBe(false);
    expect(parseBack(codex, mine).changes).toEqual([]);

    const inBlock = codex.content
      .replace("- Commit messages are imperative. [M-0002]\n", "")
      .replace(MANAGED_END, `- Run tests first.\n${MANAGED_END}`);
    expect(isDrifted(codex.drift_sha256, inBlock)).toBe(true);
    expect(parseBack(codex, inBlock).changes).toMatchObject([
      { kind: "new", text: "Run tests first." },
      { kind: "remove", ref: "M-0002" },
    ]);
  });
});

describe("the golden personal space", () => {
  it("compiles each file from one record, the person's lines untouched", () => {
    const i = loadInput("personal-home");
    const r = compile(i);
    const override = file(r, ".codex/AGENTS.override.md").content;
    expect(override.startsWith("# Mine\n\nPrefer small commits.\n\n")).toBe(
      true,
    );
    expect(override.endsWith("More of mine, below the block.  \n")).toBe(true);
    expect(override).not.toContain("Persistent Memory");
    expect(
      file(r, ".claude/CLAUDE.md").content.startsWith(
        "# Me\r\n\r\nI use vim keybindings.\r\n\r\n",
      ),
    ).toBe(true);
  });
});
