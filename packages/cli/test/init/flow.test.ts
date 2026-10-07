// memax init end to end against the fake /v2 server: a repository whose
// agent files disagree, machine-local memory in a temporary home, and a
// scripted person at the terminal. Then running it again, the
// non-interactive form, the dry run, and the rule that a hand-written file
// is never overwritten.
import { execFileSync } from "node:child_process";
import { existsSync, mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { dirname, join } from "node:path";
import type { V2 } from "memax-sdk";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { readRegistry } from "../../src/lib/daemon/registry.js";
import type { AgentEntry } from "../../src/lib/init/agents.js";
import { runGit } from "../../src/lib/init/git.js";
import { runInit } from "../../src/lib/init/run.js";
import type {
  CompileOutcome,
  InitDeps,
  InitOptions,
} from "../../src/lib/init/types.js";
import { harness, type Harness } from "../daemon/harness.js";
import { installInit, type InitState } from "./fake-init.js";

// eslint-disable-next-line no-control-regex
const plain = (s: string) => s.replace(/\x1b\[[0-9;]*m/g, "");
const TOKEN = "ghp_" + "a1B2c3D4e5F6g7H8i9J0k1L2m3N4o5P6q7R8";

let h: Harness;
let st: InitState;
let personal: V2.Space;
let present = new Set<string>();
beforeEach(async () => {
  present = new Set();
  h = await harness();
  st = installInit(h.fake);
  personal = h.fake.addSpace("personal-zz", "Personal");
  personal.kind = "personal";
  const git = (...args: string[]) =>
    execFileSync("git", args, { cwd: h.repo, stdio: "pipe" });
  git("init", "-q", "-b", "main");
  git("config", "user.email", "t@example.com");
  git("config", "user.name", "T");
  git("remote", "add", "origin", "git@github.com:acme/web.git");
  put(
    h.repo,
    "CLAUDE.md",
    [
      "# Acme web",
      "",
      "## Testing",
      "- Run tests with `pnpm test`.",
      "",
      "## Conventions",
      "- pnpm workspaces only. Never run npm install at the root.",
      "- API errors are RFC 9457 problem+json.",
      `- The CI bot token is ${TOKEN}.`,
      "- Prefer named\u{202E} exports.",
    ].join("\n"),
  );
  put(
    h.repo,
    "AGENTS.md",
    [
      "# Agents",
      "- Run `npm run test` before committing.",
      "- pnpm workspaces only; never run npm install at the root",
      "- Commit messages are short and imperative.",
    ].join("\n"),
  );
  put(
    h.repo,
    ".cursor/rules/testing.mdc",
    "---\nglobs: **/*.test.ts\nalwaysApply: false\n---\n- Use `pnpm vitest run` for unit tests.\n",
  );
  git("add", ".");
  git("commit", "-qm", "agent files");
  put(h.home, ".claude/CLAUDE.md", "# Me\n- I prefer short answers.\n");
  put(
    h.home,
    ".codex/memories/notes.md",
    "- The MCP spec requires OAuth 2.1, see https://modelcontextprotocol.io/spec.\n- Workers deploy from main only.\n",
  );
  mkdirSync(join(h.home, ".claude", "projects"), { recursive: true });
  // The conflict check finds the three test commands.
  st.conflicts = (ms) => {
    const tests = ms.filter((m) => /\btests?\b/i.test(m.statement));
    return tests.length >= 2
      ? [{ subject: "Test command", members: tests.map((m) => m.ref) }]
      : [];
  };
});
afterEach(async () => {
  await h.cleanup();
});

function put(dir: string, rel: string, content: string): void {
  mkdirSync(dirname(join(dir, rel)), { recursive: true });
  writeFileSync(join(dir, rel), content);
}

interface Run {
  code: number;
  out: string;
  asked: string[];
  mcp: string[];
}

/** Runs init with scripted answers: each question must match one. */
async function init(
  o: InitOptions,
  answers: Array<[RegExp, boolean | string]> = [],
  interactive = true,
): Promise<Run> {
  const lines: string[] = [];
  const asked: string[] = [];
  const mcp: string[] = [];
  const answer = (q: string) => {
    asked.push(plain(q));
    const a = answers.find(([re]) => re.test(q));
    if (!a) throw new Error(`unexpected question: ${q}`);
    return a[1];
  };
  const compile = async (): Promise<CompileOutcome> => ({
    timedOut: false,
    writer: "compile",
    // The daemon's rule: a file someone else wrote is reported, not written.
    targets: [...h.fake.targets.values()].map(({ target: t }) => {
      const hand =
        t.path &&
        existsSync(join(h.repo, t.path)) &&
        !t.settings.user_owned &&
        t.kind === "agents_md";
      if (hand) t.sync_state = "drifted";
      else if (t.sync_state !== "off") t.sync_state = "in_sync";
      return {
        label: t.label,
        kind: t.kind,
        sync_state: t.sync_state,
        files: t.path && t.kind !== "cursor_mdc" ? [t.path] : [],
      };
    }),
  });
  const d: InitDeps = {
    memax: h.memax,
    paths: h.paths,
    cwd: h.repo,
    home: h.home,
    env: { path: "", platform: "linux" },
    out: (l) => lines.push(plain(l)),
    interactive,
    prompt: {
      confirm: async (q) => answer(q) as boolean,
      ask: async (q) => answer(q) as string,
    },
    git: runGit,
    hasCredentials: () => true,
    signIn: async () => false,
    hasMcp: (a: AgentEntry) => mcp.includes(a.kind) || present.has(a.kind),
    writeMcp: async (a: AgentEntry) => {
      mcp.push(a.kind);
      present.add(a.kind);
      return "written";
    },
    startDaemon: async () => false,
    compile,
    appUrl: "https://memax.app",
    version: "test",
    now: () => performance.now(),
    sleep: (ms) => new Promise((r) => setTimeout(r, Math.min(ms, 5))),
  };
  const code = await runInit(o, d);
  return { code, out: lines.join("\n"), asked, mcp };
}

const imports = () => h.fake.calls("POST", /\/imports$/);
const bodies = () => imports().map((c) => c.body as V2.ImportInput);

describe("memax init", () => {
  it("imports a repository whose agent files disagree, settles them and compiles", async () => {
    mkdirSync(join(h.home, ".claude-code-marker"), { recursive: true });
    mkdirSync(join(h.home, ".claude"), { recursive: true });
    const r = await init({}, [
      [/Connect Claude Code/, true],
      [/Bring \d+ notes/, true],
      [/Which holds/, "2"],
      [/Keep the \d+\?|Keep it\?/, true],
      [/CLAUDE.md is yours/, true],
      [/Start the Memax daemon/, false],
      [/Replace AGENTS.md/, false],
    ]);
    expect(r.code).toBe(0);
    if (process.env.INIT_DUMP) writeFileSync(process.env.INIT_DUMP, r.out);
    // 1–3: a new project space for the repository, its agents, Personal switched.
    const space = h.fake.spaces.find((s) => s.repository === "acme/web")!;
    expect(space).toBeDefined();
    expect(personal.v2_enabled_at).toBeTruthy();
    expect(r.mcp).toContain("claude-code");
    expect(r.out).toContain("Agents on this machine");

    // 4–5: what was sent. No secret, no hidden characters, sources by file:line.
    const [project, home] = bodies();
    const sent = JSON.stringify(bodies());
    expect(sent).not.toContain(TOKEN);
    expect(sent).not.toContain("\u{202E}");
    expect(project.skipped).toEqual([
      {
        ref: "CLAUDE.md:9",
        reason: "secret",
        detail: expect.stringMatching(/GitHub token|vendor API key/),
      },
    ]);
    const named = project.items.find(
      (i) => i.statement === "Prefer named exports.",
    )!;
    expect(named.hidden_characters).toBe(1);
    expect(
      project.items.find((i) => i.ref === "CLAUDE.md:4")?.sources?.[0],
    ).toMatchObject({
      kind: "file",
      ref: "CLAUDE.md:4",
      trust: "repository",
      locator: { path: "CLAUDE.md", line: 4, heading: "Acme web › Testing" },
    });
    expect(
      project.items.find((i) => i.ref === ".cursor/rules/testing.mdc:5")?.scope,
    ).toEqual({ paths: ["**/*.test.ts"] });
    expect(
      home.items.find((i) => i.ref === "~/.claude/CLAUDE.md:2")?.sources?.[0]
        .trust,
    ).toBe("person");
    const oauth = home.items.find((i) => i.statement.includes("OAuth 2.1"))!;
    expect(oauth.sources?.map((s) => [s.kind, s.trust ?? null])).toEqual([
      ["file", "agent_own_work"],
      ["url", null],
    ]);

    // 6–7: the disagreement settled once, the ones that agree kept, the rest wait.
    expect(r.out).toContain("1 conflict, 1 Brief.");
    expect(st.settled).toEqual([{ n: 1, choice: "keep_one" }]);
    const kept = new Set(st.kept);
    const keptStatements = [...st.memories.values()]
      .filter((m) => kept.has(m.ref) || m.lifecycle === "kept")
      .map((m) => m.statement);
    expect(keptStatements).toEqual(
      expect.arrayContaining([
        "Run tests with `pnpm test`.",
        "pnpm workspaces only. Never run npm install at the root.",
        "API errors are RFC 9457 problem+json.",
        "Commit messages are short and imperative.",
        "I prefer short answers.",
      ]),
    );
    // Agents' notes, an outside source and a line with hidden characters wait in Review.
    for (const s of ["Workers deploy from main only.", "Prefer named exports."])
      expect(keptStatements).not.toContain(s);
    expect(keptStatements.some((s) => s.includes("OAuth"))).toBe(false);
    expect(r.out).toMatch(/proposals? wait/);

    // 8: the first Brief, the targets (CLAUDE.md made the person's), the link.
    const brief = st.briefs.get(space.id)!;
    expect(brief.sections.map((s) => s.key)).toEqual(["conventions"]);
    const kinds = [...h.fake.targets.values()].map((e) => [
      e.target.kind,
      !!e.target.settings.user_owned,
    ]);
    expect(kinds).toEqual([
      ["agents_md", false],
      ["claude_md", true],
      ["cursor_mdc", false],
      ["chatgpt", false],
    ]);
    expect(readFileSync(join(h.repo, ".memax.yml"), "utf8")).toContain(
      `space: ${space.slug}`,
    );
    expect(readRegistry(h.paths).map((l) => l.space_id)).toEqual([space.id]);
    // The hand-written AGENTS.md is left as it is: offered, declined, never overwritten.
    expect(r.asked.some((q) => /Replace AGENTS.md/.test(q))).toBe(true);
    expect(h.fake.calls("POST", /drift:overwrite/)).toEqual([]);
    expect(readFileSync(join(h.repo, "AGENTS.md"), "utf8")).toContain(
      "Run `npm run test` before committing.",
    );
    expect(r.out).toContain(`${space.slug} is compiled into`);
    expect(r.out).toContain("Next");
  });

  it("is safe and quick to run again: nothing new is proposed, and it says what is done", async () => {
    await init({ yes: true });
    const before = st.memories.size;
    const again = await init({}, [
      [/Which holds/, ""],
      [/Start the Memax daemon/, false],
      [/Replace AGENTS.md/, false],
    ]);
    expect(again.code).toBe(0);
    expect(st.memories.size).toBe(before);
    const last = bodies().slice(-2);
    expect(last.length).toBe(2);
    expect(again.out).toContain("Already done");
    expect(again.out).toContain("This repository is linked to");
    expect(again.out).toMatch(/already in the record/);
    // Nothing it did before is asked again; what's left (the disagreement
    // --yes never settles) is offered, and Enter leaves it for Review.
    expect(
      again.asked.filter((q) =>
        /Connect|Bring|Keep the|CLAUDE.md is yours/.test(q),
      ),
    ).toEqual([]);
    expect(again.asked.filter((q) => /Which holds/.test(q)).length).toBe(1);
    expect(again.out).toContain("Machine-local memory goes to personal-zz.");
  });

  it("runs without a terminal for CI: --yes --format json", async () => {
    const lines: string[] = [];
    const r = await init({ yes: true, format: "json" }, [], false);
    expect(r.code).toBe(0);
    expect(r.asked).toEqual([]);
    const report = JSON.parse(r.out);
    lines.push(r.out);
    expect(report.space.created).toBe(true);
    expect(report.imports.length).toBe(2);
    expect(report.secrets).toBe(1);
    expect(report.timings.map((t: { step: string }) => t.step)).toEqual(
      expect.arrayContaining([
        "detect",
        "sign in",
        "connect",
        "scan",
        "upload",
        "judge",
        "settle",
        "compile",
      ]),
    );
    // --yes never settles a disagreement for the person: it waits in Review.
    expect(st.settled).toEqual([]);
    expect(report.waiting).toBeGreaterThan(0);
  });

  it("needs --yes or --space to create a space without a terminal", async () => {
    const r = await init({}, [], false);
    expect(r.code).toBe(1);
    expect(r.out).toContain("memax init --yes");
    expect(imports()).toEqual([]);
  });

  it("uploads nothing on a dry run", async () => {
    const r = await init({ dryRun: true }, [], false);
    expect(r.code).toBe(0);
    expect(h.fake.log.filter((l) => l.method !== "GET")).toEqual([]);
    expect(r.out).toContain("nothing left this machine");
  });

  it("keeps machine-local memory where it is with --no-personal", async () => {
    await init({ yes: true, personal: false });
    expect(bodies().length).toBe(1);
    expect(personal.v2_enabled_at).toBeUndefined();
  });
});
