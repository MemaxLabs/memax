// memax init end to end against the real stack (plan 25 §7.3, Phase 2
// epic 2.1): a repository whose agent files disagree (CLAUDE.md runs tests
// one way, AGENTS.md another, and a Cursor rule adds its own) goes through
// `memax init --yes`; the import check flags the disagreement; the
// statements that agree are kept in bulk; the space compiles, and the files
// are on disk, measured against "first file in under five minutes". Then
// the person settles the disagreement and replaces the hand-written
// AGENTS.md, and a second run is quick and changes nothing.
//
// The worker's judge talks to a fake Anthropic-compatible model here (no
// keys, no network): it groups the two test commands as one disagreement
// and calls everything else unrelated. Sign-in is a session token written
// where `memax login` puts it; the browser step isn't timed.
//
//   MEMAX_E2E_SERVER=1 pnpm --filter memax-cli exec vitest run test/init/e2e-init.test.ts
//
// It needs what the daemon's end-to-end test needs (Go, Postgres, psql).
import { execFileSync, spawn } from "node:child_process";
import { randomUUID } from "node:crypto";
import {
  existsSync,
  mkdirSync,
  mkdtempSync,
  readdirSync,
  readFileSync,
  realpathSync,
  rmSync,
  symlinkSync,
  writeFileSync,
} from "node:fs";
import { createServer, type Server } from "node:http";
import type { AddressInfo } from "node:net";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { Memax, type V2 } from "memax-sdk";
import { afterAll, beforeAll, describe, expect, it } from "vitest";
import type { InitReport } from "../../src/lib/init/types.js";
import { startStack, type Stack } from "../daemon/e2e-stack.js";

const enabled = process.env.MEMAX_E2E_SERVER === "1";
if (!enabled) {
  console.log(
    "Skipping memax init against the real server: set MEMAX_E2E_SERVER=1 to run it.",
  );
}

const CLI = join(import.meta.dirname, "..", "..");
const BIN = join(CLI, "dist", "bin.js");
/** Plan 25 §7.3: the first compiled file within five minutes of running init. */
const FIRST_FILE_GATE_MS = 5 * 60_000;
const MANAGED_START = "<!-- memax:start -->";

const FILES: Record<string, string> = {
  "CLAUDE.md": `# Acme payments

## Commands

- Run tests with \`pnpm test\`.
- Use pnpm workspaces; never run \`npm install\` at the root.

## Conventions

- Handlers return errors as JSON with a \`code\` field.
- Every migration lives in its own file under \`migrations/\`.
`,
  "AGENTS.md": `# Acme payments

- Run tests with \`make test\`.
- Handlers return errors as JSON with a \`code\` field.
- Deploys go out from the \`release\` branch, never from \`main\`.
`,
  ".cursor/rules/testing.mdc": `---
description: How tests are written
globs: src/**/*.test.ts
alwaysApply: false
---

- Name each test file after the module it tests.
- Prefer table-driven tests over one test per case.
`,
};

/** The text of a Messages request: the system prompt and every message. */
function promptOf(body: {
  system?: unknown;
  messages?: Array<{ content: unknown }>;
}): string {
  const text = (c: unknown): string =>
    typeof c === "string"
      ? c
      : Array.isArray(c)
        ? c.map((b: { text?: string }) => b.text ?? "").join("\n")
        : "";
  return [
    text(body.system),
    ...(body.messages ?? []).map((m) => text(m.content)),
  ].join("\n");
}

const unescape = (s: string) =>
  s.replace(/&lt;/g, "<").replace(/&gt;/g, ">").replace(/&amp;/g, "&");

/**
 * A fake Anthropic Messages API for the judge: the import check groups the
 * statements that name a test command; a proposal's candidates are
 * unrelated.
 */
function fakeModel(): Promise<{
  url: string;
  server: Server;
  calls: { imports: number; proposals: number; other: number };
}> {
  const calls = { imports: 0, proposals: 0, other: 0 };
  const server = createServer((req, res) => {
    const chunks: Buffer[] = [];
    req.on("data", (c: Buffer) => chunks.push(c));
    req.on("end", () => {
      const body = JSON.parse(Buffer.concat(chunks).toString() || "{}");
      const prompt = promptOf(body);
      let answer: unknown = {};
      if (prompt.includes("<statements>")) {
        calls.imports++;
        const tests = [
          ...prompt.matchAll(
            /<statement id="([^"]+)"[^>]*>\n([\s\S]*?)\n<\/statement>/g,
          ),
        ]
          .filter((m) => /`(pnpm|make) test`/.test(unescape(m[2])))
          .map((m) => m[1]);
        answer = {
          conflicts:
            tests.length >= 2
              ? [
                  {
                    subject: "Test command",
                    members: tests,
                    confidence: 0.95,
                    rationale: `${tests.join(" and ")} name different test commands.`,
                    suggestion: "",
                  },
                ]
              : [],
        };
      } else if (prompt.includes("<candidates>")) {
        calls.proposals++;
        answer = {
          pairs: [...prompt.matchAll(/<candidate id="([^"]+)"/g)].map((m) => ({
            candidate: m[1],
            relation: "unrelated",
            confidence: 0.9,
            explicit_change: false,
            rationale: `${m[1]} is about something else.`,
            merged_statement: "",
          })),
          conditions: [],
        };
      } else {
        calls.other++;
      }
      res.writeHead(200, { "Content-Type": "application/json" });
      res.end(
        JSON.stringify({
          id: `msg_${randomUUID()}`,
          type: "message",
          role: "assistant",
          model: body.model ?? "e2e",
          content: [{ type: "text", text: JSON.stringify(answer) }],
          stop_reason: "end_turn",
          stop_sequence: null,
          usage: { input_tokens: 100, output_tokens: 50 },
        }),
      );
    });
  });
  return new Promise((resolve) =>
    server.listen(0, "127.0.0.1", () =>
      resolve({
        url: `http://127.0.0.1:${(server.address() as AddressInfo).port}`,
        server,
        calls,
      }),
    ),
  );
}

describe.skipIf(!enabled)("memax init against the real server", () => {
  let stack: Stack;
  let model: Awaited<ReturnType<typeof fakeModel>>;
  let memax: Memax;
  let home: string;
  let repo: string;
  let bin: string;
  let report: InitReport;
  let initMs = 0;
  let firstFileMs = 0;
  const env = () => ({
    ...process.env,
    HOME: home,
    // Only git: no agent CLI on this PATH, so init writes no real agent's settings.
    PATH: bin,
    MEMAX_API_URL: stack.apiUrl,
    MEMAX_API_KEY: "",
    XDG_CONFIG_HOME: "",
    FORCE_COLOR: "0",
  });
  const cli = (...args: string[]) =>
    execFileSync(process.execPath, [BIN, ...args], {
      cwd: repo,
      env: env(),
    }).toString();
  const read = (p: string) => readFileSync(join(repo, p), "utf8");
  const git = (...args: string[]) =>
    execFileSync("git", args, { cwd: repo, stdio: "pipe" }).toString();

  async function until(
    check: () => boolean | Promise<boolean>,
    ms: number,
    what: string,
  ): Promise<number> {
    const start = Date.now();
    for (;;) {
      if (await check()) return Date.now() - start;
      if (Date.now() - start > ms)
        throw new Error(
          `timed out after ${ms} ms waiting for ${what}\n${stack.logs.slice(-30).join("\n")}`,
        );
      await new Promise((r) => setTimeout(r, 25));
    }
  }

  /** Runs memax init, watching for the first compiled file while it runs. */
  function init(...args: string[]): Promise<{
    report: InitReport;
    ms: number;
    firstFileMs: number | null;
    stderr: string;
  }> {
    const start = Date.now();
    let first: number | null = null;
    const watch = setInterval(() => {
      if (first !== null) return;
      const claude = existsSync(join(repo, "CLAUDE.md"))
        ? read("CLAUDE.md")
        : "";
      const rules = existsSync(join(repo, ".cursor", "rules"))
        ? readdirSync(join(repo, ".cursor", "rules"))
        : [];
      if (
        claude.includes(MANAGED_START) ||
        rules.some((f) => f.startsWith("memax"))
      )
        first = Date.now() - start;
    }, 20);
    return new Promise((resolve, reject) => {
      const child = spawn(
        process.execPath,
        [BIN, "init", "--yes", "--format", "json", ...args],
        { cwd: repo, env: env() },
      );
      let stdout = "";
      let stderr = "";
      child.stdout.on("data", (b) => (stdout += String(b)));
      child.stderr.on("data", (b) => (stderr += String(b)));
      child.on("error", reject);
      child.on("exit", (code) => {
        clearInterval(watch);
        const ms = Date.now() - start;
        try {
          const r = JSON.parse(stdout) as InitReport & { error?: string };
          if (code !== 0 || r.error)
            return reject(
              new Error(
                `memax init exited ${code}: ${r.error ?? ""}\n${stderr}\n${stack.logs.slice(-30).join("\n")}`,
              ),
            );
          resolve({ report: r, ms, firstFileMs: first, stderr });
        } catch {
          reject(
            new Error(
              `memax init didn't print a JSON report (exit ${code}):\n${stdout}\n${stderr}`,
            ),
          );
        }
      });
    });
  }

  beforeAll(async () => {
    execFileSync(
      process.execPath,
      [join(CLI, "node_modules", "typescript", "bin", "tsc"), "-p", CLI],
      { stdio: "inherit" },
    );
    model = await fakeModel();
    stack = await startStack({
      workerEnv: {
        ANTHROPIC_API_KEY: "e2e",
        ANTHROPIC_BASE_URL: model.url,
        JUDGE_MODEL: "e2e-judge",
        JUDGE_FALLBACK_MODEL: "off",
        JUDGE_STRONG_MODEL: "off",
        JUDGE_ZDR: "false",
      },
    });
    memax = new Memax({ apiUrl: stack.apiUrl, apiKey: stack.token });
    const base = realpathSync(mkdtempSync(join(tmpdir(), "memax-e2e-init-")));
    home = join(base, "home");
    repo = join(base, "payments");
    bin = join(base, "bin");
    mkdirSync(join(home, ".memax"), { recursive: true });
    mkdirSync(bin);
    symlinkSync(
      execFileSync("sh", ["-c", "command -v git"]).toString().trim(),
      join(bin, "git"),
    );
    writeFileSync(
      join(home, ".memax", "credentials.json"),
      JSON.stringify({
        access_token: stack.token,
        refresh_token: "",
        expires_at: Date.now() + 7_200_000,
      }),
      { mode: 0o600 },
    );
    mkdirSync(repo);
    git("init", "-q", "-b", "main");
    git("config", "user.email", "e2e@example.com");
    git("config", "user.name", "E2E");
    // Not acme/api or acme/billing: reserved slugs, so the space would be api-space.
    git("remote", "add", "origin", "git@github.com:acme/payments.git");
    for (const [path, content] of Object.entries(FILES)) {
      mkdirSync(dirname(join(repo, path)), { recursive: true });
      writeFileSync(join(repo, path), content);
    }
    git("add", ".");
    git("commit", "-qm", "Agent files");
  }, 600_000);

  afterAll(async () => {
    try {
      if (home) cli("daemon", "stop");
    } catch {
      // not running
    }
    await stack?.stop();
    model?.server.close();
    if (home) rmSync(join(home, ".."), { recursive: true, force: true });
  }, 60_000);

  it("imports, flags the disagreement, keeps what agrees, and compiles within five minutes", async () => {
    const run = await init("--timing");
    report = run.report;
    initMs = run.ms;
    firstFileMs = run.firstFileMs ?? run.ms;

    // A new project space for the repository, named by its remote.
    expect(report.space).toEqual({ slug: "payments", created: true });
    expect(
      report.files.map((f) => [f.path, f.statements, f.sent]).sort(),
    ).toEqual([
      [".cursor/rules/testing.mdc", 2, 2],
      ["AGENTS.md", 3, 3],
      ["CLAUDE.md", 4, 4],
    ]);
    // Cursor was found in the repository and starts at Read.
    expect(report.agents.find((a) => a.kind === "cursor")).toMatchObject({
      found: true,
      autonomy: "read",
    });
    expect(report.secrets).toBe(0);

    // One import: the repeated line folded, the test commands in conflict.
    expect(report.imports).toHaveLength(1);
    const imp = report.imports[0];
    expect(imp).toMatchObject({ space: "payments", folded: 1, conflicts: 1 });
    expect(imp.proposed).toBe(8);
    expect(model.calls.imports).toBeGreaterThanOrEqual(1);
    const view = await memax.v2.imports.get("payments", imp.id);
    expect(view.progress.ready).toBe(true);
    expect(view.conflicts).toHaveLength(1);
    const conflict = view.conflicts[0];
    expect(conflict).toMatchObject({ subject: "Test command", state: "open" });
    const byId = new Map(view.memories.map((m) => [m.memory.id, m.memory]));
    const members = conflict.members.map((p) => byId.get(p.id)!);
    expect(members.map((m) => m.statement).sort()).toEqual([
      "Run tests with `make test`.",
      "Run tests with `pnpm test`.",
    ]);
    for (const m of members) {
      expect(m.lifecycle).toBe("proposed");
      expect(m.flags).toContain("conflict");
    }

    // --yes never settles a disagreement; everything that agrees is kept.
    expect(report.settled).toBe(0);
    expect(report.kept).toBe(6);
    expect(report.waiting).toBe(2);
    expect(report.brief).toMatch(/^B-/);

    // The files: CLAUDE.md stays the person's, with one Memax block in it;
    // the hand-written AGENTS.md is left as it is and reported as a drift.
    expect(read("CLAUDE.md")).toContain(FILES["CLAUDE.md"].trim());
    expect(read("CLAUDE.md")).toContain(MANAGED_START);
    expect(read("AGENTS.md")).toBe(FILES["AGENTS.md"]);
    expect(read(".cursor/rules/testing.mdc")).toBe(
      FILES[".cursor/rules/testing.mdc"],
    );
    expect(read(".memax.yml")).toContain("space: payments");
    const targets = (await memax.v2.targets.list("payments")).items;
    expect(
      targets.find((t: V2.Target) => t.kind === "agents_md")?.sync_state,
    ).toBe("drifted");
    expect(
      targets.find((t: V2.Target) => t.kind === "claude_md")?.settings
        .user_owned,
    ).toBe(true);
    const status = git("status", "--short", "--untracked-files=all");
    expect(status).toContain("CLAUDE.md");
    expect(status).not.toContain("AGENTS.md");

    console.log(
      `memax init: first file on disk ${firstFileMs} ms, whole run ${initMs} ms (gate ${FIRST_FILE_GATE_MS} ms); ` +
        report.timings
          .map(
            (t) =>
              `${t.step} ${t.ms} ms${t.budgetMs ? ` (budget ${t.budgetMs})` : ""}`,
          )
          .join(", "),
    );
    console.log(`git status after init:\n${status}`);
    expect(firstFileMs).toBeLessThan(FIRST_FILE_GATE_MS);
    expect(initMs).toBeLessThan(FIRST_FILE_GATE_MS);
  }, 360_000);

  it("settles the disagreement and replaces AGENTS.md when the person says so", async () => {
    const imp = report.imports[0];
    const view = await memax.v2.imports.get("payments", imp.id);
    const conflict = view.conflicts[0];
    const pnpm = view.memories.find(
      (m) => m.memory.statement === "Run tests with `pnpm test`.",
    )!.memory;
    const settled = await memax.v2.imports.settle(
      "payments",
      imp.id,
      conflict.n,
      { choice: "keep_one", keep: pnpm.ref },
      { idempotencyKey: randomUUID(), via: "cli" },
    );
    expect(settled.conflict.state).toBe("settled");

    const agents = (await memax.v2.targets.list("payments")).items.find(
      (t: V2.Target) => t.kind === "agents_md",
    )!;
    await memax.v2.targets.overwrite(
      agents.id,
      { reason: "Its statements were imported first" },
      { idempotencyKey: randomUUID(), via: "cli" },
    );
    const ms = await until(
      () =>
        /^- Run tests with `pnpm test`\. \[M-\d+\]$/m.test(read("AGENTS.md")),
      30_000,
      "the compiled AGENTS.md",
    );
    console.log(`settle → AGENTS.md on disk: ${ms} ms`);
    const compiled = read("AGENTS.md");
    expect(compiled).not.toContain("make test");
    expect(compiled).toContain("Deploys go out from the `release` branch");
    expect(read("CLAUDE.md")).toContain(FILES["CLAUDE.md"].trim());
  }, 60_000);

  it("runs again quickly, says what is done, and changes nothing", async () => {
    const before = {
      claude: read("CLAUDE.md"),
      agents: read("AGENTS.md"),
      yml: read(".memax.yml"),
    };
    const again = await init();
    console.log(`memax init, second run: ${again.ms} ms`);
    expect(again.report.space).toEqual({ slug: "payments", created: false });
    expect(again.report.imports.every((i) => i.proposed === 0)).toBe(true);
    expect(again.report.imports.every((i) => i.conflicts === 0)).toBe(true);
    expect(
      again.report.files.find((f) => f.path === "AGENTS.md"),
    ).toMatchObject({ compiled: true, sent: 0 });
    expect(again.report.kept).toBe(0);
    expect(again.report.done).toEqual(
      expect.arrayContaining([
        "The repository's space is payments.",
        "This repository is linked to payments.",
      ]),
    );
    expect(read("CLAUDE.md")).toBe(before.claude);
    expect(read("AGENTS.md")).toBe(before.agents);
    expect(read(".memax.yml")).toBe(before.yml);
    expect(again.ms).toBeLessThan(30_000);
  }, 120_000);
});
