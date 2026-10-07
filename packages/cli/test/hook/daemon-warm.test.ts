// The daemon's side of the warm start: it keeps ~/.memax/daemon/warm.json
// current (compiles, forgets, waiting gates; refs only), and reports the
// compile loads the hook queues, retrying what it couldn't send.
import { existsSync, readdirSync, readFileSync } from "node:fs";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import type { V2 } from "memax-sdk";
import { enqueueLoads } from "../../src/lib/hook/loads.js";
import { flushOnce } from "../../src/lib/hook/flush.js";
import { readWarm } from "../../src/lib/hook/warm.js";
import { loadKey } from "../../src/lib/daemon/loads.js";
import { harness, until, type Harness } from "../daemon/harness.js";
import { header } from "./fixture.js";
import {
  gateFixture,
  installHookRoutes,
  tombstoneFixture,
  type HookState,
} from "./fake-hook.js";

let h: Harness;
let st: HookState;
let space: V2.Space;
let target: V2.Target;
beforeEach(async () => {
  h = await harness();
  st = installHookRoutes(h.fake);
  space = h.fake.addSpace("acme-web");
  target = h.fake.addTarget(space, "agents_md");
  const run = h.fake.compile(target.id, {
    "AGENTS.md": `${header("C-0881")}\n- Background jobs run on River. [M-0219]\n`,
  });
  run.refs.push("M-0219");
  h.link(space);
});
afterEach(() => h.cleanup());

const warm = () => readWarm(h.paths.warm).spaces[space.id];

describe("the warm-start cache", () => {
  it("holds each linked space's compiles, forgets and waiting gates, and no words", async () => {
    const soon = new Date(Date.now() + 2 * 86_400_000);
    st.gates.set(space.id, [
      gateFixture(
        space,
        "G-0012",
        `Fly.io​ or Railway? ${"x".repeat(400)}`,
        soon,
      ),
      gateFixture(
        space,
        "G-0009",
        "Already over?",
        new Date(Date.now() - 1000),
      ),
    ]);
    st.tombstones.set(space.id, [
      tombstoneFixture(space, "M-0099", new Date(), ["M-0100"]),
      tombstoneFixture(space, "M-0010", new Date(Date.now() - 60 * 86_400_000)),
    ]);
    const d = h.daemon();
    await d.start();
    await until(() => (warm()?.gates.length ?? 0) > 0, 5_000, "the cache");
    const w = warm();
    expect(w.slug).toBe("acme-web");
    expect(w.targets).toEqual([
      expect.objectContaining({
        kind: "agents_md",
        path: "AGENTS.md",
        compile: "C-0881",
        refs: ["M-0219"],
      }),
    ]);
    expect(w.forgotten.map((f) => [f.ref, f.with])).toEqual([
      ["M-0099", ["M-0100"]],
    ]);
    expect(w.gates).toHaveLength(1);
    expect(w.gates[0].question.startsWith("Fly.io or Railway?")).toBe(true);
    expect(w.gates[0].question.length).toBeLessThanOrEqual(240);
    const raw = readFileSync(h.paths.warm, "utf8");
    expect(raw).not.toContain("Background jobs");
    expect(raw).not.toContain("the person's own words");
  });

  it("follows a Forget within a poll, and drops a space that is unlinked", async () => {
    const d = h.daemon({ warmRefreshMs: 20 });
    await d.start();
    await until(() => warm()?.targets.length === 1, 5_000, "the cache");
    st.tombstones.set(space.id, [
      tombstoneFixture(space, "M-0219", new Date()),
    ]);
    h.fake.receipts++; // a Forget writes a receipt: the probe moves
    await until(() => warm()?.forgotten.length === 1, 5_000, "the forget");
    expect(warm().forgotten[0].ref).toBe("M-0219");

    const { unlinkRepo } = await import("../../src/lib/daemon/registry.js");
    unlinkRepo(h.paths, h.repo);
    d.reload();
    await until(() => !warm(), 5_000, "the space to go");
  });

  it("is written by memax compile's one pass too", async () => {
    await h.daemon().syncOnce(space.id);
    expect(warm()?.targets[0]?.compile).toBe("C-0881");
  });
});

describe("compile loads", () => {
  const load = (over: Partial<Parameters<typeof loadKey>[0]> = {}) => ({
    space: space.id,
    compile: "C-0881",
    agent: "claude-code",
    session_ref: "sess-1",
    loaded_at: new Date().toISOString(),
    ...over,
  });

  it("reports what the hook queues, once, soon after", async () => {
    const d = h.daemon();
    await d.start();
    const l = load();
    enqueueLoads(h.paths.loads, [l]);
    await until(() => st.loads.length === 1, 5_000, "the report");
    expect(st.loads[0].body).toEqual({
      compile: "C-0881",
      agent: "claude-code",
      session_ref: "sess-1",
      loaded_at: l.loaded_at,
    });
    const call = h.fake.calls("POST", /compile-loads$/)[0];
    expect(call.headers["idempotency-key"]).toBe(loadKey(l));
    expect(call.headers["x-memax-via"]).toBe("cli");
    await until(
      () =>
        readdirSync(h.paths.loads).filter((n) => !n.startsWith(".")).length ===
        0,
      5_000,
      "the queue to empty",
    );
  });

  it("keeps a load the server can't take now, and drops one it refuses", async () => {
    st.loadStatus = 503;
    enqueueLoads(h.paths.loads, [load()]);
    const flush = () => flushOnce(h.paths, h.api, h.log, () => true);
    expect((await flush())?.retry).toBe(1);
    st.loadStatus = 201;
    expect((await flush())?.recorded).toBe(1);
    st.loadStatus = 403;
    enqueueLoads(h.paths.loads, [load({ session_ref: "sess-2" })]);
    expect((await flush())?.refused).toBe(1);
    expect(
      readdirSync(h.paths.loads).filter((n) => n.endsWith(".json")),
    ).toEqual([]);
  });

  it("drops a load older than the server's day", async () => {
    enqueueLoads(h.paths.loads, [
      load({ loaded_at: new Date(Date.now() - 25 * 3_600_000).toISOString() }),
    ]);
    expect((await flushOnce(h.paths, h.api, h.log, () => true))?.expired).toBe(
      1,
    );
    expect(st.loads).toHaveLength(0);
  });

  it("refreshes the cache of the loads' spaces when no daemon runs", async () => {
    st.gates.set(space.id, [
      gateFixture(
        space,
        "G-0020",
        "Which queue?",
        new Date(Date.now() + 86_400_000),
      ),
    ]);
    enqueueLoads(h.paths.loads, [load()]);
    expect(existsSync(h.paths.warm)).toBe(false);
    await flushOnce(h.paths, h.api, h.log, () => false);
    expect(warm()?.gates.map((g) => g.ref)).toEqual(["G-0020"]);
    expect(warm()?.targets[0]?.compile).toBe("C-0881");
  });
});
