// The home link's two tidy-ups (design §6.3): lines that now load twice,
// and V1's instruction block. Exact matches only, a copy first, and a file
// that changed meanwhile left alone.
import {
  mkdirSync,
  mkdtempSync,
  readdirSync,
  readFileSync,
  realpathSync,
  rmSync,
  statSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { MEMAX_INSTRUCTION_BLOCK } from "../../src/commands/setup-instructions.js";
import type { HomeLink } from "../../src/lib/daemon/home-link.js";
import {
  applyTidy,
  findTwice,
  findV1,
  tidyQuestion,
} from "../../src/lib/home/tidy.js";

let base: string;
let home: string;
beforeEach(() => {
  base = realpathSync(mkdtempSync(join(tmpdir(), "memax-tidy-")));
  home = join(base, "home");
  mkdirSync(home);
});
afterEach(() => rmSync(base, { recursive: true, force: true }));

const put = (rel: string, content: string) => {
  mkdirSync(dirname(join(home, rel)), { recursive: true });
  writeFileSync(join(home, rel), content);
};
const read = (rel: string) => readFileSync(join(home, rel), "utf8");

function link(...files: Array<[string, string]>): HomeLink {
  return {
    version: 1,
    space_id: "s",
    space_slug: "personal",
    machine_id: "m",
    home,
    created_at: "",
    files: files.map(([kind, path], i) => ({
      target_id: `t${i}`,
      kind: kind as HomeLink["files"][number]["kind"],
      path,
      abs: join(home, path),
    })),
  };
}

const RULES = [
  "<!-- Compiled by Memax from personal at 2026-10-11 09:00 UTC (C-0001). -->",
  "# Personal",
  "",
  "## Preferences",
  "- I prefer short answers. [M-0001]",
  "- Search with `rg`, not `grep`. [M-0002]",
  "- (Being verified) Tabs in Go. [M-0003]",
  "",
].join("\n");

describe("lines that load twice", () => {
  it("takes out exactly the person's lines Memax compiles into Claude Code's rules file", () => {
    put(".claude/rules/memax-personal.md", RULES);
    const mine = [
      "# Me",
      "",
      "- I prefer short answers.",
      "- I prefer short answers, mostly.",
      "* Search with `rg`, not `grep`.",
      "- Tabs in Go.",
      "- A list item that runs",
      "  onto a second line.",
      "Keep this.",
    ].join("\r\n");
    put(".claude/CLAUDE.md", mine);
    const [t] = findTwice(
      link(["claude_user", ".claude/rules/memax-personal.md"]),
    );
    expect(t).toMatchObject({
      kind: "twice",
      label: "~/.claude/CLAUDE.md",
      lines: 2,
    });
    expect(t.after).toBe(
      [
        "# Me",
        "",
        "- I prefer short answers, mostly.",
        "- Tabs in Go.",
        "- A list item that runs",
        "  onto a second line.",
        "Keep this.",
      ].join("\r\n"),
    );
    expect(tidyQuestion(t, "personal")).toBe(
      "  2 lines in ~/.claude/CLAUDE.md are kept in personal now and load twice. Take them out? A copy goes to ~/.memax/backup/. [y/N] ",
    );
  });

  it("reads Codex's file around Memax's block, and never touches the block", () => {
    const block =
      "<!-- memax:start -->\n- I prefer short answers. [M-0001]\n<!-- memax:end -->\n";
    put(
      ".codex/AGENTS.md",
      `# Mine\n- I prefer short answers.\n- Run tests first.\n${block}`,
    );
    const [t] = findTwice(link(["codex_user", ".codex/AGENTS.md"]));
    expect(t.lines).toBe(1);
    expect(t.after).toBe(`# Mine\n- Run tests first.\n${block}`);
  });

  it("reads Gemini's AGENTS.md around Memax's block, and GEMINI.md beside it, which Antigravity reads too", () => {
    const block =
      "<!-- memax:start -->\n- I prefer short answers. [M-0001]\n<!-- memax:end -->\n";
    put(".gemini/AGENTS.md", `- I prefer short answers.\n${block}`);
    put(
      ".gemini/GEMINI.md",
      "## Gemini Added Memories\n- I prefer short answers.\n- Tabs in Go.\n",
    );
    const found = findTwice(link(["gemini_user", ".gemini/AGENTS.md"]));
    expect(found.map((t) => [t.label, t.lines, t.after])).toEqual([
      ["~/.gemini/AGENTS.md", 1, block],
      ["~/.gemini/GEMINI.md", 1, "## Gemini Added Memories\n- Tabs in Go.\n"],
    ]);
  });

  it("finds nothing without Memax's file, or when nothing matches", () => {
    put(".claude/CLAUDE.md", "- I prefer short answers.\n");
    expect(
      findTwice(link(["claude_user", ".claude/rules/memax-personal.md"])),
    ).toEqual([]);
    put(".claude/rules/memax-personal.md", RULES);
    put(".claude/CLAUDE.md", "- Something else.\n");
    expect(
      findTwice(link(["claude_user", ".claude/rules/memax-personal.md"])),
    ).toEqual([]);
  });
});

describe("V1's instruction block", () => {
  it("is offered out of ~/.claude/CLAUDE.md and ~/.gemini/GEMINI.md when it is exactly V1's", () => {
    // As V1's memax setup --instructions wrote it.
    put(".claude/CLAUDE.md", `# Me\n- tabs\n\n${MEMAX_INSTRUCTION_BLOCK}\n`);
    put(".gemini/GEMINI.md", `${MEMAX_INSTRUCTION_BLOCK}\n`);
    const found = findV1(home, {});
    expect(found.map((t) => [t.label, t.after])).toEqual([
      ["~/.claude/CLAUDE.md", "# Me\n- tabs\n"],
      ["~/.gemini/GEMINI.md", ""],
    ]);
    expect(tidyQuestion(found[0], "personal")).toContain(
      "~/.claude/CLAUDE.md still has Memax V1's instructions",
    );
  });

  it("leaves a block that isn't exactly V1's", () => {
    put(
      ".claude/CLAUDE.md",
      `# Me\n\n${MEMAX_INSTRUCTION_BLOCK.replace("Use it proactively", "Use it")}\n`,
    );
    expect(findV1(home, {})).toEqual([]);
  });
});

describe("applying one", () => {
  it("keeps a copy first, readable by you only, then writes", async () => {
    put(".claude/CLAUDE.md", `# Me\n\n${MEMAX_INSTRUCTION_BLOCK}\n`);
    const [t] = findV1(home, {});
    expect(await applyTidy(t, home, new Date("2026-10-11T09:00:00Z"))).toBe(
      "done",
    );
    expect(read(".claude/CLAUDE.md")).toBe("# Me\n");
    const dir = join(home, ".memax", "backup");
    expect(statSync(dir).mode & 0o777).toBe(0o700);
    const [copy] = readdirSync(dir);
    expect(copy).toBe("claude-CLAUDE.md.2026-10-11T09-00-00-000Z");
    expect(statSync(join(dir, copy)).mode & 0o777).toBe(0o600);
    expect(readFileSync(join(dir, copy), "utf8")).toBe(t.before);
  });

  it("leaves a file that changed since it was read", async () => {
    put(".claude/CLAUDE.md", `# Me\n\n${MEMAX_INSTRUCTION_BLOCK}\n`);
    const [t] = findV1(home, {});
    put(".claude/CLAUDE.md", `# Me, edited\n\n${MEMAX_INSTRUCTION_BLOCK}\n`);
    expect(await applyTidy(t, home)).toBe("changed");
    expect(read(".claude/CLAUDE.md")).toContain("# Me, edited");
  });
});
