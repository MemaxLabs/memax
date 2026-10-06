// End to end against the real Go server (the Phase 1 gate, plan 25 §12):
// seed memax-v2, link a temp repository with the built CLI, run the
// daemon, Keep, and see AGENTS.md change on disk within 10 s; then edit
// AGENTS.md by hand, see the target drift with the right changes, and pull
// them back as proposals.
//
// It needs Go, Postgres (a role that can CREATE DATABASE), psql and the
// workspace's node_modules, so it runs only with MEMAX_E2E_SERVER=1:
//
//   MEMAX_E2E_SERVER=1 pnpm --filter memax-cli exec vitest run test/daemon/e2e-server.test.ts
//
// MEMAX_E2E_DATABASE_URL (or TEST_DATABASE_URL) picks the Postgres;
// MEMAX_E2E_BIN a directory of prebuilt server, worker, migrate and devseed.
import { execFileSync, spawn, type ChildProcess } from "node:child_process";
import { randomUUID } from "node:crypto";
import {
  mkdirSync,
  mkdtempSync,
  readFileSync,
  realpathSync,
  rmSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { Memax, type V2 } from "memax-sdk";
import { afterAll, beforeAll, describe, expect, it } from "vitest";
import { DAEMON_NODE_FLAGS } from "../../src/commands/daemon-control.js";
import { startStack, type Stack } from "./e2e-stack.js";

const enabled = process.env.MEMAX_E2E_SERVER === "1";
if (!enabled) {
  console.log(
    "Skipping the end-to-end test against the real server: set MEMAX_E2E_SERVER=1 to run it.",
  );
}

const CLI = join(import.meta.dirname, "..", "..");
const BIN = join(CLI, "dist", "bin.js");
/** How long the idle measurement runs (MEMAX_E2E_IDLE_MS, default a minute). */
const IDLE_MS = Number(process.env.MEMAX_E2E_IDLE_MS ?? 60_000);

describe.skipIf(!enabled)("the daemon against the real server", () => {
  let stack: Stack;
  let memax: Memax;
  let home: string;
  let repo: string;
  let daemon: ChildProcess;
  const env = () => ({
    ...process.env,
    HOME: home,
    MEMAX_API_URL: stack.apiUrl,
    MEMAX_API_KEY: "",
    XDG_CONFIG_HOME: "",
  });
  const cli = (...args: string[]) =>
    execFileSync(process.execPath, [BIN, ...args], {
      cwd: repo,
      env: env(),
    }).toString();
  const agentsMd = () => readFileSync(join(repo, "AGENTS.md"), "utf8");
  const targets = async () => (await memax.v2.targets.list("memax-v2")).items;
  const target = async (kind: V2.TargetKind) =>
    (await targets()).find((t) => t.kind === kind)!;

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
  const has = (text: string) => () => {
    try {
      return agentsMd().includes(text);
    } catch {
      return false;
    }
  };
  const settled = async () =>
    (await targets()).every(
      (t) => t.sync_state === "in_sync" || t.sync_state === "off",
    );

  beforeAll(async () => {
    execFileSync(
      process.execPath,
      [join(CLI, "node_modules", "typescript", "bin", "tsc"), "-p", CLI],
      { stdio: "inherit" },
    );
    stack = await startStack();
    memax = new Memax({ apiUrl: stack.apiUrl, apiKey: stack.token });
    const base = realpathSync(mkdtempSync(join(tmpdir(), "memax-e2e-")));
    home = join(base, "home");
    repo = join(base, "repo");
    mkdirSync(join(home, ".memax"), { recursive: true });
    mkdirSync(repo);
    writeFileSync(
      join(home, ".memax", "credentials.json"),
      JSON.stringify({
        access_token: stack.token,
        refresh_token: "",
        expires_at: Date.now() + 7_200_000,
      }),
      { mode: 0o600 },
    );
    execFileSync("git", ["init", "-q", repo]);
  }, 600_000);

  afterAll(async () => {
    daemon?.kill("SIGTERM");
    await stack?.stop();
    if (home) rmSync(join(home, ".."), { recursive: true, force: true });
  }, 60_000);

  it("links, delivers the first compile, and a Keep reaches AGENTS.md within 10 s", async () => {
    expect(cli("link", "--space", "memax-v2")).toContain("Linked");
    expect(readFileSync(join(repo, ".memax.yml"), "utf8")).toContain(
      "space: memax-v2",
    );

    daemon = spawn(
      process.execPath,
      [...DAEMON_NODE_FLAGS, BIN, "daemon", "run"],
      { env: env(), stdio: "ignore" },
    );
    const first = await until(settled, 60_000, "the first compile on disk");
    console.log(`first compile of every target on disk: ${first} ms`);
    expect(agentsMd()).toContain(
      "Background jobs run on River, Postgres-backed. We do not use Temporal. [M-0219]",
    );
    expect(readFileSync(join(repo, "CLAUDE.md"), "utf8")).toContain(
      "@AGENTS.md",
    );
    expect(
      readFileSync(join(repo, ".cursor/rules/memax-packages-web.mdc"), "utf8"),
    ).toContain("alwaysApply: false");

    // Keep → the file on disk, measured from the request.
    const samples: number[] = [];
    let t0 = Date.now();
    await memax.v2.memories.keep(
      "M-0430",
      {},
      { space: "memax-v2", idempotencyKey: randomUUID() },
    );
    samples.push(await until(has("[M-0430]"), 10_000, "M-0430 in AGENTS.md"));
    expect(Date.now() - t0).toBeLessThan(10_000);
    for (let i = 1; i <= 4; i++) {
      await until(settled, 15_000, "the space to settle");
      const statement = `End-to-end sample ${i}: the daemon writes every Keep within seconds.`;
      t0 = Date.now();
      const r = await memax.v2.memories.remember(
        "memax-v2",
        { statement, section: "conventions" },
        { idempotencyKey: randomUUID() },
      );
      expect(r.outcome).toBe("applied");
      samples.push(
        await until(has(statement), 10_000, `sample ${i} in AGENTS.md`),
      );
    }
    console.log(
      `Keep → AGENTS.md on disk (ms): ${samples.join(", ")}; max ${Math.max(...samples)}`,
    );
    expect(Math.max(...samples)).toBeLessThan(10_000);
    await until(settled, 15_000, "every target in sync");
  }, 180_000);

  it("reports a hand edit, never writes over it, and pulls it back as proposals", async () => {
    const before = agentsMd();
    let edited = before.replace(
      "- pnpm workspaces only. Never run `npm install` at the root. [M-0071]",
      "- pnpm workspaces only. Never run `npm install` or `yarn` anywhere. [M-0071]",
    );
    edited = edited.replace(
      "- Every write tool returns a receipt ID the caller can cite. [M-0112]\n",
      "",
    );
    edited = edited.replace(
      "## Open\n",
      "## Open\n\n- Prefer named exports in React components.\n",
    );
    expect(edited).not.toBe(before);
    writeFileSync(join(repo, "AGENTS.md"), edited);

    const agents = await target("agents_md");
    await until(
      async () => (await target("agents_md")).sync_state === "drifted",
      10_000,
      "AGENTS.md to drift",
    );
    const drift = await memax.v2.targets.drift(agents.id);
    expect(drift.items).toHaveLength(1);
    expect(drift.items[0].observed).toBe(edited);
    const kinds = drift.items[0].observation.changeset.changes.map(
      (c) => `${c.kind}:${c.ref ?? ""}`,
    );
    expect(kinds).toEqual(["edit:M-0071", "new:", "remove:M-0112"]);
    expect(drift.items[0].observation.observer_kind).toBe("device");

    // A Keep while it is drifted reaches the other files, never this one.
    const statement = "While AGENTS.md has a hand edit, Memax holds it.";
    await memax.v2.memories.remember(
      "memax-v2",
      { statement, section: "conventions" },
      { idempotencyKey: randomUUID() },
    );
    await new Promise((r) => setTimeout(r, 6_000));
    expect(agentsMd()).toBe(edited);

    const pull = await memax.v2.targets.pull(
      agents.id,
      {},
      { idempotencyKey: randomUUID(), via: "cli" },
    );
    expect(pull.proposals.map((p) => p.statement)).toEqual([
      "pnpm workspaces only. Never run `npm install` or `yarn` anywhere.",
      "Prefer named exports in React components.",
    ]);
    expect(pull.proposals.every((p) => p.state === "proposed")).toBe(true);
    expect(agentsMd()).toBe(edited);
    const [editP, newP] = pull.proposals;

    // Held: the file stays as it is until both are decided, even with the
    // Keep made while it was drifted compiled and waiting.
    expect(pull.target.sync_state).toBe("held");
    expect(pull.target.holds?.[0].proposals).toEqual([editP.ref, newP.ref]);
    await memax.v2.memories.reject(
      newP.id,
      {},
      { space: "memax-v2", idempotencyKey: randomUUID() },
    );
    await new Promise((r) => setTimeout(r, 5_000));
    expect(agentsMd()).toBe(edited);
    expect((await target("agents_md")).sync_state).toBe("held");

    // Keeping the last one lifts the hold; undoing that Keep at once holds
    // the file again, before anything is delivered over the edit.
    const kept = await memax.v2.memories.keep(
      editP.id,
      {},
      { space: "memax-v2", idempotencyKey: randomUUID() },
    );
    await memax.v2.receipts.undo(
      kept.receipts[0].id,
      {},
      { idempotencyKey: randomUUID() },
    );
    expect((await target("agents_md")).sync_state).toBe("held");
    await new Promise((r) => setTimeout(r, 5_000));
    expect(agentsMd()).toBe(edited);

    // Kept for good: the file comes back compiled, with the kept line and
    // the held Keep, and without the rejected line.
    await memax.v2.memories.keep(
      editP.id,
      {},
      { space: "memax-v2", idempotencyKey: randomUUID() },
    );
    await until(
      has(`or \`yarn\` anywhere. [${editP.ref}]`),
      15_000,
      "the pulled edit compiled back",
    );
    await until(has(statement), 15_000, "the held Keep delivered");
    expect(agentsMd()).not.toContain("Prefer named exports");
    await until(
      async () => (await target("agents_md")).sync_state === "in_sync",
      15_000,
      "AGENTS.md in sync",
    );
  }, 120_000);

  it("compiles on request with memax compile, and the daemon writes it", async () => {
    // Asynchronously: a blocked event loop would stop draining the server's
    // and worker's logs, and they would stall on a full pipe.
    const t0 = Date.now();
    const child = spawn(
      process.execPath,
      [BIN, "compile", "--format", "json"],
      { cwd: repo, env: env() },
    );
    let out = "";
    child.stdout.on("data", (b) => (out += String(b)));
    const code = await new Promise<number | null>((r) => child.once("exit", r));
    if (code !== 0)
      console.log(
        `memax compile exited ${code}:\n${out}\n${stack.logs.slice(-40).join("\n")}`,
      );
    const report = JSON.parse(out);
    console.log(
      `memax compile, every target compiled and on disk: ${Date.now() - t0} ms`,
    );
    expect(report).toMatchObject({
      space: "memax-v2",
      writer: "daemon",
      timed_out: false,
    });
    expect(report.requested).toHaveLength(4);
    expect(
      report.targets.every(
        (t: { sync_state: string }) => t.sync_state === "in_sync",
      ),
    ).toBe(true);
  }, 60_000);

  it(
    "shows it all in memax status and memax daemon status, then stops",
    async () => {
      const status = JSON.parse(cli("status", "--format", "json"));
      expect(status.space.slug).toBe("memax-v2");
      expect(status.daemon).toEqual({ running: true, linked_here: true });
      expect(status.agents.map((a: { mark: string }) => a.mark)).toEqual(
        expect.arrayContaining(["CC", "CX"]),
      );
      const ds = JSON.parse(cli("daemon", "status", "--format", "json"));
      expect(ds.running).toBe(true);
      expect(
        ds.repos[0].targets.find(
          (t: { kind: string }) => t.kind === "agents_md",
        ).state,
      ).toBe("in_sync");

      // What one idle poll costs on the wire.
      const auth = { headers: { Authorization: `Bearer ${stack.token}` } };
      const list = await fetch(
        `${stack.apiUrl}/v2/spaces/memax-v2/targets`,
        auth,
      );
      const probe = await fetch(
        `${stack.apiUrl}/v2/spaces/memax-v2/receipts?limit=1`,
        auth,
      );
      console.log(
        `targets list: ${(await list.arrayBuffer()).byteLength} bytes; idle probe: ${(await probe.arrayBuffer()).byteLength} bytes`,
      );

      // Idle cost: memory and CPU over a quiet minute.
      const pid = daemon.pid!;
      const stat = () =>
        readFileSync(`/proc/${pid}/stat`, "utf8").split(") ")[1].split(" ");
      const mem = () => {
        const s = readFileSync(`/proc/${pid}/status`, "utf8");
        const g = (k: string) =>
          Number(s.match(new RegExp(`^${k}:\\s+(\\d+) kB`, "m"))?.[1] ?? 0) /
          1024;
        return `rss ${g("VmRSS").toFixed(1)} MiB (anon ${g("RssAnon").toFixed(1)}, file ${g("RssFile").toFixed(1)})`;
      };
      if (process.platform === "linux") {
        const s1 = stat();
        const t1 = Date.now();
        await new Promise((r) => setTimeout(r, IDLE_MS));
        const s2 = stat();
        const ticks =
          Number(s2[11]) + Number(s2[12]) - Number(s1[11]) - Number(s1[12]);
        const cpu = (ticks / 100 / ((Date.now() - t1) / 1000)) * 100;
        console.log(
          `daemon idle: ${mem()}, cpu ${cpu.toFixed(2)}% over ${Math.round((Date.now() - t1) / 1000)} s`,
        );
        expect(cpu).toBeLessThan(1);
      }
      expect(cli("daemon", "stop")).toContain("Stopped the Memax daemon.");
    },
    120_000 + IDLE_MS,
  );
});
