// A pulled hand edit holds its file until each proposal it wrote is kept
// or rejected; the daemon never writes over a held file, and against a
// server that doesn't say, it holds a pulled edit to be safe.
import { readFileSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { harness, type Harness } from "./harness.js";

let h: Harness;
beforeEach(async () => {
  h = await harness();
});
afterEach(async () => {
  await h.cleanup();
});

const agents = () => readFileSync(join(h.repo, "AGENTS.md"), "utf8");
const deliveries = () => h.fake.calls("POST", /\/deliveries$/);
const previews = () => h.fake.calls("GET", /\/preview$/);

/** Delivers C1, edits AGENTS.md by hand, reports it, and pulls it. */
async function pulled() {
  const space = h.fake.addSpace("memax-v2");
  const t = h.fake.addTarget(space, "agents_md");
  h.link(space);
  h.fake.compile(t.id, { "AGENTS.md": "# Brief\n\n- pnpm only. [M-0071]\n" });
  const d = h.daemon();
  await d.syncOnce();
  const edited =
    "# Brief\n\n- pnpm only; never npm. [M-0071]\n- Prefer named exports.\n";
  writeFileSync(join(h.repo, "AGENTS.md"), edited);
  await d.checkFile(h.repo, "AGENTS.md");
  await h.memax.v2.targets.pull(t.id, {}, { idempotencyKey: "pull-1" });
  return { t, d, edited };
}

describe("a file a pull holds", () => {
  it("is never written while its proposals wait, and says so", async () => {
    const { t, d, edited } = await pulled();
    h.fake.hold(t.id, ["M-0450", "M-0451"]);
    // A Keep elsewhere compiles a new run: it waits.
    h.fake.dirty(t.id);
    h.fake.compile(t.id, {
      "AGENTS.md": "# Brief\n\n- pnpm only. [M-0071]\n- A new fact. [M-0452]\n",
    });
    h.fake.entry(t.id).target.sync_state = "held";
    const before = previews().length;
    await d.syncOnce();
    await d.syncOnce();
    expect(agents()).toBe(edited);
    expect(previews()).toHaveLength(before); // nothing fetched to write
    expect(deliveries()).toHaveLength(1);
    expect(d.snapshot().repos[0].targets[0]).toMatchObject({
      state: "held",
      detail: "holding for 2 proposals in Review",
    });
  });

  it("is written over once the hold lifts, even with no new compile", async () => {
    const { t, d, edited } = await pulled();
    h.fake.hold(t.id, ["M-0450"]);
    await d.syncOnce();
    expect(agents()).toBe(edited);
    // Rejected: the delivered run is due over the edit, and the rejected
    // line goes.
    h.fake.lift(t.id);
    await d.syncOnce();
    expect(agents()).toBe("# Brief\n\n- pnpm only. [M-0071]\n");
    expect(h.fake.entry(t.id).target.sync_state).toBe("in_sync");
    expect(
      h.fake.entry(t.id).target.delivered?.files[0].observation,
    ).toBeUndefined();
  });

  it("holds a pulled edit against a server that doesn't say", async () => {
    const { t, d, edited } = await pulled();
    // An older server: the accepted edit carries no `held`, and the
    // delivered run is still named (a pull).
    const target = h.fake.entry(t.id).target;
    delete target.delivered!.files[0].held;
    h.fake.dirty(t.id);
    h.fake.compile(t.id, { "AGENTS.md": "# Brief, recompiled\n" });
    h.fake.entry(t.id).target.sync_state = "pending_delivery";
    await d.syncOnce();
    expect(agents()).toBe(edited);
    expect(d.snapshot().repos[0].targets[0].state).toBe("held");
  });

  it("shows in memax status as waiting on you", async () => {
    const { t } = await pulled();
    h.fake.hold(t.id, ["M-0450", "M-0451"]);
    const { targetRows } = await import("../../src/commands/v2-output.js");
    // eslint-disable-next-line no-control-regex
    const row = targetRows([h.fake.entry(t.id).target])[0].replace(
      /\x1b\[[0-9;]*m/g,
      "",
    );
    expect(row).toMatch(
      /○ AGENTS\.md\s+held\s+holding for 2 proposals in Review/,
    );
  });

  it("writes over an overwritten edit against a server that doesn't say", async () => {
    const space = h.fake.addSpace("memax-v2");
    const t = h.fake.addTarget(space, "agents_md");
    h.link(space);
    h.fake.compile(t.id, { "AGENTS.md": "# Brief\n" });
    const d = h.daemon();
    await d.syncOnce();
    writeFileSync(join(h.repo, "AGENTS.md"), "# Mine\n");
    await d.checkFile(h.repo, "AGENTS.md");
    await h.memax.v2.targets.overwrite(t.id, {}, { idempotencyKey: "ow-1" });
    const target = h.fake.entry(t.id).target;
    delete target.delivered!.files[0].held; // an older server
    h.fake.compile(t.id, { "AGENTS.md": "# Brief, again\n" });
    await d.syncOnce();
    expect(agents()).toBe("# Brief, again\n");
  });
});
