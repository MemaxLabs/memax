// memax verify: why a memory is stale, the change in this clone behind a
// check's cause, and the answer (still true, new words, or Forget on the
// web), on a real git repository against the fake /v2.
import type { V2 } from "memax-sdk";
import { afterEach, describe, expect, it } from "vitest";
import { verify, type VerifyDeps } from "../../src/commands/verify.js";
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
const APP = "https://app.example";
const STATEMENT =
  "Ask's model tiers are set in internal/ask/config.go, one per plan.";

const receipt = (action: string, at: string) =>
  ({ id: `r-${action}`, action, occurred_at: at }) as unknown as V2.Receipt;

/**
 * A linked clone where internal/ask/config.go was renamed, and M-0187,
 * which cites it, marked stale for it at the commit after the rename.
 */
async function setup(o: { stale?: boolean; refuse?: string } = {}) {
  h = await harness();
  const sp = h.fake.addSpace("memax-v2");
  sp.v2_enabled_at = "2026-10-01T00:00:00Z";
  sp.repository = "MemaxLabs/memax";
  const { root } = repository({
    "internal/ask/config.go": "package ask\n\nconst Tiers = 3\n",
    "README.md": "# memax\n",
  });
  git(root, "mv", "internal/ask/config.go", "internal/ask/tiers.go");
  git(root, "commit", "-q", "-m", "Split the ask tiers");
  const renamed = git(root, "rev-parse", "--short", "HEAD");
  git(root, "push", "-q", "origin", "main");
  const at = git(root, "rev-parse", "HEAD");
  const report: V2.StaleReport = {
    memory_id: "m-1",
    ref: "M-0187",
    space_id: sp.id,
    version: 2,
    statement: STATEMENT,
    kind: "fact",
    state: "stale",
    stale: o.stale ?? true,
    flagged: receipt("flagged", "2026-09-11T14:02:00Z"),
    kept: receipt("kept", "2026-07-14T09:00:00Z"),
    reads_since: 88,
    causes: [
      {
        kind: "check",
        condition: {
          id: "c1",
          kind: "path_exists",
          path: "internal/ask/config.go",
          origin: "derived",
          from: "statement",
        },
        evidence: {
          change: "renamed",
          path: "internal/ask/config.go",
          renamed_to: "internal/ask/tiers.go",
        },
        branch: "main",
        commit: at,
        commit_at: "2026-09-11T13:00:00Z",
        checked_at: "2026-09-11T14:01:00Z",
        observer: "device",
      },
    ],
  } as V2.StaleReport;
  const verifies: Array<{ body: unknown; headers: Record<string, unknown> }> =
    [];
  h.fake.extra.push((method, path, url, body) => {
    if (
      method === "GET" &&
      path === "/v2/memories/M-0187/stale" &&
      url.searchParams.get("space") === sp.id
    )
      return { status: 200, data: report };
    if (method === "POST" && path === "/v2/memories/M-0187:verify") {
      if (o.refuse)
        return {
          status: 403,
          error: "refused",
          message: "Refused.",
          details: {
            policy: {
              effect: "refuse",
              code: o.refuse,
              message: "Decisions in this team space need a person on the web.",
            },
          },
        };
      return {
        status: 200,
        data: {
          outcome: "applied",
          policy: { effect: "apply" },
          memory: { ref: "M-0187", version: 3 },
          receipts: [receipt("verified", new Date().toISOString())],
        },
      };
    }
    return null;
  });
  h.link(sp, root);
  // What the request carried, read off the fake's log.
  const calls = () => {
    verifies.length = 0;
    for (const c of h.fake.calls("POST", /:verify(\?|$)/))
      verifies.push({ body: c.body, headers: c.headers });
    return verifies;
  };
  return { sp, root, renamed, calls };
}

function deps(
  root: string,
  out: string[],
  answers?: Array<string | ((prefill?: string) => string)>,
): VerifyDeps {
  return {
    memax: h.memax,
    paths: h.paths,
    cwd: root,
    appUrl: APP,
    out: (...l) => out.push(...l),
    ...(answers
      ? {
          ask: async (_q: string, prefill?: string) => {
            const a = answers.shift();
            if (a === undefined) throw new Error("asked more than expected");
            return typeof a === "function" ? a(prefill) : a;
          },
        }
      : {}),
  };
}

describe("memax verify", () => {
  it("says why, shows the change in this clone, and keeps the rename's new words", async () => {
    const { root, renamed, calls } = await setup();
    const out: string[] = [];
    let offered: string | undefined;
    const code = await verify(
      "m-0187",
      {},
      deps(root, out, [
        "2",
        (prefill) => {
          offered = prefill;
          return prefill ?? ""; // Enter keeps the suggestion
        },
      ]),
    );
    expect(code).toBe(0);
    const text = plain(out);
    expect(text).toContain("Verify M-0187 · stale since Sep 11");
    expect(text).toContain(STATEMENT);
    expect(text).toContain("kept · Jul 14 · read 88 times since");
    expect(text).toContain(
      "○ internal/ask/config.go was renamed to internal/ask/tiers.go on main.",
    );
    expect(text).toContain(
      `The change here: ${renamed} Split the ask tiers · Test,`,
    );
    expect(text).toContain("rename from internal/ask/config.go");
    expect(text).toContain("rename to internal/ask/tiers.go");
    expect(text).not.toContain("README");
    expect(text).toContain("Memax suggests new words from the rename.");
    expect(offered).toBe(
      "Ask's model tiers are set in internal/ask/tiers.go, one per plan.",
    );
    expect(text).toContain("✓ Updated M-0187 and verified it.");
    const [sent] = calls();
    expect(sent.body).toEqual({ answer: "update", statement: offered });
    expect(sent.headers["if-match"]).toBe('"2"');
    expect(sent.headers["x-memax-via"]).toBe("cli");
    expect(sent.headers["idempotency-key"]).toBeTruthy();
  });

  it("answers still true from a flag, without a terminal", async () => {
    const { root, calls } = await setup();
    const out: string[] = [];
    expect(
      await verify(
        "M-0187",
        {
          stillTrue: true,
          reason: "Checked: the tiers moved, the fact holds.",
        },
        deps(root, out),
      ),
    ).toBe(0);
    expect(plain(out)).toContain("✓ Verified M-0187. It's kept as it is.");
    expect(calls()[0].body).toEqual({
      answer: "still_true",
      reason: "Checked: the tiers moved, the fact holds.",
    });
  });

  it("sends Forget to the web, and asks nothing to change", async () => {
    const { root, calls } = await setup();
    const out: string[] = [];
    expect(await verify("M-0187", {}, deps(root, out, ["3"]))).toBe(0);
    expect(plain(out)).toContain(
      `Forget it on the web, which shows what forgetting it removes first: ${APP}/memax-v2/memories/M-0187?overlay=verify`,
    );
    const json: string[] = [];
    expect(
      await verify(
        "M-0187",
        { forget: true, format: "json" },
        deps(root, json),
      ),
    ).toBe(0);
    expect(JSON.parse(json.join("\n"))).toMatchObject({
      report: { ref: "M-0187", stale: true },
      forget_on_web: `${APP}/memax-v2/memories/M-0187?overlay=verify`,
    });
    expect(calls()).toHaveLength(0);
  });

  it("leaves it as it is on Enter, and asks again on an answer it doesn't know", async () => {
    const { root, calls } = await setup();
    const out: string[] = [];
    expect(await verify("M-0187", {}, deps(root, out, ["4", ""]))).toBe(0);
    const text = plain(out);
    expect(text).toContain("Choose what's true now: 1, 2 or 3.");
    expect(text).toContain("Left as it is: still being verified.");
    expect(calls()).toHaveLength(0);
  });

  it("refuses words that didn't change, and says how to answer without a terminal", async () => {
    const { root, calls } = await setup();
    const same: string[] = [];
    expect(
      await verify("M-0187", { update: ` ${STATEMENT} ` }, deps(root, same)),
    ).toBe(1);
    expect(plain(same)).toContain(
      "Change the words first, or answer still true.",
    );
    const bare: string[] = [];
    expect(await verify("M-0187", {}, deps(root, bare))).toBe(0);
    expect(plain(bare)).toContain(
      'Answer with --still-true, --update "<what\'s true now>" or --forget.',
    );
    expect(calls()).toHaveLength(0);
  });

  it("says when it isn't stale any more", async () => {
    const { root } = await setup({ stale: false });
    const out: string[] = [];
    expect(await verify("M-0187", { stillTrue: true }, deps(root, out))).toBe(
      0,
    );
    expect(plain(out)).toContain(
      "M-0187 isn't stale any more. Someone verified it, or it changed.",
    );
  });

  it("sends a decision that needs a person on the web there", async () => {
    const { root } = await setup({ refuse: "decision_needs_web" });
    await expect(
      verify("M-0187", { stillTrue: true }, deps(root, [])),
    ).rejects.toThrow(
      `Decisions in this team space need a person on the web. Verify it on the web: ${APP}/memax-v2/memories/M-0187?overlay=verify`,
    );
  });

  it("takes one answer at a time", async () => {
    const { root } = await setup();
    const out: string[] = [];
    expect(
      await verify(
        "M-0187",
        { stillTrue: true, forget: true },
        deps(root, out),
      ),
    ).toBe(2);
    expect(plain(out)).toContain(
      "Give one answer: --still-true, --update or --forget.",
    );
  });
});
