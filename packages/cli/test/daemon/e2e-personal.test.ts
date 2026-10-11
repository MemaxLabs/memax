// The personal layer end to end against the real Go server (personal-
// layer design §10, the acceptance check): two machines, two temporary
// homes and two device ids, each with Claude Code and Codex set up, run
// the built CLI's memax link --home and a daemon. A person keeps a
// personal fact, and within 60 s both homes carry it, with the person's
// own lines in each file unchanged byte for byte. Forget takes it out of
// both within 60 s, and a grep of both homes finds nothing. Then one
// machine unlinks and the other signs out (memax logout): each takes the
// layer off, giving the person's files back as they were. A third
// machine, with only Claude Code, sets up with memax init --yes --home
// outside any repository, then gains Codex's file with memax connect.
//
// It needs Go, Postgres (a role that can CREATE DATABASE), psql and the
// workspace's node_modules, so it runs only with MEMAX_E2E_SERVER=1:
//
//   MEMAX_E2E_SERVER=1 pnpm --filter memax-cli exec vitest run test/daemon/e2e-personal.test.ts
//
// MEMAX_E2E_DATABASE_URL (or TEST_DATABASE_URL) picks the Postgres;
// MEMAX_E2E_BIN a directory of prebuilt server, worker, migrate and devseed.
import { execFileSync, spawn, type ChildProcess } from "node:child_process";
import { randomUUID } from "node:crypto";
import {
  existsSync,
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
import { join } from "node:path";
import { Memax } from "memax-sdk";
import { afterAll, beforeAll, describe, expect, it } from "vitest";
import { DAEMON_NODE_FLAGS } from "../../src/commands/daemon-control.js";
import { startStack, type Stack } from "./e2e-stack.js";

const enabled = process.env.MEMAX_E2E_SERVER === "1";
if (!enabled) {
  console.log(
    "Skipping the personal layer's end-to-end test: set MEMAX_E2E_SERVER=1 to run it.",
  );
}

const CLI = join(import.meta.dirname, "..", "..");
const BIN = join(CLI, "dist", "bin.js");
const RULES = ".claude/rules/memax-personal.md";
const CODEX = ".codex/AGENTS.md";
const CANON = ".memax/personal/AGENTS.md";
const FACT = "Answer in British English, and keep replies short.";
/** Machine B's own Codex file: lines of the person's, no final newline. */
const MINE_B = "# My Codex notes\n\n- Prefer rg to grep.\n- Run tests first.";

interface Home {
  name: string;
  dir: string;
  daemon?: ChildProcess;
}

// Timeouts: the setup builds the CLI and Go binaries and starts the stack
// (minutes on a cold machine); each step waits up to 60 s per home, as the
// acceptance check allows, so its bound is a multiple of that.
describe.skipIf(!enabled)("the personal layer on several machines", () => {
  let stack: Stack;
  let memax: Memax;
  let base: string;
  let personal: string;
  const a: Home = { name: "machine-a", dir: "" };
  const b: Home = { name: "machine-b", dir: "" };
  const c: Home = { name: "machine-c", dir: "" };

  const env = (h: Home) => ({
    ...process.env,
    HOME: h.dir,
    MEMAX_API_URL: stack.apiUrl,
    MEMAX_API_KEY: "",
    XDG_CONFIG_HOME: "",
    CLAUDE_CONFIG_DIR: "",
    CODEX_HOME: "",
    CI: "",
  });
  /** The built CLI, without blocking the loop (the stack's logs must drain). */
  const cli = (h: Home, ...args: string[]) =>
    new Promise<{ code: number | null; out: string }>((resolve) => {
      const child = spawn(process.execPath, [BIN, ...args], {
        cwd: h.dir,
        env: env(h),
      });
      let out = "";
      child.stdout.on("data", (c) => (out += String(c)));
      child.stderr.on("data", (c) => (out += String(c)));
      child.once("exit", (code) => resolve({ code, out }));
    });
  /** Codex's session-start hook, as Codex runs it, outside any repository. */
  const hook = (h: Home) =>
    new Promise<string>((resolve) => {
      const child = spawn(
        process.execPath,
        [BIN, "hook", "session-start", "--agent", "codex"],
        { cwd: h.dir, env: env(h) },
      );
      let out = "";
      child.stdout.on("data", (c) => (out += String(c)));
      child.once("exit", () => resolve(out));
      child.stdin.end(
        JSON.stringify({
          session_id: randomUUID(),
          cwd: h.dir,
          source: "startup",
        }),
      );
    });
  const read = (h: Home, path: string) => {
    try {
      return readFileSync(join(h.dir, path), "utf8");
    } catch {
      return null;
    }
  };
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
      await new Promise((r) => setTimeout(r, 50));
    }
  }
  /** Every file under a home, as text (what a grep sees). */
  function everything(dir: string): string {
    let all = "";
    for (const e of readdirSync(dir, { withFileTypes: true })) {
      const p = join(dir, e.name);
      if (e.isDirectory()) all += everything(p);
      else if (e.isFile() && statSync(p).size < 4 << 20)
        all += readFileSync(p, "utf8");
    }
    return all;
  }

  beforeAll(async () => {
    execFileSync(
      process.execPath,
      [join(CLI, "node_modules", "typescript", "bin", "tsc"), "-p", CLI],
      { stdio: "inherit" },
    );
    stack = await startStack();
    memax = new Memax({ apiUrl: stack.apiUrl, apiKey: stack.token });
    // ZZ's Personal space, on the V2 record (signing in makes it; devseed
    // signs nobody in).
    personal = randomUUID();
    execFileSync("psql", [
      stack.dbUrl,
      "-v",
      "ON_ERROR_STOP=1",
      "-qc",
      `INSERT INTO hubs (id, name, slug, hub_type, owner_id, space_kind, v2_enabled_at)
         VALUES ('${personal}', 'Personal', 'zz-personal', 'personal', '${stack.zz}', 'personal', now());
       INSERT INTO hub_members (hub_id, user_id, role) VALUES ('${personal}', '${stack.zz}', 'owner');`,
    ]);
    base = realpathSync(mkdtempSync(join(tmpdir(), "memax-e2e-personal-")));
    for (const h of [a, b, c]) {
      h.dir = join(base, h.name);
      for (const d of [".memax", ".claude", ...(h === c ? [] : [".codex"])])
        mkdirSync(join(h.dir, d), { recursive: true });
      writeFileSync(
        join(h.dir, ".memax", "credentials.json"),
        JSON.stringify({
          access_token: stack.token,
          refresh_token: "",
          expires_at: Date.now() + 7_200_000,
        }),
        { mode: 0o600 },
      );
    }
    writeFileSync(join(b.dir, CODEX), MINE_B);
  }, 600_000);

  afterAll(async () => {
    for (const h of [a, b]) h.daemon?.kill("SIGTERM");
    await stack?.stop();
    if (base) rmSync(base, { recursive: true, force: true });
  }, 60_000);

  it("links both machines' homes, each delivering its first files", async () => {
    for (const h of [a, b]) {
      const r = await cli(h, "link", "--home", "--yes", "--name", h.name);
      if (r.code !== 0) console.log(r.out, stack.logs.slice(-30).join("\n"));
      expect(r.code).toBe(0);
      expect(r.out).toContain("~/.claude/rules/memax-personal.md");
      expect(r.out).toContain(`Linked this machine`);
      const link = JSON.parse(
        readFileSync(join(h.dir, ".memax", "daemon", "home.json"), "utf8"),
      );
      expect(link.files.map((f: { path: string }) => f.path).sort()).toEqual(
        [RULES, CANON, CODEX].sort(),
      );
      expect(read(h, RULES)).toContain(
        "The person you work with keeps these in Memax.",
      );
      expect(statSync(join(h.dir, RULES)).mode & 0o777).toBe(0o600);
    }
    // B's own lines stay as they were, the block after them.
    expect(
      read(b, CODEX)!.startsWith(`${MINE_B}\n<!-- memax:start -->\n`),
    ).toBe(true);
    const machines = (await memax.v2.machines.list(personal)).items;
    expect(machines.map((m) => m.name).sort()).toEqual([
      "machine-a",
      "machine-b",
    ]);
    expect(new Set(machines.map((m) => m.device_id)).size).toBe(2);

    for (const h of [a, b]) {
      h.daemon = spawn(
        process.execPath,
        [...DAEMON_NODE_FLAGS, BIN, "daemon", "run"],
        { env: env(h), stdio: "ignore" },
      );
    }
    // Each machine's first Codex session: the baseline the next one is told against.
    for (const h of [a, b]) {
      await until(
        () => existsSync(join(h.dir, ".memax", "daemon", "daemon.pid")),
        10_000,
        `${h.name}'s daemon`,
      );
      await hook(h);
    }
  }, 180_000);

  let ref = "";
  it("a kept personal fact reaches both homes within 60 s, the person's lines untouched", async () => {
    const t0 = Date.now();
    const r = await memax.v2.memories.remember(
      personal,
      { statement: FACT, section: "preferences" },
      { idempotencyKey: randomUUID() },
    );
    expect(r.outcome).toBe("applied");
    ref = r.memory!.ref;
    const line = `${FACT} [${ref}]`;
    for (const h of [a, b]) {
      for (const path of [RULES, CODEX]) {
        await until(
          () => !!read(h, path)?.includes(line),
          60_000,
          `${ref} in ${h.name}'s ~/${path}`,
        );
      }
    }
    console.log(`Keep → both homes on disk: ${Date.now() - t0} ms`);
    expect(Date.now() - t0).toBeLessThan(60_000);
    expect(
      read(b, CODEX)!.startsWith(`${MINE_B}\n<!-- memax:start -->\n`),
    ).toBe(true);
    // A had no Codex file: it holds only Memax's block.
    expect(read(a, CODEX)!.startsWith("<!-- memax:start -->\n")).toBe(true);

    // The next Codex session on each machine is told the new line, once.
    for (const h of [a, b]) {
      const told = await hook(h);
      expect(told).toContain('<memax-context space="zz-personal">');
      expect(told).toContain(line);
      expect(await hook(h)).toBe("");
    }
    // Their loads, reported by each daemon, count as reads of the fact.
    await until(
      async () =>
        ((await memax.v2.memories.get(ref, { space: personal })).reads?.reads ??
          0) >= 2,
      15_000,
      `the home loads counted as reads of ${ref}`,
    );
  }, 120_000);

  it("Forget takes it out of both homes within 60 s, and a grep finds nothing", async () => {
    const { memory } = await memax.v2.memories.get(ref, { space: personal });
    const t0 = Date.now();
    await memax.v2.memories.forget(
      ref,
      {},
      {
        space: personal,
        idempotencyKey: randomUUID(),
        ifMatch: memory.version,
      },
    );
    for (const h of [a, b]) {
      for (const path of [RULES, CODEX, CANON]) {
        await until(
          () => !read(h, path)?.includes(FACT),
          60_000,
          `${ref} out of ${h.name}'s ~/${path}`,
        );
      }
    }
    console.log(`Forget → out of both homes: ${Date.now() - t0} ms`);
    for (const h of [a, b]) expect(everything(h.dir)).not.toContain(FACT);
    // And none of Postgres holds them: every table's rows, as pg_dump has them.
    const dump = execFileSync("pg_dump", ["--data-only", stack.dbUrl], {
      maxBuffer: 256 << 20,
    }).toString();
    expect(dump.length).toBeGreaterThan(0);
    expect(dump).not.toContain(FACT);
    // The tombstone names each machine's files, every one rewritten.
    await until(
      async () => {
        const ts = await memax.v2.memories.tombstone(ref, { space: personal });
        const homes = ts.steps.filter((s) => s.kind === "target");
        return (
          homes.length === 6 &&
          homes.every((s) => s.status === "done") &&
          new Set(homes.map((s) => s.target?.machine?.name)).size === 2
        );
      },
      30_000,
      "every machine's files rewritten in the tombstone",
    );
    expect(
      read(b, CODEX)!.startsWith(`${MINE_B}\n<!-- memax:start -->\n`),
    ).toBe(true);
  }, 120_000);

  it("memax init --yes --home outside a repository links the home; memax connect adds Codex's file", async () => {
    const init = await cli(
      c,
      "init",
      "--yes",
      "--home",
      "--no-daemon",
      "--no-connect",
      "--wait",
      "2",
      "--format",
      "json",
    );
    if (init.code !== 0)
      console.log(init.out, stack.logs.slice(-30).join("\n"));
    expect(init.code).toBe(0);
    expect(JSON.parse(init.out).home).toEqual({ linked: true });
    expect(read(c, RULES)).toContain(
      "The person you work with keeps these in Memax.",
    );
    expect(existsSync(join(c.dir, ".codex"))).toBe(false);

    // Codex is set up on the machine later: connect adds its file and writes it.
    mkdirSync(join(c.dir, ".codex"));
    const connect = await cli(
      c,
      "connect",
      "codex",
      "--no-hook",
      "--format",
      "json",
    );
    if (connect.code !== 0)
      console.log(connect.out, stack.logs.slice(-30).join("\n"));
    const r = JSON.parse(connect.out);
    expect(r.home).toMatchObject({
      state: "added",
      path: "~/.codex/AGENTS.md",
      compile: "compiled",
    });
    expect(read(c, CODEX)!.startsWith("<!-- memax:start -->\n")).toBe(true);
    const link = JSON.parse(
      readFileSync(join(c.dir, ".memax", "daemon", "home.json"), "utf8"),
    );
    expect(link.files.map((f: { path: string }) => f.path).sort()).toEqual(
      [CANON, CODEX, RULES].sort(),
    );
  }, 180_000);

  it("memax unlink --home gives machine B's files back as they were", async () => {
    const r = await cli(b, "unlink", "--home");
    expect(r.code).toBe(0);
    expect(read(b, CODEX)).toBe(MINE_B);
    expect(existsSync(join(b.dir, RULES))).toBe(false);
    expect(existsSync(join(b.dir, CANON))).toBe(false);
    expect(existsSync(join(b.dir, ".memax", "daemon", "home.json"))).toBe(
      false,
    );
    const machines = (await memax.v2.machines.list(personal)).items;
    expect(machines.find((m) => m.name === "machine-b")?.linked).toBe(false);
    expect(machines.find((m) => m.name === "machine-a")?.linked).toBe(true);
  }, 60_000);

  it("memax logout on machine A takes its layer off and keeps home.json", async () => {
    expect(read(a, RULES)).not.toBeNull();
    const r = await cli(a, "logout");
    expect(r.out).toContain("Took your Personal space off this machine's home");
    expect(existsSync(join(a.dir, RULES))).toBe(false);
    expect(existsSync(join(a.dir, CODEX))).toBe(false);
    expect(existsSync(join(a.dir, ".memax", "daemon", "home.json"))).toBe(true);
  }, 60_000);
});
