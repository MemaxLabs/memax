// The daemon's checks (lib/daemon/checks.ts) on real git repositories in
// temporary folders, against an in-memory /v2: eligibility (the pushed
// default branch of a clone of the space's repository), what each run
// reads and sends, renames, the queue, the refs watch, and the daemon's
// wiring through the fake server.
import { execFileSync } from "node:child_process";
import {
  mkdirSync,
  mkdtempSync,
  readdirSync,
  readFileSync,
  realpathSync,
  rmSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import type { V2 } from "memax-sdk";
import { afterEach, describe, expect, it } from "vitest";
import type { DaemonApi } from "../../src/lib/daemon/api.js";
import {
  CheckReporter,
  RepoChecker,
  repoKeyOf,
  type CheckTiming,
} from "../../src/lib/daemon/checks.js";
import { globMatcher, globPrefix } from "../../src/lib/daemon/glob.js";
import { memoryLogger } from "../../src/lib/daemon/log.js";
import { daemonPaths } from "../../src/lib/daemon/paths.js";
import { harness, type Harness } from "./harness.js";

const GIT_ENV = {
  ...process.env,
  GIT_AUTHOR_NAME: "Test",
  GIT_AUTHOR_EMAIL: "test@example.com",
  GIT_COMMITTER_NAME: "Test",
  GIT_COMMITTER_EMAIL: "test@example.com",
  GIT_CONFIG_NOSYSTEM: "1",
  HOME: tmpdir(),
};
const git = (cwd: string, ...args: string[]) =>
  execFileSync("git", args, { cwd, env: GIT_ENV, stdio: "pipe" })
    .toString()
    .trim();

const bases: string[] = [];
afterEach(() => {
  for (const b of bases.splice(0)) rmSync(b, { recursive: true, force: true });
});

/** A clone of MemaxLabs/memax (a bare origin named so), on main, pushed. */
function repository(files: Record<string, string>): {
  base: string;
  root: string;
} {
  const base = realpathSync(mkdtempSync(join(tmpdir(), "memax-checks-")));
  bases.push(base);
  const root = join(base, "repo");
  const origin = join(base, "MemaxLabs", "memax.git");
  mkdirSync(root);
  mkdirSync(origin, { recursive: true });
  git(origin, "init", "-q", "--bare", "-b", "main");
  git(root, "init", "-q", "-b", "main");
  write(root, files);
  git(root, "add", "-A");
  git(root, "commit", "-q", "-m", "first");
  git(root, "remote", "add", "origin", origin);
  git(root, "push", "-q", "-u", "origin", "main");
  git(root, "remote", "set-head", "origin", "main");
  return { base, root };
}

function write(root: string, files: Record<string, string>): void {
  for (const [p, content] of Object.entries(files)) {
    mkdirSync(join(root, p, ".."), { recursive: true });
    writeFileSync(join(root, p), content);
  }
}

const H = (n: number) => String(n).repeat(64).slice(0, 64);

function conditions(
  ...items: Array<[string, { path?: string; glob?: string }]>
): V2.SpaceConditions {
  return {
    space_id: "space-1",
    repository: "MemaxLabs/memax",
    items: items.map(([memory, c], i) => ({
      memory,
      conditions: [
        {
          condition: {
            id: "c1",
            kind: "path_exists",
            ...c,
            origin: "derived",
            from: "statement",
          } as V2.Condition,
          hash: H(i + 1),
          armed: true,
        },
      ],
    })),
  };
}

interface Fake {
  api: DaemonApi;
  reports: Array<{ space: string; input: V2.CheckRunInput; key: string }>;
  conditions: V2.SpaceConditions;
  /** The next report fails with this status (0: the network). */
  fail?: number;
  conditionReads: number;
}

function fakeApi(conds: V2.SpaceConditions): Fake {
  const f: Fake = {
    reports: [],
    conditions: conds,
    conditionReads: 0,
    api: {} as DaemonApi,
  };
  f.api = {
    conditions: async () => {
      f.conditionReads++;
      return f.conditions;
    },
    reportChecks: async (
      space: string,
      input: V2.CheckRunInput,
      key: string,
    ) => {
      if (f.fail !== undefined) {
        const status = f.fail;
        f.fail = undefined;
        throw Object.assign(new Error("down"), {
          status,
          code: status ? "invalid_request" : "network_error",
        });
      }
      f.reports.push({ space, input, key });
      return {
        run: { id: `run-${f.reports.length}` } as V2.CheckRun,
        flagged: input.results
          .filter((r) => r.outcome === "fails")
          .map((r) => r.memory),
      };
    },
  } as unknown as DaemonApi;
  return f;
}

const QUIET: Partial<CheckTiming> = {
  statMs: 3_600_000,
  firstMs: 3_600_000,
  minGapMs: 0,
};

function checker(root: string, fake: Fake, timing = QUIET) {
  const base = realpathSync(mkdtempSync(join(tmpdir(), "memax-checks-home-")));
  bases.push(base);
  const paths = daemonPaths(join(base, "daemon"));
  const log = memoryLogger();
  const reporter = new CheckReporter({ paths, api: fake.api, log });
  const c = new RepoChecker({
    link: { root, space_id: "space-1", space_slug: "memax-v2", created_at: "" },
    api: fake.api,
    paths,
    log,
    deviceId: "device-1",
    reporter,
    timing,
  });
  return { c, reporter, paths, log };
}

describe("globs", () => {
  it("match the scope glob syntax and list from their literal folder", () => {
    const cases: Array<[string, string, boolean]> = [
      ["packages/web/**", "packages/web/src/app.tsx", true],
      ["packages/web/**", "packages/webapp/x", false],
      ["**/*.go", "internal/ask/config.go", true],
      ["**/*.go", "config.go", true],
      ["internal/*/config.go", "internal/ask/config.go", true],
      ["internal/*/config.go", "internal/ask/v2/config.go", false],
      ["src/?.ts", "src/a.ts", true],
      ["src/[!a]*.ts", "src/a.ts", false],
      ["src/[ab].ts", "src/b.ts", true],
      ["docs/adr-0+1.md", "docs/adr-0+1.md", true],
    ];
    for (const [glob, path, want] of cases)
      expect(globMatcher(glob)(path), `${glob} ~ ${path}`).toBe(want);
    expect(globPrefix("packages/web/**")).toBe("packages/web");
    expect(globPrefix("**/*.go")).toBe("");
    expect(globPrefix("internal/*/config.go")).toBe("internal");
  });

  it("read a remote's owner/name in every spelling", () => {
    for (const url of [
      "git@github.com:MemaxLabs/memax.git",
      "https://github.com/MemaxLabs/memax",
      "ssh://git@github.com/MemaxLabs/memax.git",
      "/tmp/x/MemaxLabs/memax.git",
    ])
      expect(repoKeyOf(url)).toBe("memaxlabs/memax");
  });
});

describe("a repository's checks", () => {
  it("reports a hold, then a rename, from the pushed default branch: results only", async () => {
    const { root } = repository({
      "packages/server/internal/ask/config.go": "package ask\n// TierHaiku\n",
      "packages/web/src/app.tsx": "export {}\n",
    });
    const fake = fakeApi(
      conditions(
        ["M-0001", { path: "packages/server/internal/ask/config.go" }],
        ["M-0002", { glob: "packages/web/**" }],
        ["M-0003", { path: "never/there.go" }],
      ),
    );
    const { c, paths } = checker(root, fake);

    const first = await c.run("conditions");
    expect(first).toMatchObject({ ran: true, results: 3, sent: 3 });
    // The tips, the paths in one batch, the globs' prefixes in one listing.
    expect(first.cost?.processes).toBe(3);
    const head = git(root, "rev-parse", "HEAD");
    expect(fake.reports[0].input).toMatchObject({
      repository: "MemaxLabs/memax",
      branch: "main",
      commit: head,
      trigger: "conditions",
      device: "device-1",
      complete: true,
    });
    expect(
      fake.reports[0].input.results.map((r) => [r.memory, r.outcome]),
    ).toEqual([
      ["M-0001", "holds"],
      ["M-0002", "holds"],
      ["M-0003", "fails"],
    ]);
    expect(fake.reports[0].input.results[2].evidence).toEqual({
      change: "removed",
      path: "never/there.go",
    });
    // Nothing a file holds leaves the machine, nor sits in the daemon's files.
    const sent = JSON.stringify(fake.reports);
    expect(sent).not.toContain("TierHaiku");
    expect(sent).not.toContain("package ask");
    const state = readdirSync(paths.checks).filter((n) => n.endsWith(".json"));
    expect(state).toHaveLength(1);
    const saved = readFileSync(join(paths.checks, state[0]), "utf8");
    expect(saved).not.toContain("TierHaiku");
    expect(saved).not.toContain("package ask");

    // Nothing moved: no git, no report.
    const still = await c.run("schedule");
    expect(still).toMatchObject({ ran: false, skipped: "unchanged" });
    expect(still.cost?.processes).toBe(0);
    expect(fake.reports).toHaveLength(1);

    // The file moves, and the move is pushed: only what changed goes.
    git(
      root,
      "mv",
      "packages/server/internal/ask/config.go",
      "packages/server/internal/ask/tiers.go",
    );
    git(root, "commit", "-q", "-m", "rename");
    git(root, "push", "-q", "origin", "main");
    const moved = await c.run("head_moved");
    expect(moved).toMatchObject({ ran: true, sent: 1, flagged: ["M-0001"] });
    // A fourth process only because a path is gone: renames since it held.
    expect(moved.cost?.processes).toBe(4);
    expect(fake.reports[1].input.results).toEqual([
      {
        memory: "M-0001",
        condition: "c1",
        hash: H(1),
        outcome: "fails",
        evidence: {
          change: "renamed",
          path: "packages/server/internal/ask/config.go",
          renamed_to: "packages/server/internal/ask/tiers.go",
        },
      },
    ]);
    expect(fake.reports[1].input.commit).toBe(git(root, "rev-parse", "HEAD"));
    expect(fake.reports[1].key).not.toBe(fake.reports[0].key);
  });

  it("never reports from another branch, an unpushed commit, a fork's clone or without origin's default", async () => {
    const { root } = repository({ "go.mod": "module x\n" });
    const fake = fakeApi(conditions(["M-0001", { path: "go.mod" }]));
    const { c, log } = checker(root, fake);

    git(root, "checkout", "-q", "-b", "feature");
    expect(await c.run("head_moved")).toMatchObject({
      ran: false,
      skipped: "other_branch",
    });
    git(root, "checkout", "-q", "main");
    write(root, { "go.mod": "module y\n" });
    git(root, "commit", "-q", "-am", "local");
    expect(await c.run("head_moved")).toMatchObject({
      ran: false,
      skipped: "unpushed",
    });
    git(root, "push", "-q", "origin", "main");
    git(
      root,
      "remote",
      "set-url",
      "origin",
      "git@github.com:someone/memax.git",
    );
    expect(await c.run("head_moved")).toMatchObject({
      ran: false,
      skipped: "fork",
    });
    git(
      root,
      "remote",
      "set-url",
      "origin",
      join(root, "..", "MemaxLabs", "memax.git"),
    );
    git(root, "remote", "set-head", "origin", "-d");
    expect(await c.run("head_moved")).toMatchObject({
      ran: false,
      skipped: "no_default_branch",
    });
    git(root, "checkout", "-q", "--detach");
    expect(await c.run("head_moved")).toMatchObject({
      ran: false,
      skipped: "detached",
    });
    expect(fake.reports).toHaveLength(0);
    // Each reason once in the log, never anything a file holds.
    const lines = log.lines.filter((l) =>
      l.includes("not checking a repository"),
    );
    expect(lines.map((l) => JSON.parse(l.split("why=")[1]) as string)).toEqual([
      "not on the default branch",
      "the default branch has commits that aren't pushed",
      "origin isn't the space's repository",
      "origin's default branch isn't known (git remote set-head origin --auto)",
      "HEAD is detached",
    ]);
  });

  it("queues a report it couldn't send and sends it later, as it was", async () => {
    const { root } = repository({ "go.mod": "module x\n" });
    const fake = fakeApi(conditions(["M-0001", { path: "go.mod" }]));
    const { c, reporter, paths } = checker(root, fake);
    fake.fail = 0; // offline
    const run = await c.run("conditions");
    expect(run).toMatchObject({ ran: true, sent: 1 });
    expect(run.flagged).toBeUndefined();
    const queue = join(paths.checks, "queue");
    expect(readdirSync(queue)).toHaveLength(1);
    // Nothing changed since: the next run doesn't report it again.
    expect(await c.run("schedule")).toMatchObject({ skipped: "unchanged" });
    await reporter.flush();
    expect(fake.reports).toHaveLength(1);
    expect(readdirSync(queue)).toHaveLength(0);
    expect(fake.reports[0].input.results[0].outcome).toBe("holds");

    // One Memax refuses is dropped, and said once.
    write(root, { "other.go": "package x\n" });
    git(root, "add", "-A");
    git(root, "commit", "-q", "-m", "more");
    git(root, "push", "-q", "origin", "main");
    fake.conditions = conditions(
      ["M-0001", { path: "go.mod" }],
      ["M-0002", { path: "other.go" }],
    );
    c.conditionsChanged();
    fake.fail = 400;
    await c.run("conditions");
    expect(readdirSync(queue)).toHaveLength(0);
    expect(fake.reports).toHaveLength(1);
  });

  it("says unknown for a glob over too many paths, and isn't complete", async () => {
    const { root } = repository({ "a/1.go": "", "a/2.go": "", "b.go": "" });
    const fake = fakeApi(
      conditions(["M-0001", { glob: "a/*.go" }], ["M-0002", { path: "b.go" }]),
    );
    const { c } = checker(root, fake, { ...QUIET, globCap: 1 });
    await c.run("conditions");
    const r = fake.reports[0].input;
    expect(r.complete).toBe(false);
    expect(r.results.map((x) => x.outcome)).toEqual(["unknown", "holds"]);
  });

  it("keeps its schedule: a run asked for out of turn doesn't bring the next one forward", async () => {
    const { root } = repository({ "go.mod": "module x\n" });
    const fake = fakeApi(conditions(["M-0001", { path: "go.mod" }]));
    const { c } = checker(root, fake, {
      statMs: 3_600_000,
      firstMs: 3_600_000,
      scheduleMs: 3_600_000,
      minGapMs: 0,
    });
    c.start();
    try {
      await c.run("conditions");
      const first = c.lastRun;
      await new Promise((r) => setTimeout(r, 300));
      expect(c.lastRun).toBe(first);
      expect(fake.conditionReads).toBe(1);
    } finally {
      await c.stop();
    }
  });

  it("watches the refs: still refs run no git, a push wakes it", async () => {
    const { root } = repository({ "go.mod": "module x\n" });
    const fake = fakeApi(conditions(["M-0001", { path: "go.mod" }]));
    const { c } = checker(root, fake, {
      statMs: 20,
      firstMs: 0,
      minGapMs: 0,
    });
    c.start();
    try {
      await until(() => fake.reports.length === 1, 5_000, "the first run");
      const first = c.lastRun;
      await new Promise((r) => setTimeout(r, 300));
      expect(c.lastRun).toBe(first); // no run while nothing moved
      git(root, "mv", "go.mod", "go.work");
      git(root, "commit", "-q", "-m", "move");
      git(root, "push", "-q", "origin", "main");
      await until(
        () => fake.reports.length === 2,
        5_000,
        "the run after the push",
      );
      expect(fake.reports[1].input.trigger).toBe("head_moved");
      expect(fake.reports[1].input.results[0].evidence).toMatchObject({
        change: "renamed",
        renamed_to: "go.work",
      });
    } finally {
      await c.stop();
    }
  });
});

describe("the daemon's checks", () => {
  let h: Harness;
  afterEach(async () => {
    await h?.cleanup();
  });

  it("checks a linked repository beside its delivery, and reports through /v2", async () => {
    h = await harness();
    const sp = h.fake.addSpace("memax-v2");
    // The linked folder is a clone of MemaxLabs/memax.
    const { root } = repository({ "go.mod": "module x\n" });
    const conds = conditions(["M-0001", { path: "go.mod" }]);
    conds.space_id = sp.id;
    const runs: V2.CheckRunInput[] = [];
    h.fake.extra.push((method, path, _url, body) => {
      if (method === "GET" && path === `/v2/spaces/${sp.id}/conditions`)
        return { status: 200, data: conds };
      if (method === "POST" && path === `/v2/spaces/${sp.id}/check-runs`) {
        runs.push(body as unknown as V2.CheckRunInput);
        return {
          status: 201,
          data: { run: { id: "run-1" }, flagged: [] },
        };
      }
      return null;
    });
    h.link(sp, root);
    const d = h.daemon({ checks: { firstMs: 0, statMs: 20, minGapMs: 0 } });
    await d.start();
    await until(() => runs.length === 1, 10_000, "a check run");
    expect(runs[0]).toMatchObject({ branch: "main", device: "device-1" });
    expect(runs[0].results).toEqual([
      { memory: "M-0001", condition: "c1", hash: H(1), outcome: "holds" },
    ]);
    const report = h.fake.calls("POST", /check-runs$/)[0];
    expect(report.headers["idempotency-key"]).toMatch(/^memax-cli-chk-/);
    expect(report.headers["x-memax-via"]).toBe("cli");
    // The space's feed moving again only asks for the conditions: nothing
    // changed, so nothing more is sent.
    await new Promise((r) => setTimeout(r, 300));
    expect(runs).toHaveLength(1);
    expect(d.checker(root)?.lastRun?.skipped ?? "ran").not.toBe("error");
  });
});

async function until(
  check: () => boolean,
  ms: number,
  what: string,
): Promise<void> {
  const start = Date.now();
  while (!check()) {
    if (Date.now() - start > ms)
      throw new Error(`timed out waiting for ${what}`);
    await new Promise((r) => setTimeout(r, 10));
  }
}
