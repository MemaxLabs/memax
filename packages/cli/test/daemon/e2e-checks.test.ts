// The first slice of stale detection end to end (the stale-detection
// design §7, steps 1 to 4), against the real Go server: a kept memory
// names a file in memax-v2's repository; a clone of MemaxLabs/memax is
// linked and the daemon runs; the file is renamed on main and pushed; the
// daemon's next check finds it gone, the memory turns stale (Memax's flag,
// sourced to the run), every compiled file says "(Being verified)", and
// the stale report says what moved where. Then Verify settles it.
//
// Like e2e-server.test.ts it needs Go, Postgres and psql, so it runs only
// with MEMAX_E2E_SERVER=1:
//
//   MEMAX_E2E_SERVER=1 pnpm --filter memax-cli exec vitest run test/daemon/e2e-checks.test.ts
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
import { Memax } from "memax-sdk";
import { afterAll, beforeAll, describe, expect, it } from "vitest";
import { DAEMON_NODE_FLAGS } from "../../src/commands/daemon-control.js";
import { startStack, type Stack } from "./e2e-stack.js";

const enabled = process.env.MEMAX_E2E_SERVER === "1";
const CLI = join(import.meta.dirname, "..", "..");
const BIN = join(CLI, "dist", "bin.js");
const CITED = "packages/server/internal/ask/config.go";
const MOVED = "packages/server/internal/ask/tiers.go";

const GIT_ENV = {
  ...process.env,
  GIT_AUTHOR_NAME: "Test",
  GIT_AUTHOR_EMAIL: "test@example.com",
  GIT_COMMITTER_NAME: "Test",
  GIT_COMMITTER_EMAIL: "test@example.com",
  GIT_CONFIG_NOSYSTEM: "1",
};

describe.skipIf(!enabled)("stale detection, end to end", () => {
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
  const git = (...args: string[]) =>
    execFileSync("git", args, {
      cwd: repo,
      env: { ...GIT_ENV, HOME: home },
      stdio: "pipe",
    })
      .toString()
      .trim();

  async function until<T>(
    check: () => Promise<T | undefined | false>,
    ms: number,
    what: string,
  ): Promise<{ value: T; ms: number }> {
    const start = Date.now();
    for (;;) {
      const v = await check().catch(() => undefined);
      if (v) return { value: v, ms: Date.now() - start };
      if (Date.now() - start > ms)
        throw new Error(
          `timed out after ${ms} ms waiting for ${what}\n${stack.logs.slice(-30).join("\n")}`,
        );
      await new Promise((r) => setTimeout(r, 250));
    }
  }

  beforeAll(async () => {
    execFileSync(
      process.execPath,
      [join(CLI, "node_modules", "typescript", "bin", "tsc"), "-p", CLI],
      {
        stdio: "inherit",
      },
    );
    stack = await startStack();
    memax = new Memax({ apiUrl: stack.apiUrl, apiKey: stack.token });
    const base = realpathSync(mkdtempSync(join(tmpdir(), "memax-e2e-checks-")));
    home = join(base, "home");
    repo = join(base, "repo");
    mkdirSync(join(home, ".memax"), { recursive: true });
    writeFileSync(
      join(home, ".memax", "credentials.json"),
      JSON.stringify({
        access_token: stack.token,
        refresh_token: "",
        expires_at: Date.now() + 7_200_000,
      }),
      { mode: 0o600 },
    );
    // memax-v2 compiles for MemaxLabs/memax: its clone, with a bare origin
    // named so, on main and pushed.
    const origin = join(base, "MemaxLabs", "memax.git");
    mkdirSync(origin, { recursive: true });
    mkdirSync(join(repo, "packages", "server", "internal", "ask"), {
      recursive: true,
    });
    execFileSync("git", ["init", "-q", "--bare", "-b", "main", origin], {
      env: GIT_ENV,
    });
    execFileSync("git", ["init", "-q", "-b", "main", repo], { env: GIT_ENV });
    writeFileSync(join(repo, CITED), "package ask\n\nconst Tier = TierHaiku\n");
    git("add", "-A");
    git("commit", "-q", "-m", "ask");
    git("remote", "add", "origin", origin);
    git("push", "-q", "-u", "origin", "main");
    git("remote", "set-head", "origin", "main");
    // Building the server, worker and compile service, then migrating and
    // seeding a fresh database, takes minutes on a loaded machine.
  }, 900_000);

  afterAll(async () => {
    daemon?.kill("SIGTERM");
    await stack?.stop();
    if (home) rmSync(join(home, ".."), { recursive: true, force: true });
  }, 60_000);

  it("a renamed cited file marks its memory stale within the daemon's cycle, and Verify settles it", async () => {
    const kept = await memax.v2.memories.remember(
      "memax-v2",
      {
        statement: `Ask's tiers are set in \`${CITED}\`.`,
        section: "conventions",
      },
      { idempotencyKey: randomUUID() },
    );
    expect(kept.outcome).toBe("applied");
    const ref = kept.memory.ref;
    const conds = await memax.v2.checks.conditions("memax-v2");
    const mine = conds.items.find((i) => i.memory === ref);
    expect(mine?.conditions[0].condition).toMatchObject({
      kind: "path_exists",
      path: CITED,
      origin: "derived",
    });

    expect(cli("link", "--space", "memax-v2")).toContain("Linked");
    daemon = spawn(
      process.execPath,
      [...DAEMON_NODE_FLAGS, BIN, "daemon", "run"],
      { env: env(), stdio: "ignore" },
    );

    // The first check arms it: the file is there on main, as pushed.
    const armed = await until(
      async () =>
        (await memax.v2.checks.conditions("memax-v2")).items.find(
          (i) => i.memory === ref,
        )?.conditions[0].armed,
      // The daemon's first run comes as soon as its first poll sees the
      // space: seconds, more on a loaded machine.
      60_000,
      "the condition to arm",
    );
    console.log(`armed ${armed.ms} ms after the daemon started`);

    // The file moves on main, and the move is pushed.
    git("mv", CITED, MOVED);
    git("commit", "-q", "-m", "Ask's tiers move to tiers.go");
    git("push", "-q", "origin", "main");
    const moved = git("rev-parse", "HEAD");
    const stale = await until(
      async () => {
        const m = await memax.v2.memories.get(ref, { space: "memax-v2" });
        return m.memory.state === "stale" ? m : undefined;
      },
      // The daemon stats the refs every 15 s and checks a repository at
      // most once a minute: the cycle is about 75 s at worst.
      120_000,
      `${ref} to turn stale`,
    );
    console.log(`stale ${stale.ms} ms after the push`);
    expect(stale.ms).toBeLessThan(90_000);

    const why = await memax.v2.memories.stale(ref, { space: "memax-v2" });
    expect(why.flagged).toMatchObject({
      action: "flagged",
      actor_kind: "memax",
      via: "system",
    });
    expect(why.flagged?.source?.kind).toBe("check");
    expect(why.flagged?.reason).toContain(MOVED);
    const cause = why.causes.find((c) => c.kind === "check");
    expect(cause).toMatchObject({
      kind: "check",
      branch: "main",
      commit: moved,
      observer: "device",
      evidence: { change: "renamed", path: CITED, renamed_to: MOVED },
    });

    // Every compiled file marks it, and the daemon writes it.
    const markedLine = new RegExp(
      `^- \\(Being verified\\) .*\\[${ref}\\]$`,
      "m",
    );
    const marked = await until(
      // Its own line: the demo has another memory being verified already.
      async () =>
        markedLine.test(readFileSync(join(repo, "AGENTS.md"), "utf8")) ||
        undefined,
      30_000,
      "AGENTS.md to mark it",
    );
    console.log(`AGENTS.md marked ${marked.ms} ms after the flag`);

    // Verify's update: new words, the flag clears, the moved path is
    // what it now relies on.
    const verified = await memax.v2.memories.verify(
      ref,
      { answer: "update", statement: `Ask's tiers are set in \`${MOVED}\`.` },
      {
        space: "memax-v2",
        ifMatch: stale.value.memory.version,
        idempotencyKey: randomUUID(),
      },
    );
    expect(verified.memory.state).toBe("kept");
    const after = (await memax.v2.checks.conditions("memax-v2")).items.find(
      (i) => i.memory === ref,
    );
    expect(
      after?.conditions.map((c) => (c.condition as { path?: string }).path),
    ).toContain(MOVED);
  }, 300_000); // the compile reaching AGENTS.md (half a minute) and Verify. // Arming (up to a minute), the next cycle after the push (up to two),
});
