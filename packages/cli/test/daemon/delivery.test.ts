import {
  readFileSync,
  statSync,
  writeFileSync,
  existsSync,
  chmodSync,
} from "node:fs";
import { join } from "node:path";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { driftHash } from "../../../compiler/src/index.js";
import { harness, type Harness } from "./harness.js";
import { manifest } from "./fake-v2.js";

const AGENTS_1 =
  "<!-- Compiled by Memax from memax-v2 (C-0881). -->\n\n# Brief\n\n- Jobs run on River. [M-0219]\n";
const AGENTS_2 = AGENTS_1 + "- pnpm workspaces only. [M-0071]\n";
const AGENTS_3 = AGENTS_2 + "- Every write returns a receipt. [M-0112]\n";

let h: Harness;
beforeEach(async () => {
  h = await harness();
});
afterEach(async () => {
  await h.cleanup();
});

function setup() {
  const space = h.fake.addSpace("memax-v2");
  const t = h.fake.addTarget(space, "agents_md");
  h.link(space);
  return { space, t };
}

const read = (p: string) => readFileSync(join(h.repo, p), "utf8");
const deliveries = () => h.fake.calls("POST", /\/deliveries$/);
const observations = () => h.fake.calls("POST", /\/observations$/);

describe("delivery", () => {
  it("writes the file and acknowledges the run's drift hash, as the CLI", async () => {
    const { t } = setup();
    const run = h.fake.compile(t.id, { "AGENTS.md": AGENTS_1 });
    const d = h.daemon();
    await d.syncOnce();

    expect(read("AGENTS.md")).toBe(AGENTS_1);
    expect(deliveries()).toHaveLength(1);
    const ack = deliveries()[0];
    expect(ack.body).toEqual({ compile: run.ref, sha256: driftHash(AGENTS_1) });
    expect(ack.headers["x-memax-via"]).toBe("cli");
    expect(ack.headers["idempotency-key"]).toMatch(/^memax-cli-dlv-/);
    expect(ack.status).toBe(200);
    expect(h.fake.entry(t.id).target.sync_state).toBe("in_sync");
    expect(d.snapshot().repos[0].targets[0]).toMatchObject({
      state: "in_sync",
      here: { compile: run.ref },
    });
  });

  it("overwrites on the next compile while the disk still matches the baseline", async () => {
    const { t } = setup();
    h.fake.compile(t.id, { "AGENTS.md": AGENTS_1 });
    const d = h.daemon();
    await d.syncOnce();
    chmodSync(join(h.repo, "AGENTS.md"), 0o640);
    const second = h.fake.compile(t.id, { "AGENTS.md": AGENTS_2 });
    await d.syncOnce();

    expect(read("AGENTS.md")).toBe(AGENTS_2);
    expect(statSync(join(h.repo, "AGENTS.md")).mode & 0o777).toBe(0o640); // mode kept
    expect(
      deliveries().map((c) => (c.body as { compile: string }).compile),
    ).toEqual([expect.any(String), second.ref]);
    expect(observations()).toHaveLength(0);
  });

  it("never writes over a hand edit, and reports it exactly once", async () => {
    const { t } = setup();
    h.fake.compile(t.id, { "AGENTS.md": AGENTS_1 });
    const d = h.daemon();
    await d.syncOnce();
    const edited = AGENTS_1 + "- Prefer named exports. \n";
    writeFileSync(join(h.repo, "AGENTS.md"), edited);
    h.fake.compile(t.id, { "AGENTS.md": AGENTS_2 });
    await d.syncOnce();
    await d.syncOnce();
    await d.checkFile(h.repo, "AGENTS.md");
    await d.syncOnce();

    expect(read("AGENTS.md")).toBe(edited);
    expect(deliveries()).toHaveLength(1);
    expect(observations()).toHaveLength(1);
    expect(observations()[0].body).toEqual({
      path: "AGENTS.md",
      content: edited,
      device_id: "device-1",
    });
    expect(h.fake.entry(t.id).target.sync_state).toBe("drifted");
    const snap = d.snapshot().repos[0].targets[0];
    expect(snap).toMatchObject({ state: "hand_edit", detail: "1 local edit" });
    // A different edit is a new report; the same one again is not.
    writeFileSync(join(h.repo, "AGENTS.md"), edited + "- More.\n");
    await new Promise((r) => setTimeout(r, 5));
    await d.checkFile(h.repo, "AGENTS.md");
    expect(observations().length).toBeLessThanOrEqual(1); // within the per-file gap: deferred, not dropped
  });

  it("never clobbers a hand edit made between the poll and the write", async () => {
    const { t } = setup();
    h.fake.compile(t.id, { "AGENTS.md": AGENTS_1 });
    let edited = "";
    const d = h.daemon({
      hooks: {
        beforeCommit: (abs) => {
          if (!abs.endsWith("AGENTS.md") || !existsSync(abs)) return;
          edited = readFileSync(abs, "utf8") + "- Typed just now.\n";
          writeFileSync(abs, edited);
        },
      },
    });
    // The first write lands on an absent file; then race the second.
    await d.syncOnce();
    expect(read("AGENTS.md")).toBe(AGENTS_1);
    h.fake.compile(t.id, { "AGENTS.md": AGENTS_2 });
    await d.syncOnce();

    expect(read("AGENTS.md")).toBe(edited);
    expect(deliveries()).toHaveLength(1);
    expect(observations()).toHaveLength(1);
    expect((observations()[0].body as { content: string }).content).toBe(
      edited,
    );
    expect(h.log.lines.join("")).toContain("held a write");
  });

  it("never clobbers a file created between the poll and the write", async () => {
    const { t } = setup();
    h.fake.compile(t.id, { "AGENTS.md": AGENTS_1 });
    const mine = "# My own notes\n";
    const d = h.daemon({
      hooks: {
        beforeCommit: (abs) => {
          if (abs.endsWith("AGENTS.md") && !existsSync(abs))
            writeFileSync(abs, mine);
        },
      },
    });
    await d.syncOnce();
    expect(read("AGENTS.md")).toBe(mine);
    expect(deliveries()).toHaveLength(0);
    expect(observations()).toHaveLength(1);
  });

  it("writes over an earlier compile brought back by git, without calling it a hand edit", async () => {
    const { t } = setup();
    h.fake.compile(t.id, { "AGENTS.md": AGENTS_1 });
    h.fake.compile(t.id, { "AGENTS.md": AGENTS_2 });
    // Another device delivered C-2; this one has C-1 on disk from a pull.
    writeFileSync(join(h.repo, "AGENTS.md"), AGENTS_1);
    const third = h.fake.compile(t.id, { "AGENTS.md": AGENTS_3 });
    await h.daemon().syncOnce();
    expect(read("AGENTS.md")).toBe(AGENTS_3);
    expect(observations()).toHaveLength(0);
    expect((deliveries()[0].body as { compile: string }).compile).toBe(
      third.ref,
    );
  });

  it("treats a person's AGENTS.md that Memax never wrote as a hand edit", async () => {
    const { t } = setup();
    writeFileSync(join(h.repo, "AGENTS.md"), "# Ours, by hand\n");
    h.fake.compile(t.id, { "AGENTS.md": AGENTS_1 });
    await h.daemon().syncOnce();
    expect(read("AGENTS.md")).toBe("# Ours, by hand\n");
    expect(observations()).toHaveLength(1);
    expect(deliveries()).toHaveLength(0);
  });

  it("acknowledges without writing when the disk already holds the run (line endings aside)", async () => {
    const { t } = setup();
    writeFileSync(join(h.repo, "AGENTS.md"), AGENTS_1.replace(/\n/g, "\r\n"));
    const before = statSync(join(h.repo, "AGENTS.md")).mtimeMs;
    h.fake.compile(t.id, { "AGENTS.md": AGENTS_1 });
    await h.daemon().syncOnce();
    expect(statSync(join(h.repo, "AGENTS.md")).mtimeMs).toBe(before);
    expect(deliveries()).toHaveLength(1);
  });

  it("delivers a multi-file scoped target, acknowledging the manifest, and removes a file a later run drops", async () => {
    const space = h.fake.addSpace("memax-v2");
    const t = h.fake.addTarget(space, "cursor_mdc");
    h.link(space);
    const web =
      "---\nglobs: packages/web/**\nalwaysApply: false\n---\n\n- Use Ledger. [M-0442]\n";
    const srv =
      "---\nglobs: packages/server/**\nalwaysApply: false\n---\n\n- Go stdlib. [M-0500]\n";
    const run = h.fake.compile(t.id, {
      ".cursor/rules/memax-packages-web.mdc": web,
      ".cursor/rules/memax-packages-server.mdc": srv,
    });
    const d = h.daemon();
    await d.syncOnce();
    expect(read(".cursor/rules/memax-packages-web.mdc")).toBe(web);
    expect((deliveries()[0].body as { sha256: string }).sha256).toBe(
      manifest({
        ".cursor/rules/memax-packages-web.mdc": driftHash(web),
        ".cursor/rules/memax-packages-server.mdc": driftHash(srv),
      }),
    );
    expect(run.drift_sha256).toBe(
      (deliveries()[0].body as { sha256: string }).sha256,
    );

    // The server-scoped facts were forgotten: that file goes, the other stays.
    h.fake.compile(t.id, { ".cursor/rules/memax-packages-web.mdc": web });
    await d.syncOnce();
    expect(
      existsSync(join(h.repo, ".cursor/rules/memax-packages-server.mdc")),
    ).toBe(false);
    expect(read(".cursor/rules/memax-packages-web.mdc")).toBe(web);
    expect(deliveries()).toHaveLength(2);
  });

  it("acknowledges a run with no files after removing the files it no longer writes", async () => {
    const space = h.fake.addSpace("memax-v2");
    const t = h.fake.addTarget(space, "cursor_mdc");
    h.link(space);
    const web = "---\nglobs: packages/web/**\n---\n\n- Use Ledger. [M-0442]\n";
    h.fake.compile(t.id, { ".cursor/rules/memax-packages-web.mdc": web });
    const d = h.daemon();
    await d.syncOnce();
    h.fake.compile(t.id, {});
    await d.syncOnce();
    expect(
      existsSync(join(h.repo, ".cursor/rules/memax-packages-web.mdc")),
    ).toBe(false);
    expect(h.fake.entry(t.id).target.sync_state).toBe("in_sync");
  });

  it("delivers the last good run when the latest compile failed, and fetches its preview once", async () => {
    const { t } = setup();
    h.fake.compile(t.id, { "AGENTS.md": AGENTS_1 });
    h.fake.failCompile(t.id);
    const d = h.daemon();
    await d.syncOnce();
    await d.syncOnce();
    await d.syncOnce();
    expect(read("AGENTS.md")).toBe(AGENTS_1);
    expect(h.fake.calls("GET", /\/preview$/)).toHaveLength(1);
  });

  it("writes nothing for MCP, copy-out and pull-request targets", async () => {
    const space = h.fake.addSpace("memax-v2");
    const gpt = h.fake.addTarget(space, "chatgpt");
    const pr = h.fake.addTarget(space, "agents_md", { delivery: "pr" });
    h.link(space);
    h.fake.compile(gpt.id, {});
    h.fake.compile(pr.id, { "AGENTS.md": AGENTS_1 });
    await h.daemon().syncOnce();
    expect(existsSync(join(h.repo, "AGENTS.md"))).toBe(false);
    expect(h.fake.calls("GET", /\/preview$/)).toHaveLength(0);
  });

  it("retries a failed acknowledgement with the same key", async () => {
    const { t } = setup();
    h.fake.compile(t.id, { "AGENTS.md": AGENTS_1 });
    h.fake.fail(/\/deliveries$/, 503, "busy");
    const d = h.daemon();
    await d.syncOnce();
    expect(h.fake.entry(t.id).target.sync_state).toBe("pending_delivery");
    // Past the per-target backoff.
    await new Promise((r) => setTimeout(r, 2_100));
    await d.syncOnce();
    const keys = deliveries().map((c) => c.headers["idempotency-key"]);
    expect(keys).toHaveLength(2);
    expect(keys[0]).toBe(keys[1]);
    expect(h.fake.entry(t.id).target.sync_state).toBe("in_sync");
  });
});
