// Which home files memax link --home offers, resolved in the person's
// shell, what it refuses and why, and the consent it asks (design §6.1).
import {
  mkdirSync,
  mkdtempSync,
  realpathSync,
  rmSync,
  symlinkSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import {
  agentFolder,
  homeFileRefusal,
  validHomePath,
} from "../../src/lib/daemon/home-place.js";
import {
  CONSENT_QUESTION,
  consentLines,
  COPILOT_BESIDE_CLAUDE,
  planHomeFiles,
} from "../../src/lib/home/files.js";

let base: string;
let home: string;
beforeEach(() => {
  base = realpathSync(mkdtempSync(join(tmpdir(), "memax-home-")));
  home = join(base, "home");
  mkdirSync(home);
});
afterEach(() => rmSync(base, { recursive: true, force: true }));

const dir = (...p: string[]) =>
  mkdirSync(join(home, ...p), { recursive: true });
const plan = (env: Record<string, string> = {}) =>
  planHomeFiles({ home, env }).map((f) => ({
    kind: f.kind,
    path: f.path,
    refusal: f.refusal,
  }));

describe("the files a home link offers", () => {
  it("is Claude Code's rules file, Codex's AGENTS.md and the canonical copy", () => {
    dir(".claude");
    dir(".codex");
    expect(plan()).toEqual([
      {
        kind: "claude_user",
        path: ".claude/rules/memax-personal.md",
        refusal: null,
      },
      { kind: "codex_user", path: ".codex/AGENTS.md", refusal: null },
      { kind: "agents_md", path: ".memax/personal/AGENTS.md", refusal: null },
    ]);
  });

  it("offers Gemini's ~/.gemini/AGENTS.md only where something reads it", () => {
    const gemini = (env: Record<string, string> = {}) =>
      planHomeFiles({ home, env }, ["gemini_user"])
        .filter((f) => f.kind === "gemini_user")
        .map((f) => ({ path: f.path, agent: f.agent, readBy: f.readBy }));
    dir(".gemini");
    // Gemini CLI alone reads GEMINI.md: a file there would be read by no one.
    expect(gemini()).toEqual([]);
    expect(plan()).toEqual([]);
    // Antigravity reads it in every session; still unconfirmed by a real
    // run, so memax link --home's own consent leaves it out.
    dir(".gemini", "antigravity-cli");
    expect(plan()).toEqual([]);
    expect(gemini()).toEqual([
      {
        path: ".gemini/AGENTS.md",
        agent: "Antigravity",
        readBy: ["antigravity"],
      },
    ]);
    // Gemini CLI too, once its context.fileName lists AGENTS.md.
    writeFileSync(
      join(home, ".gemini", "settings.json"),
      '{\n  // mine\n  "context": { "fileName": ["GEMINI.md", "AGENTS.md"] }\n}\n',
    );
    expect(gemini()).toEqual([
      {
        path: ".gemini/AGENTS.md",
        agent: "Antigravity and Gemini CLI",
        readBy: ["antigravity", "gemini-cli"],
      },
    ]);
    // GEMINI_CLI_HOME moves the folder that holds .gemini.
    dir(".config", "gemini", ".gemini", "antigravity-cli");
    const moved = planHomeFiles(
      { home, env: { GEMINI_CLI_HOME: join(home, ".config", "gemini") } },
      ["gemini_user"],
    ).find((f) => f.kind === "gemini_user");
    expect(moved).toMatchObject({
      path: ".config/gemini/.gemini/AGENTS.md",
      refusal: null,
    });
  });

  it("offers Copilot's copilot-instructions.md, under COPILOT_HOME too", () => {
    dir(".copilot");
    expect(plan()).toEqual([
      {
        kind: "copilot_user",
        path: ".copilot/copilot-instructions.md",
        refusal: null,
      },
      { kind: "agents_md", path: ".memax/personal/AGENTS.md", refusal: null },
    ]);
    dir(".config", "copilot");
    const moved = planHomeFiles({
      home,
      env: { COPILOT_HOME: join(home, ".config", "copilot") },
    }).find((f) => f.kind === "copilot_user");
    expect(moved).toMatchObject({
      path: ".config/copilot/copilot-instructions.md",
      block: true,
      readBy: ["copilot"],
      refusal: null,
    });
  });

  it("writes Copilot's file beside Claude Code's, and says VS Code's Local agent loads it twice", () => {
    dir(".copilot");
    const alone = planHomeFiles({ home, env: {} });
    expect(alone.every((f) => f.note === undefined)).toBe(true);
    dir(".claude");
    const both = planHomeFiles({ home, env: {} });
    expect(both.map((f) => [f.kind, f.note])).toEqual([
      ["claude_user", COPILOT_BESIDE_CLAUDE],
      ["copilot_user", COPILOT_BESIDE_CLAUDE],
      ["agents_md", undefined],
    ]);
    const consent = consentLines(home, both).join("\n");
    expect(consent.split("VS Code's Local agent")).toHaveLength(2);
    expect(consent).toContain("~/.copilot/copilot-instructions.md");
  });

  it("offers Windsurf's global rules only when asked, while no real run has confirmed them", () => {
    dir(".codeium", "windsurf");
    expect(plan()).toEqual([]);
    expect(
      planHomeFiles({ home, env: {} }, ["windsurf_user"]).map((f) => [
        f.kind,
        f.path,
        f.block,
        f.readBy,
        f.refusal,
      ]),
    ).toEqual([
      [
        "windsurf_user",
        ".codeium/windsurf/memories/global_rules.md",
        true,
        ["windsurf"],
        null,
      ],
      ["agents_md", ".memax/personal/AGENTS.md", false, [], null],
    ]);
  });

  it("offers nothing on a machine with neither agent", () => {
    dir(".cursor");
    expect(plan()).toEqual([]);
  });

  it("follows CLAUDE_CONFIG_DIR and CODEX_HOME, which the daemon won't have", () => {
    dir(".config", "claude");
    dir(".config", "codex");
    expect(
      plan({
        CLAUDE_CONFIG_DIR: join(home, ".config", "claude"),
        CODEX_HOME: "~/.config/codex",
      }).map((f) => f.path),
    ).toEqual([
      ".config/claude/rules/memax-personal.md",
      ".config/codex/AGENTS.md",
      ".memax/personal/AGENTS.md",
    ]);
  });

  it("uses Codex's AGENTS.override.md when it is there and not empty, as Codex does", () => {
    dir(".codex");
    writeFileSync(join(home, ".codex", "AGENTS.override.md"), "");
    expect(plan()[0].path).toBe(".codex/AGENTS.md");
    writeFileSync(join(home, ".codex", "AGENTS.override.md"), "# mine\n");
    expect(plan()[0].path).toBe(".codex/AGENTS.override.md");
  });

  it("refuses a folder outside home, one that isn't a dot folder, a symlink and a dotfiles repository", () => {
    const outside = join(base, "opt-claude");
    mkdirSync(outside);
    expect(plan({ CLAUDE_CONFIG_DIR: outside })[0].refusal).toBe(
      `${join(outside, "rules", "memax-personal.md")} is outside your home folder, so Memax won't write it`,
    );
    dir("claude-config");
    expect(
      plan({ CLAUDE_CONFIG_DIR: join(home, "claude-config") })[0].refusal,
    ).toBe(
      "~/claude-config isn't a dot folder of your home folder; Memax writes home files only in folders like ~/.claude",
    );
    // ~/.claude linked into a dotfiles repository (stow, chezmoi and the like).
    dir("dotfiles", ".git");
    dir("dotfiles", "claude");
    symlinkSync(join(home, "dotfiles", "claude"), join(home, ".claude"));
    expect(plan()[0].refusal).toBe(
      "~/.claude is in ~/dotfiles, a git repository. Your Personal space would be committed with it",
    );
    // A symlink that isn't into a repository.
    dir("elsewhere");
    symlinkSync(join(home, "elsewhere"), join(home, ".codex"));
    expect(plan()[1].refusal).toBe(
      "~/.codex is a symlink; Memax doesn't write through links",
    );
  });

  it("refuses everything when the home folder itself is a git work tree", () => {
    dir(".git");
    dir(".claude");
    for (const f of plan()) expect(f.refusal).toContain("a git repository");
  });

  it("holds the server's home path rules (ledger checkHomePath)", () => {
    for (const [kind, path, ok] of [
      ["claude_user", ".claude/rules/memax-personal.md", true],
      ["claude_user", ".claude/CLAUDE.md", true],
      ["claude_user", ".claude/rules/notes.md", false],
      ["claude_user", ".memax/rules/memax-personal.md", false],
      ["codex_user", ".codex/AGENTS.md", true],
      ["codex_user", ".codex/AGENTS.override.md", true],
      ["codex_user", ".codex/config.toml", false],
      ["codex_user", "codex/AGENTS.md", false],
      ["agents_md", ".memax/personal/AGENTS.md", true],
      ["agents_md", ".memax/AGENTS.md", false],
      ["gemini_user", ".gemini/AGENTS.md", true],
      ["gemini_user", ".config/g/.gemini/AGENTS.md", true],
      ["gemini_user", ".gemini/GEMINI.md", false],
      ["gemini_user", ".gemini/config/AGENTS.md", false],
      ["gemini_user", ".memax/.gemini/AGENTS.md", false],
      ["copilot_user", ".copilot/copilot-instructions.md", true],
      ["copilot_user", ".config/copilot/copilot-instructions.md", true],
      ["copilot_user", ".github/copilot-instructions.md", false],
      ["copilot_user", ".copilot/instructions/memax.instructions.md", false],
      ["windsurf_user", ".codeium/windsurf/memories/global_rules.md", true],
      ["windsurf_user", ".codeium/windsurf/global_rules.md", false],
      ["cursor_mdc", ".cursor/rules/memax-x.mdc", false],
    ] as const)
      expect(validHomePath(kind, path), `${kind} ${path}`).toBe(ok);
    expect(agentFolder("codex_user", ".config/codex/AGENTS.md")).toBe(
      ".config/codex",
    );
    expect(agentFolder("agents_md", ".memax/personal/AGENTS.md")).toBeNull();
    expect(homeFileRefusal(home, "codex_user", ".codex/AGENTS.md")).toBe(
      "Codex isn't set up here any more: ~/.codex is gone",
    );
  });
});

describe("the consent", () => {
  it("names every file and what happens to it, as the design writes it", () => {
    dir(".claude");
    dir(".codex");
    writeFileSync(
      join(home, ".codex", "AGENTS.md"),
      "# Mine\n\n- one\n- two\n- three\n- four\n",
    );
    const lines = consentLines(home, planHomeFiles({ home, env: {} }));
    expect([...lines, CONSENT_QUESTION].join("\n")).toBe(
      [
        "",
        "  Write your Personal space into your agents' own files on this machine?",
        "",
        "    ~/.claude/rules/memax-personal.md   a new file, Memax's",
        "    ~/.codex/AGENTS.md                  one Memax block; your 6 lines stay as they are",
        "    ~/.memax/personal/AGENTS.md         a new file, Memax's",
        "",
        "  Every agent on this machine reads these in every project. Anyone who uses",
        "  this account on this computer can read them too. Hand edits come back as",
        "  proposals, and memax unlink --home takes them off. [Y/n] ",
      ].join("\n"),
    );
  });

  it("says when a file is already Memax's, has V1's block, or isn't there yet", () => {
    dir(".claude", "rules");
    dir(".codex");
    writeFileSync(
      join(home, ".claude", "rules", "memax-personal.md"),
      "<!-- Compiled by Memax from personal (C-0001). -->\n",
    );
    writeFileSync(
      join(home, ".codex", "AGENTS.md"),
      "mine\n<!-- memax:start -->\nV1's guidance\n<!-- memax:end -->\n",
    );
    const text = consentLines(home, planHomeFiles({ home, env: {} })).join(
      "\n",
    );
    expect(text).toContain(
      "~/.claude/rules/memax-personal.md   Memax's, already here",
    );
    expect(text).toContain(
      "one Memax block, in place of the one there; your 1 line stays as it is",
    );
    rmSync(join(home, ".codex", "AGENTS.md"));
    expect(
      consentLines(home, planHomeFiles({ home, env: {} })).join("\n"),
    ).toContain("a new file, with one Memax block");
  });
});
