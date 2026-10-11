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
  bytesContain,
  CheckReporter,
  RepoChecker,
  repoKeyOf,
  type CheckTiming,
} from "../../src/lib/daemon/checks.js";
import { containsText } from "../../src/lib/daemon/compiler/conditions/evaluate.js";
import {
  globMatcher,
  globPrefix,
} from "../../src/lib/daemon/compiler/conditions/glob.js";
import { memoryLogger } from "../../src/lib/daemon/log.js";
import { daemonPaths } from "../../src/lib/daemon/paths.js";
import { installHookRoutes, tombstoneFixture } from "../hook/fake-hook.js";
import { harness, type Harness } from "./harness.js";

// The checks' git ignores pathspec settings it inherits: with this, a bare
// manifest name's `:(glob)` pathspec would match nothing, and every
// dependency would read as gone.
process.env.GIT_LITERAL_PATHSPECS = "1";

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

/** One condition a memory, of any kind, armed. */
function kinds(
  ...items: Array<[string, Record<string, unknown>]>
): V2.SpaceConditions {
  return {
    space_id: "space-1",
    repository: "MemaxLabs/memax",
    items: items.map(([memory, c], i) => ({
      memory,
      conditions: [
        {
          condition: { id: "c1", ...c, origin: "person" } as V2.Condition,
          hash: H(i + 1),
          armed: true,
        },
      ],
    })),
  };
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

  it("match text on a blob's bytes as the evaluator does on its text", () => {
    const hays = [
      "const TierHaiku = 1",
      "TierHaiku",
      "xTierHaiku TierHaiku_2",
      "é TierHaiku€",
      "naïve TierHaikué",
      "(TierHaiku)\nTierHaikuX",
      "中文TierHaiku中文",
      "",
    ];
    for (const hay of hays)
      for (const needle of ["TierHaiku", "é", "Tier", "中文", "u€"])
        for (const word of [false, true])
          expect(
            bytesContain(Buffer.from(hay, "utf8"), needle, word),
            `${hay} ~ ${needle} ${word}`,
          ).toBe(containsText(hay, needle, word));
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

  it("looks for a rename from Memax's latest hold when it saw none itself", async () => {
    const { root } = repository({
      "packages/server/internal/ask/config.go": "package ask\n// TierHaiku\n",
      "docs/old.md": "# Old\n",
    });
    // Another machine saw them hold here.
    const held = git(root, "rev-parse", "HEAD");
    git(
      root,
      "mv",
      "packages/server/internal/ask/config.go",
      "packages/server/internal/ask/tiers.go",
    );
    git(root, "rm", "-q", "docs/old.md");
    git(root, "commit", "-q", "-m", "move one, remove one");
    git(root, "push", "-q", "origin", "main");
    const conds = conditions(
      ["M-0001", { path: "packages/server/internal/ask/config.go" }],
      ["M-0002", { path: "docs/old.md" }],
      ["M-0003", { path: "never/there.go" }],
      ["M-0004", { path: "gone/elsewhere.go" }],
    );
    conds.items[0].conditions[0].held_commit = held;
    conds.items[1].conditions[0].held_commit = held;
    // Never held anywhere: gone is removed, whatever happened to it.
    conds.items[2].conditions[0].armed = false;
    // Held at a commit this clone doesn't have: where it went can't be said.
    conds.items[3].conditions[0].held_commit = "f".repeat(40);
    const fake = fakeApi(conds);
    // This machine's first run: no hold of its own to start from.
    const { c } = checker(root, fake);
    const run = await c.run("conditions");
    // The tips, the paths, and one rename lookup for each commit held at.
    expect(run.cost?.processes).toBe(4);
    expect(
      fake.reports[0].input.results.map((r) => [
        r.memory,
        r.outcome,
        r.evidence,
      ]),
    ).toEqual([
      [
        "M-0001",
        "fails",
        {
          change: "renamed",
          path: "packages/server/internal/ask/config.go",
          renamed_to: "packages/server/internal/ask/tiers.go",
        },
      ],
      ["M-0002", "fails", { change: "removed", path: "docs/old.md" }],
      ["M-0003", "fails", { change: "removed", path: "never/there.go" }],
      ["M-0004", "unknown", undefined],
    ]);
    expect(fake.reports[0].input.complete).toBe(false);
  });

  it("checks file_contains, dep_present and file_unchanged at the commit, results only", async () => {
    // config.go is large enough that reading it again would show.
    const filler = "// tiers\n".repeat(4_000);
    const { root } = repository({
      "packages/server/internal/ask/config.go": `package ask\n${filler}const Default = TierHaiku\n`,
      "go.mod":
        "module github.com/MemaxLabs/memax\n\nrequire (\n\tgithub.com/riverqueue/river v0.14.2\n)\n",
      "tools/go.mod": "module tools\n",
      "packages/web/package.json": JSON.stringify({
        dependencies: { "@tanstack/react-query": "^5.62.0" },
      }),
      "docs/adr/004-queues.md": "# Queues\n\nRiver.\n",
    });
    const adr = git(root, "rev-parse", "HEAD:docs/adr/004-queues.md");
    const fake = fakeApi(
      kinds(
        [
          "M-0001",
          {
            kind: "file_contains",
            path: "packages/server/internal/ask/config.go",
            text: "TierHaiku",
            word: true,
          },
        ],
        [
          "M-0002",
          {
            kind: "dep_present",
            manifest: "go.mod",
            name: "github.com/riverqueue/river",
          },
        ],
        [
          "M-0003",
          {
            kind: "dep_present",
            manifest: "packages/web/package.json",
            name: "@tanstack/react-query",
            range: "^5",
          },
        ],
        [
          "M-0004",
          { kind: "file_unchanged", path: "docs/adr/004-queues.md", blob: adr },
        ],
      ),
    );
    const { c, paths } = checker(root, fake);

    const first = await c.run("conditions");
    // The tips, one listing (for the bare go.mod), one cat-file.
    expect(first).toMatchObject({ ran: true, results: 4, sent: 4 });
    expect(first.cost?.processes).toBe(3);
    const r1 = fake.reports[0].input;
    expect(r1.results.map((r) => [r.memory, r.outcome])).toEqual([
      ["M-0001", "holds"],
      ["M-0002", "holds"],
      ["M-0003", "holds"],
      ["M-0004", "holds"],
    ]);
    expect(r1.complete).toBe(true);

    // The same commit again: the same blobs, so nothing is read but their ids.
    const again = await c.run("verify");
    expect(again).toMatchObject({ ran: true, sent: 0 });
    expect(again.cost!.bytes).toBeLessThan(2_000);

    // A push changes each: the identifier, the require, the constraint, the ADR.
    write(root, {
      "packages/server/internal/ask/config.go": `package ask\n${filler}const Default = TierHaikuV2\n`,
      "go.mod": "module github.com/MemaxLabs/memax\n",
      "packages/web/package.json": JSON.stringify({
        dependencies: { "@tanstack/react-query": "^4.36.1" },
      }),
      "docs/adr/004-queues.md": "# Queues\n\nRiver, still.\n",
    });
    git(root, "commit", "-q", "-am", "change what they rely on");
    git(root, "push", "-q", "origin", "main");
    const moved = await c.run("head_moved");
    expect(moved).toMatchObject({ ran: true, sent: 4 });
    expect(moved.cost?.processes).toBe(3);
    expect(
      fake.reports[1].input.results.map((r) => [
        r.memory,
        r.outcome,
        r.evidence,
      ]),
    ).toEqual([
      [
        "M-0001",
        "fails",
        {
          change: "text_missing",
          path: "packages/server/internal/ask/config.go",
        },
      ],
      ["M-0002", "fails", { change: "dependency_removed", path: "go.mod" }],
      [
        "M-0003",
        "fails",
        {
          change: "constraint_changed",
          path: "packages/web/package.json",
          constraint: "^4.36.1",
        },
      ],
      [
        "M-0004",
        "fails",
        { change: "blob_changed", path: "docs/adr/004-queues.md" },
      ],
    ]);

    // Nothing a file holds, nor a condition's text, leaves the machine or
    // sits in the daemon's files.
    const sent = JSON.stringify(fake.reports);
    const saved = readdirSync(paths.checks)
      .filter((n) => n.endsWith(".json"))
      .map((n) => readFileSync(join(paths.checks, n), "utf8"))
      .join("\n");
    for (const s of [sent, saved]) {
      expect(s).not.toContain("TierHaiku");
      expect(s).not.toContain("// tiers");
      expect(s).not.toContain("River");
      expect(s).not.toContain("module ");
    }
  });

  it("a file over its cap, a kind it doesn't check and a file that moved", async () => {
    const { root } = repository({
      "docs/big.md": "x".repeat(4_096),
      "docs/adr/004-queues.md": "# Queues\n\nRiver is the queue, everywhere.\n",
    });
    const adr = git(root, "rev-parse", "HEAD:docs/adr/004-queues.md");
    const conds = kinds(
      ["M-0001", { kind: "file_contains", path: "docs/big.md", text: "x" }],
      [
        "M-0002",
        { kind: "file_unchanged", path: "docs/adr/004-queues.md", blob: adr },
      ],
      [
        "M-0003",
        { kind: "pr_state", repo: "MemaxLabs/memax", number: 7, state: "open" },
      ],
    );
    const fake = fakeApi(conds);
    const { c } = checker(root, fake, { ...QUIET, fileCap: 1_024 });
    await c.run("conditions");
    const r = fake.reports[0].input;
    // pr_state is GitHub's: not checked here, so the run isn't complete.
    expect(r.results.map((x) => [x.memory, x.outcome])).toEqual([
      ["M-0001", "unknown"],
      ["M-0002", "holds"],
    ]);
    expect(r.complete).toBe(false);

    git(root, "mv", "docs/adr/004-queues.md", "docs/adr/0004-queues.md");
    git(root, "commit", "-q", "-m", "renumber");
    git(root, "push", "-q", "origin", "main");
    await c.run("head_moved");
    expect(fake.reports[1].input.results).toEqual([
      {
        memory: "M-0002",
        condition: "c1",
        hash: H(2),
        outcome: "fails",
        evidence: {
          change: "renamed",
          path: "docs/adr/004-queues.md",
          renamed_to: "docs/adr/0004-queues.md",
        },
      },
    ]);
  });

  it("finds a bare manifest name anywhere, and says which one held", async () => {
    const { root } = repository({
      "package.json": JSON.stringify({ devDependencies: { turbo: "^2" } }),
      "packages/sdk/package.json": JSON.stringify({ dependencies: {} }),
      "packages/web/package.json": JSON.stringify({
        dependencies: { "@tanstack/react-query": "^5.62.0" },
      }),
      "requirements.txt": "-r base.txt\nflask==3.0\n",
    });
    const fake = fakeApi(
      kinds(
        [
          "M-0001",
          {
            kind: "dep_present",
            manifest: "package.json",
            name: "@tanstack/react-query",
          },
        ],
        // Another file it includes may list it: unknown, never gone.
        [
          "M-0002",
          {
            kind: "dep_present",
            manifest: "requirements.txt",
            name: "requests",
          },
        ],
      ),
    );
    const { c } = checker(root, fake);
    await c.run("conditions");
    expect(fake.reports[0].input.results.map((x) => x.outcome)).toEqual([
      "holds",
      "unknown",
    ]);
    git(root, "rm", "-q", "packages/web/package.json");
    git(root, "commit", "-q", "-m", "drop web");
    git(root, "push", "-q", "origin", "main");
    await c.run("head_moved");
    // The manifest it held in is gone, and no other declares it.
    expect(fake.reports[1].input.results).toEqual([
      {
        memory: "M-0001",
        condition: "c1",
        hash: H(1),
        outcome: "fails",
        evidence: { change: "removed", path: "packages/web/package.json" },
      },
    ]);
  });

  it("drops a forgotten memory's conditions and results at once, and checks it no more", async () => {
    const { root } = repository({
      "packages/server/internal/ask/config.go": "package ask\n",
      "go.mod": "module x\n",
    });
    // M-0001's file is gone: its result's evidence names the path.
    const fake = fakeApi(
      conditions(
        ["M-0001", { path: "packages/server/internal/ask/tiers.go" }],
        ["M-0002", { path: "go.mod" }],
      ),
    );
    const { c, paths } = checker(root, fake);
    await c.run("conditions");
    const state = () =>
      readdirSync(paths.checks)
        .filter((n) => n.endsWith(".json"))
        .map((n) => readFileSync(join(paths.checks, n), "utf8"))
        .join("\n");
    expect(state()).toContain("M-0001");
    expect(state()).toContain("packages/server/internal/ask");

    // The warm cache hears of the Forget: nothing of M-0001 is left on
    // disk, before any run.
    c.forget(["M-0001"]);
    expect(state()).not.toContain("M-0001");
    expect(state()).toContain("M-0002/c1");
    // The conditions in memory went with it: the next run reads them again,
    // and a list that still has it (read before the Forget) is passed over.
    const reads = fake.conditionReads;
    const run = await c.run("verify");
    expect(fake.conditionReads).toBe(reads + 1);
    expect(run.results).toBe(1);
    expect(state()).not.toContain("M-0001");
  });

  it("drops a forgotten memory's results from the reports waiting to go", async () => {
    const { root } = repository({
      "go.mod": "module x\n",
      "docs/a.md": "# A\n",
    });
    const fake = fakeApi(
      conditions(
        ["M-0001", { path: "docs/a.md" }],
        ["M-0002", { path: "go.mod" }],
      ),
    );
    const { c, reporter, paths } = checker(root, fake);
    fake.fail = 0; // offline: the report waits
    await c.run("conditions");
    const queue = join(paths.checks, "queue");
    const [before] = readdirSync(queue);
    expect(readFileSync(join(queue, before), "utf8")).toContain("M-0001");

    reporter.forget("space-1", ["m-0001"]);
    const [after] = readdirSync(queue);
    // Rewritten without it, under the key of what it says now.
    expect(after).not.toBe(before);
    const waiting = readFileSync(join(queue, after), "utf8");
    expect(waiting).not.toContain("M-0001");
    expect(waiting).not.toContain("docs/a.md");
    // Another space's reports aren't touched; one only about the memory goes.
    reporter.forget("space-2", ["M-0002"]);
    expect(readdirSync(queue)).toEqual([after]);
    reporter.forget("space-1", ["M-0002"]);
    expect(readdirSync(queue)).toEqual([]);
    await reporter.flush();
    expect(fake.reports).toHaveLength(0);
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

  it("drops a memory's checks as soon as the warm cache hears it was forgotten", async () => {
    h = await harness();
    const st = installHookRoutes(h.fake);
    const sp = h.fake.addSpace("memax-v2");
    const { root } = repository({ "go.mod": "module x\n" });
    // M-0001's file is gone: its result's evidence names the path.
    const conds = conditions(
      ["M-0001", { path: "docs/adr.md" }],
      ["M-0002", { path: "go.mod" }],
    );
    conds.space_id = sp.id;
    let runs = 0;
    h.fake.extra.push((method, path) => {
      if (method === "GET" && path === `/v2/spaces/${sp.id}/conditions`)
        return { status: 200, data: conds };
      if (method === "POST" && path === `/v2/spaces/${sp.id}/check-runs`) {
        runs++;
        return { status: 201, data: { run: { id: "run-1" }, flagged: [] } };
      }
      return null;
    });
    h.link(sp, root);
    // A minute between runs, as the daemon has it: the drop can't wait for one.
    const d = h.daemon({
      warmRefreshMs: 20,
      checks: { firstMs: 0, statMs: 3_600_000, minGapMs: 60_000 },
    });
    await d.start();
    await until(() => runs === 1, 10_000, "a check run");
    const state = () =>
      readdirSync(h.paths.checks)
        .filter((n) => n.endsWith(".json"))
        .map((n) => readFileSync(join(h.paths.checks, n), "utf8"))
        .join("\n");
    expect(state()).toContain("docs/adr.md");

    // M-0001 is forgotten: its tombstone, and the receipt that moves the feed.
    st.tombstones.set(sp.id, [tombstoneFixture(sp, "M-0001", new Date())]);
    h.fake.receipts++;
    await until(() => !state().includes("M-0001"), 5_000, "the drop");
    expect(state()).not.toContain("docs/adr.md");
    expect(state()).toContain("M-0002");
    expect(runs).toBe(1);

    // Unlinked, its check state goes, where no later Forget would reach it.
    const { unlinkRepo } = await import("../../src/lib/daemon/registry.js");
    unlinkRepo(h.paths, root);
    d.reload();
    await until(() => state() === "", 5_000, "the state file to go");
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
