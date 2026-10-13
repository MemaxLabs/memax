// memax check: the daemon's checks of this repository now, through the
// running daemon or in this process, and what each condition found, on a
// real git repository against the fake /v2.
import type { V2 } from "memax-sdk";
import { afterEach, describe, expect, it } from "vitest";
import { check, type CheckDeps } from "../../src/commands/check.js";
import { OneShotDelivery } from "../../src/lib/daemon/oneshot.js";
import { cleanupRepos, git, repository } from "./git-repo.js";
import { harness, type Harness } from "./harness.js";

let h: Harness;
afterEach(async () => {
  await h?.cleanup();
  cleanupRepos();
});

// eslint-disable-next-line no-control-regex
const plain = (lines: string[]) =>
  lines.join("\n").replace(/\x1b\[[0-9;]*m/g, "");
const H = (n: number) => String(n).repeat(64);

/** A linked clone whose space has two memories with conditions on it. */
async function setup() {
  h = await harness();
  const sp = h.fake.addSpace("memax-v2");
  const { root } = repository({
    "docs/adr.md": "# ADR\n",
    "go.mod": "module x\n\nrequire github.com/riverqueue/river v0.11.0\n",
  });
  const conds: V2.SpaceConditions = {
    space_id: sp.id,
    repository: "MemaxLabs/memax",
    items: [
      {
        memory: "M-0001",
        conditions: [
          {
            condition: {
              id: "c1",
              kind: "path_exists",
              path: "docs/adr.md",
              origin: "derived",
              from: "statement",
            },
            hash: H(1),
            armed: true,
          },
        ],
      },
      {
        memory: "M-0002",
        conditions: [
          {
            condition: {
              id: "c1",
              kind: "dep_present",
              manifest: "go.mod",
              name: "github.com/riverqueue/river",
              origin: "person",
            },
            hash: H(2),
            armed: true,
          },
        ],
      },
    ],
  };
  const reports: V2.CheckRunInput[] = [];
  h.fake.extra.push((method, path, _url, body) => {
    if (method === "GET" && path === `/v2/spaces/${sp.id}/conditions`)
      return { status: 200, data: conds };
    if (method === "POST" && path === `/v2/spaces/${sp.id}/check-runs`) {
      const input = body as V2.CheckRunInput;
      reports.push(input);
      const flagged = input.results
        .filter((r) => r.outcome === "fails")
        .map((r) => r.memory);
      return { status: 201, data: { run: { id: "run-1" }, flagged } };
    }
    return null;
  });
  h.link(sp, root);
  return { sp, root, reports };
}

function deps(root: string, out: string[], local = true): CheckDeps {
  return {
    memax: h.memax,
    paths: h.paths,
    cwd: root,
    out: (l) => out.push(l),
    oneShot: local
      ? () =>
          OneShotDelivery.open({
            paths: h.paths,
            api: h.api,
            deviceId: "device-1",
            log: h.log,
            version: "test",
            apiUrl: h.fake.url,
          })
      : async () => {
          throw new Error("the running daemon should have answered");
        },
  };
}

describe("memax check", () => {
  it("checks without a daemon, says what each condition found, and what Memax marked stale", async () => {
    const { root, reports } = await setup();
    const out: string[] = [];
    expect(await check({}, deps(root, out))).toBe(0);
    const head = git(root, "rev-parse", "--short", "HEAD");
    let text = plain(out);
    expect(text).toContain(`→ memax-v2 · main at ${head}, checked`);
    expect(text).toMatch(/● M-0001 {2}docs\/adr\.md exists/);
    expect(text).toMatch(
      /● M-0002 {2}go\.mod lists github\.com\/riverqueue\/river/,
    );
    expect(reports).toHaveLength(1);

    // The cited file goes from main: the next check says so, and Memax marks it.
    git(root, "rm", "-q", "docs/adr.md");
    git(root, "commit", "-qm", "Drop the ADR");
    git(root, "push", "-q", "origin", "main");
    out.length = 0;
    expect(await check({}, deps(root, out))).toBe(0);
    text = plain(out);
    expect(text).toMatch(/○ M-0001 {2}docs\/adr\.md exists/);
    expect(text).toContain(
      "docs/adr.md is gone from main. Memax marked it stale.",
    );
    expect(text).toContain(
      "Memax marked M-0001 stale. Verify it: memax verify M-0001",
    );
    expect(reports[1].trigger).toBe("verify");
    expect(reports[1].results.map((r) => [r.memory, r.outcome])).toEqual([
      ["M-0001", "fails"],
    ]);
  });

  it("asks the running daemon, and reports in JSON", async () => {
    const { root } = await setup();
    const d = h.daemon();
    await d.start();
    const out: string[] = [];
    expect(await check({ format: "json" }, deps(root, out, false))).toBe(0);
    const json = JSON.parse(out.join("\n"));
    expect(json).toMatchObject({
      space: "memax-v2",
      branch: "main",
      run: { ran: true },
      view: { root, commit: git(root, "rev-parse", "HEAD") },
    });
    expect(
      json.view.items.map((i: { memory: string; outcome: string }) => [
        i.memory,
        i.outcome,
      ]),
    ).toEqual([
      ["M-0001", "holds"],
      ["M-0002", "holds"],
    ]);
  });

  it("says why it checks nothing on another branch", async () => {
    const { root, reports } = await setup();
    git(root, "switch", "-q", "-c", "feat/tiers");
    const out: string[] = [];
    expect(await check({}, deps(root, out))).toBe(0);
    expect(plain(out)).toContain(
      "Not checked here: not on the default branch.",
    );
    expect(reports).toHaveLength(0);
  });

  it("says a space on V1 isn't checked", async () => {
    const { sp, root, reports } = await setup();
    h.fake.switchSpace(sp, false);
    const out: string[] = [];
    expect(await check({}, deps(root, out))).toBe(0);
    expect(plain(out)).toContain(
      "Not checked here: the space is on V1 again, and Memax checks only spaces on V2.",
    );
    expect(reports).toHaveLength(0);
  });

  it("points a repository that isn't linked to memax link", async () => {
    h = await harness();
    const { root } = repository({ "a.txt": "a\n" });
    const out: string[] = [];
    expect(await check({}, deps(root, out))).toBe(1);
    expect(plain(out)).toContain(
      "This repository isn't linked, so Memax checks nothing here. Link it: memax link",
    );
  });
});
