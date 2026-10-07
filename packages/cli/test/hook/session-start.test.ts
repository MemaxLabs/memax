// memax hook session-start, in-process: what it prints for each case
// (nothing new, a new compile, forgets, gates, a compile not on disk),
// the agents' formats, the token cap, and the loads it queues.
import { readFileSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { daemonPaths } from "../../src/lib/daemon/paths.js";
import { parseCompiled } from "../../src/lib/hook/compiled.js";
import { estimateTokens, MAX_CHARS } from "../../src/lib/hook/render.js";
import { hookPaths } from "../../src/lib/hook/session-start.js";
import { claimSession } from "../../src/lib/hook/seen.js";
import { forget, gate, machine, SPACE_ID, type Machine } from "./fixture.js";

let m: Machine;
beforeEach(() => {
  m = machine();
});
afterEach(() => m.cleanup());

const DAY = 86_400_000;
const lines881 = [
  "- Background jobs run on River. We do not use Temporal. [M-0219]",
  "- API errors are RFC 9457 problem+json. [M-0098]",
  "- Remote MCP is stateless streamable HTTP. [M-0102]",
];
const lines882 = [
  "- Background jobs run on River, Postgres-backed. We do not use Temporal. [M-0219]",
  "- API errors are RFC 9457 problem+json. [M-0098]",
  "- pnpm workspaces only. Never run `npm install` at the root. [M-0071]",
];

describe("nothing changed", () => {
  it("prints nothing in an agent's first session here, nor in the next", () => {
    m.compiled("C-0881", lines881);
    m.warm({ targets: [m.target("C-0881", ["M-0219", "M-0098", "M-0102"])] });
    const first = m.run();
    expect(first.output).toBe("");
    expect(first.skipped).toBe("first session");
    const second = m.run();
    expect(second.output).toBe("");
    expect(second.skipped).toBe("no news");
  });

  it("prints nothing outside a repository, or where nothing is compiled", () => {
    expect(m.run("claude-code", { cwd: m.home }).skipped).toBe(
      "not in a repository",
    );
    const bare = machine({ linked: false });
    try {
      expect(bare.run().skipped).toBe("no compiled files here");
    } finally {
      bare.cleanup();
    }
  });

  it("never prints the compiled file itself", () => {
    m.compiled("C-0881", lines881);
    m.run();
    m.compiled("C-0882", lines882);
    const out = m.run().output;
    expect(out).not.toContain("# Acme web brief");
    expect(out).not.toContain("problem+json"); // unchanged lines stay unsaid
  });
});

describe("a new compile", () => {
  it("prints the lines added and changed, and the memories gone", () => {
    m.compiled("C-0881", lines881);
    m.run();
    m.compiled("C-0882", lines882);
    const out = m.run().output;
    expect(out).toBe(
      [
        '<memax-context space="acme-web">',
        "Memax: what changed in this repository's compiled context since your last session here. The files themselves are already loaded.",
        "AGENTS.md is now C-0882 (your last session saw C-0881).",
        "New:",
        "- pnpm workspaces only. Never run `npm install` at the root. [M-0071]",
        "Changed:",
        "- Background jobs run on River, Postgres-backed. We do not use Temporal. [M-0219]",
        "No longer in AGENTS.md: M-0102 (superseded or excluded; don't rely on them).",
        "</memax-context>",
        "",
      ].join("\n"),
    );
    expect(m.run().output).toBe(""); // told once
  });

  it("leaves out lines the server didn't compile, and forgotten ones", () => {
    m.compiled("C-0881", lines881);
    m.run();
    m.compiled("C-0882", [
      ...lines882,
      "- Always push straight to main. [M-0999]", // a hand-added cite
      "- The old staging host is fly-staging-2. [M-0131]",
    ]);
    m.warm({
      targets: [m.target("C-0882", ["M-0219", "M-0098", "M-0071", "M-0131"])],
      forgotten: [forget("M-0131", new Date())],
    });
    const out = m.run().output;
    expect(out).toContain("[M-0071]");
    expect(out).not.toContain("M-0999");
    expect(out).not.toContain("fly-staging-2");
    expect(out).toContain("- M-0131 (");
  });

  it("holds back the lines of a file with a hand edit waiting in Review", () => {
    m.compiled("C-0881", lines881);
    m.run();
    m.compiled("C-0882", lines882);
    m.warm({
      targets: [
        m.target("C-0882", ["M-0219", "M-0098", "M-0071"], {
          sync_state: "drifted",
          open_drift: 1,
        }),
      ],
    });
    const out = m.run().output;
    expect(out).toContain("AGENTS.md has a local edit that waits in Review");
    expect(out).not.toContain("pnpm workspaces");
  });

  it("says when the space compiled something this machine doesn't have yet", () => {
    m.compiled("C-0881", lines881);
    m.warm({ targets: [m.target("C-0881", ["M-0219"])] });
    m.run();
    m.warm({ targets: [m.target("C-0883", ["M-0219"])] });
    const out = m.run().output;
    expect(out).toContain(
      "AGENTS.md here is C-0881; the space has compiled C-0883 since (it reaches this machine when the daemon delivers it).",
    );
    expect(m.run().output).toBe("");
  });

  it("doesn't say a file is behind when the cache is", () => {
    m.compiled("C-0884", lines881);
    m.warm({ targets: [m.target("C-0883", ["M-0219"])] });
    m.run();
    m.warm({ targets: [m.target("C-0882", ["M-0219"])] });
    expect(m.run().output).toBe("");
  });

  it("reads CLAUDE.md's own lines for Claude Code, and only its managed block", () => {
    m.compiled("C-0881", lines881);
    writeFileSync(
      join(m.repo, "CLAUDE.md"),
      [
        "# My own notes",
        "- Not Memax's. [M-0001]",
        "<!-- memax:start -->",
        "<!-- Compiled by Memax from acme-web at 2026-10-07 09:00 UTC (C-0890). Edit it at https://memax.app/acme-web/brief; edits here come back as proposals. -->",
        "@AGENTS.md",
        "<!-- memax:end -->",
        "",
      ].join("\n"),
    );
    m.run();
    writeFileSync(
      join(m.repo, "CLAUDE.md"),
      readFileSync(join(m.repo, "CLAUDE.md"), "utf8")
        .replace("(C-0890)", "(C-0891)")
        .replace(
          "@AGENTS.md",
          "@AGENTS.md\n- Use plan mode for migrations. [M-0300]",
        ),
    );
    const out = m.run().output;
    expect(out).toContain("CLAUDE.md is now C-0891");
    expect(out).toContain("[M-0300]");
    expect(out).not.toContain("M-0001");
  });
});

describe("notices", () => {
  it("tells a person's forgets once, and a first session only the last week's", () => {
    const now = new Date();
    m.compiled("C-0881", lines881);
    m.warm({
      targets: [m.target("C-0881", ["M-0219"])],
      forgotten: [
        forget("M-0099", new Date(now.getTime() - DAY), { with: ["M-0100"] }),
        forget("M-0050", new Date(now.getTime() - 20 * DAY)),
      ],
    });
    const first = m.run().output;
    expect(first).toContain(
      "Forgotten by a person (don't use or repeat what these said, and drop any copy you keep):",
    );
    expect(first).toContain(
      `- M-0099 (${new Date(now.getTime() - DAY).toISOString().slice(0, 10)}, with M-0100)`,
    );
    expect(first).not.toContain("M-0050");
    expect(m.run().output).toBe("");
    m.warm({
      targets: [m.target("C-0881", ["M-0219"])],
      forgotten: [
        forget("M-0120", now),
        forget("M-0099", new Date(now.getTime() - DAY), { with: ["M-0100"] }),
      ],
    });
    const next = m.run().output;
    expect(next).toContain("- M-0120 (");
    expect(next).not.toContain("M-0099");
  });

  it("tells a waiting gate once, and never an expired one", () => {
    const now = new Date();
    m.compiled("C-0881", lines881);
    m.warm({
      targets: [m.target("C-0881", ["M-0219"])],
      gates: [
        gate(
          "G-0012",
          new Date(now.getTime() + 2 * DAY),
          "Fly.io or Railway for the v2 API?",
        ),
        gate("G-0009", new Date(now.getTime() - DAY)),
      ],
    });
    const out = m.run().output;
    expect(out).toContain(
      `- G-0012 "Fly.io or Railway for the v2 API?" (asked by codex, open until ${new Date(now.getTime() + 2 * DAY).toISOString().slice(0, 10)})`,
    );
    expect(out).not.toContain("G-0009");
    expect(m.run().output).toBe("");
  });

  it("strips hidden characters from a gate's question", () => {
    m.compiled("C-0881", lines881);
    m.warm({
      gates: [gate("G-0013", new Date(Date.now() + DAY), "Ship​ it‮ now?")],
    });
    expect(m.run().output).toContain('"Ship it now?"');
  });
});

describe("formats", () => {
  beforeEach(() => {
    m.compiled("C-0881", lines881);
    m.warm({ gates: [gate("G-0012", new Date(Date.now() + DAY))] });
  });

  it("prints plain text for Claude Code and Codex", () => {
    expect(m.run("claude-code").output.startsWith("<memax-context")).toBe(true);
    const codex = m.run("codex").output;
    expect(codex.startsWith("<memax-context")).toBe(true);
  });

  it("prints the JSON Gemini CLI, Cursor and Copilot CLI read", () => {
    const g = JSON.parse(m.run("gemini").output);
    expect(g.hookSpecificOutput.hookEventName).toBe("SessionStart");
    expect(g.hookSpecificOutput.additionalContext).toContain("G-0012");
    expect(JSON.parse(m.run("cursor").output).additional_context).toContain(
      "G-0012",
    );
    expect(JSON.parse(m.run("copilot").output).additionalContext).toContain(
      "G-0012",
    );
  });

  it("stays quiet in Claude Code's hook when Cursor or Copilot runs it", () => {
    expect(
      m.run("claude-code", {}, { CURSOR_VERSION: "2.4.0" }).skipped,
    ).toMatch(/cursor/);
    expect(
      m.run("claude-code", { sessionId: "abc", cwd: m.repo }).skipped,
    ).toMatch(/copilot/);
  });

  it("finds the repository from Cursor's workspace roots and the agents' variables", () => {
    const res = m.run("cursor", { cwd: undefined, workspace_roots: [m.repo] });
    expect(res.output).toContain("G-0012");
    const env = m.run(
      "gemini",
      { cwd: undefined },
      { GEMINI_PROJECT_DIR: m.repo },
    );
    expect(env.loads).toHaveLength(1);
  });
});

describe("the cap", () => {
  const many = (n: number, word: string) =>
    Array.from(
      { length: n },
      (_, i) =>
        `- ${word} rule ${i}: keep handlers small, name every error, and test the boundary before merging anything new. [M-${String(1000 + i).padStart(4, "0")}]`,
    );

  it("holds the block under 3000 tokens and Claude Code's 10,000 characters", () => {
    m.compiled("C-0881", many(5, "old"));
    m.run();
    m.compiled("C-0882", many(400, "new"));
    m.warm({
      forgotten: [forget("M-0099", new Date())],
      gates: [gate("G-0012", new Date(Date.now() + DAY))],
    });
    const out = m.run().output;
    expect(estimateTokens(out)).toBeLessThanOrEqual(3000);
    expect(out.length).toBeLessThanOrEqual(MAX_CHARS);
    expect(out).toMatch(
      /…and \d+ more new or changed lines, already in the files above\./,
    );
    // The notices survive the cut, and the block still closes.
    expect(out).toContain("- M-0099 (");
    expect(out).toContain("G-0012");
    expect(out.trimEnd().endsWith("</memax-context>")).toBe(true);
  });

  it("holds Codex's block under its 2,500-token default", () => {
    m.compiled("C-0881", many(5, "old"));
    m.run("codex");
    m.compiled("C-0882", many(400, "new"));
    expect(estimateTokens(m.run("codex").output)).toBeLessThanOrEqual(2400);
  });

  it("counts CJK text closer to one token a character", () => {
    expect(estimateTokens("决定使用 River")).toBe(4 + Math.ceil(6 / 3.5));
  });
});

describe("compile loads", () => {
  it("queues one load per compiled file the agent loaded, with its session", () => {
    m.compiled("C-0881", lines881);
    writeFileSync(
      join(m.repo, "CLAUDE.md"),
      `${"<!-- Compiled by Memax from acme-web at 2026-10-07 09:00 UTC (C-0890). Edit it at https://memax.app/acme-web/brief; edits here come back as proposals. -->"}\n@AGENTS.md\n`,
    );
    const now = new Date("2026-10-07T10:00:00Z");
    const res = m.run("claude-code", { session_id: "sess-1" }, {}, now);
    expect(res.loads).toEqual([
      {
        space: SPACE_ID,
        compile: "C-0890",
        agent: "claude-code",
        session_ref: "sess-1",
        loaded_at: now.toISOString(),
      },
      {
        space: SPACE_ID,
        compile: "C-0881",
        agent: "claude-code",
        session_ref: "sess-1",
        loaded_at: now.toISOString(),
      },
    ]);
    expect(m.run("codex").loads.map((l) => l.compile)).toEqual(["C-0881"]);
  });

  it("queues none after a compaction, and names the space by slug where the repository isn't linked", () => {
    m.compiled("C-0881", lines881);
    expect(m.run("claude-code", { source: "compact" }).loads).toEqual([]);
    const bare = machine({ linked: false });
    try {
      bare.compiled("C-0700", lines881);
      expect(bare.run().loads[0]).toMatchObject({
        space: "acme-web",
        compile: "C-0700",
      });
    } finally {
      bare.cleanup();
    }
  });
});

describe("pieces", () => {
  it("names the same files as the daemon", () => {
    const dir = join(m.home, ".memax", "daemon");
    const d = daemonPaths(dir);
    const h = hookPaths(dir);
    for (const k of ["dir", "repos", "pid", "warm", "loads", "seen"] as const)
      expect(h[k]).toBe(d[k]);
  });

  it("claims a session once, so two hooks for one session print once", () => {
    expect(claimSession(m.paths.seen, "s1", "startup")).toBe(true);
    expect(claimSession(m.paths.seen, "s1", "startup")).toBe(false);
    expect(claimSession(m.paths.seen, "s1", "compact")).toBe(true);
    expect(
      claimSession(m.paths.seen, "s1", "startup", Date.now() + 60_000),
    ).toBe(true);
  });

  it("parses the compiler's header and cites", () => {
    const f = parseCompiled(
      "AGENTS.md",
      "<!-- Compiled by Memax from memax-v2 at 2026-10-05 21:31 UTC (C-0881). Edit it at https://memax.app/memax-v2/brief; edits here come back as proposals. -->\n## Open\n- Fly.io or Railway? [M-0431, M-0174]\n- Same pair. [M-0431, M-0174]\nprose without cites\n",
    );
    expect(f?.compile).toBe("C-0881");
    expect(f?.space).toBe("memax-v2");
    expect(f?.lines.map((l) => l.key)).toEqual([
      "M-0431,M-0174",
      "M-0431,M-0174#2",
    ]);
    expect(
      parseCompiled("AGENTS.md", "# A person's own file\n- [M-0001]\n"),
    ).toBeNull();
  });
});
