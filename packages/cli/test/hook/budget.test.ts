// The session-start hook's budget (plan 25 §7.5, personal-layer design
// §7.1): under 100 ms from process start to exit, with a linked
// repository and a home link, over 50 cold runs of the built CLI, each a
// new process that prints both sections. Measured against `node -e ''`
// on the same machine. Opt-in (MEMAX_HOOK_BUDGET=1), since a loaded
// machine moves wall clock:
//
//   MEMAX_HOOK_BUDGET=1 pnpm --filter memax-cli exec vitest run test/hook/budget.test.ts
import { execFileSync, spawn, type ChildProcess } from "node:child_process";
import {
  mkdirSync,
  mkdtempSync,
  realpathSync,
  renameSync,
  rmSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterAll, beforeAll, describe, expect, it } from "vitest";
import { header } from "./fixture.js";

const enabled = process.env.MEMAX_HOOK_BUDGET === "1";
const RUNS = 50;
const cli = join(import.meta.dirname, "..", "..");
const out = join(cli, "node_modules", ".cache", "memax-hook-budget", "dist");
const bin = join(out, "bin.js");
const REPO_SPACE = "11111111-1111-4111-8111-111111111111";
const PERSONAL = "22222222-2222-4222-8222-222222222222";

let base = "";
let home = "";
let repo = "";
let daemon: ChildProcess | null = null;

function percentile(xs: number[], p: number): number {
  const s = [...xs].sort((a, b) => a - b);
  return s[Math.min(s.length - 1, Math.ceil((p / 100) * s.length) - 1)];
}

function time(args: string[], stdin: string, env: NodeJS.ProcessEnv) {
  return new Promise<{ ms: number; stdout: string }>((resolve, reject) => {
    const started = performance.now();
    const child = spawn(process.execPath, args, {
      env,
      cwd: home,
      stdio: ["pipe", "pipe", "ignore"],
    });
    let stdout = "";
    child.stdout!.on("data", (b) => (stdout += String(b)));
    child.on("error", reject);
    child.on("exit", () =>
      resolve({ ms: performance.now() - started, stdout }),
    );
    child.stdin!.end(stdin);
  });
}

describe.skipIf(!enabled)("the session-start hook's budget", () => {
  beforeAll(() => {
    rmSync(out, { recursive: true, force: true });
    execFileSync(
      process.execPath,
      [
        join(cli, "node_modules", "typescript", "bin", "tsc"),
        "-p",
        cli,
        "--outDir",
        out,
      ],
      { stdio: "inherit" },
    );
    base = realpathSync(mkdtempSync(join(tmpdir(), "memax-hook-budget-")));
    home = join(base, "home");
    repo = join(base, "repo");
    const dir = join(home, ".memax", "daemon");
    mkdirSync(dir, { recursive: true });
    mkdirSync(join(repo, ".git"), { recursive: true });
    // A linked repository with AGENTS.md and CLAUDE.md, 40 cited lines.
    const lines = Array.from(
      { length: 40 },
      (_, i) =>
        `- Decision ${i}: a line of about the usual length for a compiled fact. [M-0${100 + i}]`,
    );
    writeFileSync(
      join(repo, "AGENTS.md"),
      `${header("C-0881")}\n## Decisions\n${lines.join("\n")}\n`,
    );
    writeFileSync(join(repo, "CLAUDE.md"), `${header("C-0882")}\n@AGENTS.md\n`);
    writeFileSync(
      join(dir, "repos.json"),
      JSON.stringify({
        version: 1,
        repos: [
          {
            root: repo,
            space_id: REPO_SPACE,
            space_slug: "acme-web",
            created_at: "2026-10-01T00:00:00Z",
          },
        ],
      }),
    );
    // The home link: Claude Code's rules file (6 KiB, as compiled) and Codex's block.
    const personal = Array.from(
      { length: 50 },
      (_, i) =>
        `- Personal preference ${i}, as the person keeps it in Memax. [M-0${500 + i}]`,
    );
    const body = `${header("C-1201", "personal")}\n## Preferences\n${personal.join("\n")}\n`;
    mkdirSync(join(home, ".claude", "rules"), { recursive: true });
    mkdirSync(join(home, ".codex"), { recursive: true });
    writeFileSync(join(home, ".claude/rules/memax-personal.md"), body);
    writeFileSync(
      join(home, ".codex/AGENTS.md"),
      `# Mine\n<!-- memax:start -->\n${body}<!-- memax:end -->\n`,
    );
    // The canonical copy, which Cursor's hook prints in full.
    mkdirSync(join(home, ".memax", "personal"), { recursive: true });
    writeFileSync(join(home, ".memax/personal/AGENTS.md"), body);
    const files = [
      ["claude_user", ".claude/rules/memax-personal.md"],
      ["codex_user", ".codex/AGENTS.md"],
      ["agents_md", ".memax/personal/AGENTS.md"],
    ];
    writeFileSync(
      join(dir, "home.json"),
      JSON.stringify({
        version: 1,
        space_id: PERSONAL,
        space_slug: "personal",
        machine_id: "m-1",
        home,
        files: files.map(([kind, path], i) => ({
          target_id: `t-${i}`,
          kind,
          path,
          abs: join(home, path),
        })),
        created_at: new Date().toISOString(),
      }),
    );
    const now = new Date().toISOString();
    const expires = new Date(Date.now() + 86_400_000).toISOString();
    writeFileSync(
      join(dir, "warm.json"),
      JSON.stringify({
        version: 1,
        spaces: {
          [REPO_SPACE]: {
            slug: "acme-web",
            updated_at: now,
            targets: [],
            forgotten: [],
            gates: [
              {
                ref: "G-0012",
                question: "Fly.io or Railway?",
                asked_at: now,
                expires_at: expires,
              },
            ],
          },
        },
        home: {
          space_id: PERSONAL,
          machine_id: "m-1",
          slug: "personal",
          updated_at: now,
          targets: [],
          forgotten: [{ id: "t-1", ref: "M-0499", with: [], at: now }],
          gates: [],
        },
      }),
    );
    // A daemon "runs" (its pid file names `daemon run`), as on a set-up
    // machine, so the hook queues its loads without starting a flush.
    daemon = spawn(
      process.execPath,
      ["-e", "setInterval(() => {}, 1e9)", "daemon", "run"],
      {
        stdio: "ignore",
      },
    );
    writeFileSync(join(dir, "daemon.pid"), `${daemon.pid}\n`);
  }, 120_000);

  afterAll(() => {
    daemon?.kill("SIGKILL");
    if (base) rmSync(base, { recursive: true, force: true });
  });

  // 200 processes, one after another: about 20 s on a quiet machine.
  it(
    `exits within 100 ms at p95 over ${RUNS} cold runs`,
    { timeout: 120_000 },
    async () => {
      const env = {
        ...process.env,
        HOME: home,
        MEMAX_API_URL: "http://127.0.0.1:9",
        NODE_OPTIONS: "",
      };
      const node: number[] = [];
      const hook: number[] = [];
      // One run of each first, so the file system cache is the same for both.
      await time(["-e", ""], "", env);
      for (let i = 0; i < RUNS; i++) {
        node.push((await time(["-e", ""], "", env)).ms);
        // A first session each time (what it told is cleared), so it prints
        // both sections and writes both seen files.
        rmSync(join(home, ".memax", "daemon", "seen"), {
          recursive: true,
          force: true,
        });
        const event = JSON.stringify({
          session_id: `s-${i}`,
          cwd: repo,
          source: "startup",
        });
        const r = await time(
          [bin, "hook", "session-start", "--agent", "claude-code"],
          event,
          env,
        );
        expect(r.stdout).toContain("Your Personal space (personal)");
        hook.push(r.ms);
      }
      // The same without the home link, for what the Personal section costs.
      const homeLink = join(home, ".memax", "daemon", "home.json");
      renameSync(homeLink, `${homeLink}.off`);
      const repoOnly: number[] = [];
      for (let i = 0; i < RUNS; i++) {
        rmSync(join(home, ".memax", "daemon", "seen"), {
          recursive: true,
          force: true,
        });
        const event = JSON.stringify({
          session_id: `r-${i}`,
          cwd: repo,
          source: "startup",
        });
        repoOnly.push(
          (
            await time(
              [bin, "hook", "session-start", "--agent", "claude-code"],
              event,
              env,
            )
          ).ms,
        );
      }
      renameSync(`${homeLink}.off`, homeLink);
      // Cursor: the canonical copy's kept lines printed in full.
      const cursor: number[] = [];
      for (let i = 0; i < RUNS; i++) {
        const event = JSON.stringify({
          session_id: `c-${i}`,
          workspace_roots: [repo],
        });
        const r = await time(
          [bin, "hook", "session-start", "--agent", "cursor"],
          event,
          env,
        );
        expect(r.stdout).toContain("You load no file of it");
        cursor.push(r.ms);
      }
      const p = (xs: number[]) =>
        `p50 ${percentile(xs, 50).toFixed(1)} ms, p95 ${percentile(xs, 95).toFixed(1)} ms`;
      console.log(
        [
          `node -e '': ${p(node)}`,
          `memax hook session-start, repository + home link: ${p(hook)}`,
          `memax hook session-start, repository only: ${p(repoOnly)}`,
          `memax hook session-start --agent cursor, Personal printed: ${p(cursor)}`,
        ].join("\n"),
      );
      expect(percentile(hook, 95)).toBeLessThan(100);
      expect(percentile(cursor, 95)).toBeLessThan(100);
    },
  );
});
