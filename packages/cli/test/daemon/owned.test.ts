// A CLAUDE.md the person owns: Memax manages one block in it and leaves
// every byte outside the markers alone.
import { readFileSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { blockOf } from "./fake-v2.js";
import { harness, type Harness } from "./harness.js";

let h: Harness;
beforeEach(async () => {
  h = await harness();
});
afterEach(async () => {
  await h.cleanup();
});

const BEFORE = "# My Claude notes\r\n\r\nAlways run the linter.\r\n";
const AFTER = "\r\n## Mine, below the block\r\n";
const SHIM_1 = [
  "<!-- Compiled by Memax from memax-v2 (C-0881). -->",
  "@AGENTS.md",
];
const SHIM_2 = [...SHIM_1, "", "- Claude-only: use plan mode first. [M-0300]"];

function setup() {
  const space = h.fake.addSpace("memax-v2");
  const t = h.fake.addTarget(space, "claude_md", { user_owned: true });
  h.link(space);
  return t;
}
const file = () => join(h.repo, "CLAUDE.md");
const read = () => readFileSync(file(), "utf8");

describe("a user-owned CLAUDE.md", () => {
  it("inserts, updates and removes only the managed block", async () => {
    const t = setup();
    writeFileSync(file(), BEFORE);
    h.fake.compile(t.id, { "CLAUDE.md": SHIM_1 });
    const d = h.daemon();
    await d.syncOnce();

    // Insert: appended after a blank line, in the file's own line endings.
    const inserted = read();
    expect(inserted.startsWith(BEFORE)).toBe(true);
    expect(blockOf(inserted)).toBe(
      ["<!-- memax:start -->", ...SHIM_1, "<!-- memax:end -->", ""].join("\n"),
    );
    expect(inserted).toContain("\r\n<!-- memax:start -->\r\n");
    expect(h.fake.entry(t.id).target.sync_state).toBe("in_sync");

    // The person writes below the block; an update keeps it byte for byte.
    writeFileSync(file(), inserted + AFTER);
    h.fake.compile(t.id, { "CLAUDE.md": SHIM_2 });
    await d.syncOnce();
    const updated = read();
    expect(updated.startsWith(BEFORE)).toBe(true);
    expect(updated.endsWith(AFTER)).toBe(true);
    expect(blockOf(updated)).toContain("use plan mode first");
    expect(h.fake.calls("POST", /\/observations$/)).toHaveLength(0);

    // Stop compiling: the block goes, everything else stays exactly.
    h.fake.setState(t.id, "off");
    await d.syncOnce();
    const removed = read();
    expect(blockOf(removed)).toBeNull();
    expect(removed).toBe(BEFORE + "\r\n" + AFTER);
    expect(d.snapshot().repos[0].targets[0].state).toBe("off_block_removed");
    await d.syncOnce();
    expect(read()).toBe(removed);
  });

  it("judges drift by the block only, and reports an edit inside it", async () => {
    const t = setup();
    writeFileSync(file(), BEFORE);
    h.fake.compile(t.id, { "CLAUDE.md": SHIM_1 });
    const d = h.daemon();
    await d.syncOnce();
    const edited = read().replace(
      "@AGENTS.md",
      "@AGENTS.md\n- Mine, inside the block.",
    );
    writeFileSync(file(), edited);
    h.fake.compile(t.id, { "CLAUDE.md": SHIM_2 });
    await d.syncOnce();
    expect(read()).toBe(edited);
    expect(h.fake.calls("POST", /\/observations$/)).toHaveLength(1);
    expect(h.fake.entry(t.id).target.sync_state).toBe("drifted");

    // Stopped while the block has edits: the block stays.
    h.fake.setState(t.id, "off");
    await d.syncOnce();
    expect(read()).toBe(edited);
    expect(d.snapshot().repos[0].targets[0].state).toBe("off_block_kept");
  });

  it("creates the file with just the block when there is none", async () => {
    const t = setup();
    h.fake.compile(t.id, { "CLAUDE.md": SHIM_1 });
    await h.daemon().syncOnce();
    expect(read()).toBe(
      ["<!-- memax:start -->", ...SHIM_1, "<!-- memax:end -->", ""].join("\n"),
    );
  });

  it("reports broken markers as a hand edit and writes nothing", async () => {
    const t = setup();
    writeFileSync(file(), "# Mine\n<!-- memax:start -->\nhalf a block\n");
    h.fake.compile(t.id, { "CLAUDE.md": SHIM_1 });
    await h.daemon().syncOnce();
    expect(read()).toBe("# Mine\n<!-- memax:start -->\nhalf a block\n");
    expect(h.fake.calls("POST", /\/observations$/)).toHaveLength(1);
  });

  it("reports a removed block once Memax had delivered one", async () => {
    const t = setup();
    writeFileSync(file(), BEFORE);
    h.fake.compile(t.id, { "CLAUDE.md": SHIM_1 });
    const d = h.daemon();
    await d.syncOnce();
    writeFileSync(file(), BEFORE);
    h.fake.compile(t.id, { "CLAUDE.md": SHIM_2 });
    await d.syncOnce();
    expect(read()).toBe(BEFORE);
    expect(h.fake.calls("POST", /\/observations$/)).toHaveLength(1);
  });
});

describe("a target that stops compiling", () => {
  it("leaves a file Memax owns where it is", async () => {
    const space = h.fake.addSpace("memax-v2");
    const t = h.fake.addTarget(space, "agents_md");
    h.link(space);
    h.fake.compile(t.id, { "AGENTS.md": "# Brief\n" });
    const d = h.daemon();
    await d.syncOnce();
    h.fake.setState(t.id, "off");
    h.fake.compile(t.id, { "AGENTS.md": "# Brief, later\n" });
    await d.syncOnce();
    expect(readFileSync(join(h.repo, "AGENTS.md"), "utf8")).toBe("# Brief\n");
    expect(d.snapshot().repos[0].targets[0]).toMatchObject({
      state: "off",
      detail: "stopped; the file stays where it is",
    });
  });
});
