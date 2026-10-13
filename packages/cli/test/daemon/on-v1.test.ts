// A space switched back to V1: its targets are still listed, but V1's
// two-way sync writes its files again, so the daemon writes nothing for it,
// reports no hand edit and checks nothing (the server would refuse the
// reports), and the warm cache tells the hook to stay quiet. Its state is
// kept, so once the space is on V2 again the daemon picks up where it was,
// and what V1 wrote meanwhile is a hand edit, never written over (rule 6).
import { mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { renderDaemonStatus } from "../../src/commands/daemon-status.js";
import { writeHomeLink } from "../../src/lib/daemon/home-link.js";
import { readWarm } from "../../src/lib/hook/warm.js";
import { harness, until, type Harness } from "./harness.js";

let h: Harness;
beforeEach(async () => {
  h = await harness();
});
afterEach(async () => {
  await h.cleanup();
});

const AGENTS_1 =
  "<!-- Compiled by Memax from acme-web (C-0881). -->\n\n# Brief\n\n- Jobs run on River. [M-0219]\n";
const AGENTS_2 = AGENTS_1 + "- pnpm workspaces only. [M-0071]\n";
const V1_FILE = "# Acme web\n\nV1's own sync wrote this.\n";

const read = (p: string) => readFileSync(join(h.repo, p), "utf8");
const deliveries = () => h.fake.calls("POST", /\/deliveries$/);
const observations = () => h.fake.calls("POST", /\/observations$/);
const plain = (lines: string[]) =>
  // eslint-disable-next-line no-control-regex
  lines.join("\n").replace(/\x1b\[[0-9;]*m/g, "");

describe("a space switched back to V1", () => {
  it("leaves its files to V1, and picks up where it was once the space is on V2 again", async () => {
    const space = h.fake.addSpace("acme-web");
    const t = h.fake.addTarget(space, "agents_md");
    h.fake.compile(t.id, { "AGENTS.md": AGENTS_1 });
    h.link(space);
    const d = h.daemon();
    await d.syncOnce();
    expect(read("AGENTS.md")).toBe(AGENTS_1);
    expect(deliveries()).toHaveLength(1);

    // Back on V1: V1's sync writes the file, and the space compiles again.
    h.fake.switchSpace(space, false);
    await d.syncOnce();
    writeFileSync(join(h.repo, "AGENTS.md"), V1_FILE);
    await d.checkFile(h.repo, "AGENTS.md");
    h.fake.compile(t.id, { "AGENTS.md": AGENTS_2 });
    await d.syncOnce();
    expect(read("AGENTS.md")).toBe(V1_FILE);
    expect(deliveries()).toHaveLength(1);
    expect(observations()).toHaveLength(0);
    expect(h.fake.calls("GET", /\/preview$/)).toHaveLength(1);
    const repo = d.snapshot().repos[0];
    expect(repo.on_v1).toBe(true);
    expect(repo.targets.map((x) => x.state)).toEqual(["on_v1"]);
    expect(readWarm(h.paths.warm).spaces[space.id].on_v1).toBe(true);
    const status = plain(renderDaemonStatus({ running: true, repos: [repo] }));
    expect(status).toContain(
      "acme-web is on V1 again, so V1 writes these files and Memax leaves them as they are.",
    );
    expect(status).toMatch(/AGENTS\.md\s+on V1/);
    expect(h.log.lines.join("\n")).toContain(
      "the space is on V1 again: leaving its files to V1",
    );

    // On V2 again: what V1 wrote is a hand edit, reported and never
    // written over; the cache speaks for the space again.
    h.fake.switchSpace(space, true);
    await d.syncOnce();
    await d.checkFile(h.repo, "AGENTS.md");
    expect(read("AGENTS.md")).toBe(V1_FILE);
    expect(deliveries()).toHaveLength(1);
    expect(observations().length).toBeGreaterThan(0);
    expect(d.snapshot().repos[0].on_v1).toBeUndefined();
    expect(readWarm(h.paths.warm).spaces[space.id].on_v1).toBeUndefined();
    expect(h.log.lines.join("\n")).toContain(
      "the space is on V2 again: writing its files",
    );
  });

  it("stops watching the files while it runs, and watches them again once the space is back", async () => {
    const space = h.fake.addSpace("acme-web");
    const t = h.fake.addTarget(space, "agents_md");
    h.fake.compile(t.id, { "AGENTS.md": AGENTS_1 });
    h.link(space);
    const d = h.daemon();
    await d.start();
    await until(() => deliveries().length === 1, 5_000, "the first delivery");

    h.fake.switchSpace(space, false);
    await until(
      () => d.snapshot().repos[0]?.on_v1 === true,
      5_000,
      "the poll that says V1",
    );
    writeFileSync(join(h.repo, "AGENTS.md"), V1_FILE);
    // Longer than the watcher's debounce and a rescan in the harness.
    await new Promise((r) => setTimeout(r, 400));
    expect(observations()).toHaveLength(0);

    h.fake.switchSpace(space, true);
    await until(
      () => observations().length > 0,
      5_000,
      "the hand edit V1 left, reported once the space is on V2",
    );
    expect(read("AGENTS.md")).toBe(V1_FILE);
  });

  it("leaves this machine's home files as they are while Personal is on V1", async () => {
    const space = h.fake.addPersonalSpace();
    const m = h.fake.addMachine(space);
    const rules = h.fake.addHomeTarget(space, m, "claude_user");
    const RULES = ".claude/rules/memax-personal.md";
    mkdirSync(join(h.home, ".claude"), { recursive: true });
    writeHomeLink(h.paths, {
      space_id: space.id,
      space_slug: space.slug,
      machine_id: m.id,
      home: h.home,
      files: [
        {
          target_id: rules.id,
          kind: rules.kind,
          path: RULES,
          abs: join(h.home, RULES),
        },
      ],
    });
    const lines = (n: string) =>
      `<!-- Compiled by Memax from personal (C-090${n}). -->\n- Answer in British English. [M-0001]\n`;
    h.fake.compile(rules.id, { [RULES]: lines("1") });
    const d = h.daemon();
    await d.syncOnce();
    const at = join(h.home, RULES);
    expect(readFileSync(at, "utf8")).toBe(lines("1"));

    h.fake.switchSpace(space, false);
    h.fake.compile(rules.id, { [RULES]: lines("2") });
    await d.syncOnce();
    expect(readFileSync(at, "utf8")).toBe(lines("1"));
    expect(d.snapshot().home?.on_v1).toBe(true);
    expect(readWarm(h.paths.warm).home?.on_v1).toBe(true);

    h.fake.switchSpace(space, true);
    await d.syncOnce();
    expect(readFileSync(at, "utf8")).toBe(lines("2"));
    expect(readWarm(h.paths.warm).home?.on_v1).toBeUndefined();
  });
});
