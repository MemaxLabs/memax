// memax status and memax compile: what they print, in text and JSON.
import { execFileSync } from "node:child_process";
import { readFileSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import type { V2 } from "memax-sdk";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { compile, type CompileDeps } from "../../src/commands/compile.js";
import { buildStatus, renderStatus } from "../../src/commands/status.js";
import { writeMemaxYmlSpace } from "../../src/lib/project-context.js";
import { OneShotDelivery } from "../../src/lib/daemon/oneshot.js";
import { harness, type Harness } from "./harness.js";

// eslint-disable-next-line no-control-regex
const plain = (lines: string[]) =>
  lines.join("\n").replace(/\x1b\[[0-9;]*m/g, "");

let h: Harness;
beforeEach(async () => {
  h = await harness();
  execFileSync("git", ["init", "-q", h.repo]);
});
afterEach(async () => {
  await h.cleanup();
});

function agent(
  kind: V2.AgentKind,
  name: string,
  autonomy: V2.Autonomy,
  space: V2.Space,
  state: V2.AgentState = "active",
): V2.AgentConnection {
  return {
    id: `${kind}-id`,
    person_id: "p",
    agent: kind,
    display_name: name,
    surface: "cli",
    credential: { kind: "api_key", id: "k", active: true },
    max_autonomy: "write",
    state,
    spaces: [
      {
        space_id: space.id,
        slug: space.slug,
        name: space.name,
        kind: space.kind,
        autonomy,
        reads_7d: 0,
        writes_7d: 0,
        updated_at: new Date().toISOString(),
      },
    ],
    reads_7d: 0,
    writes_7d: 0,
    created_receipt_id: "r",
    last_receipt_id: "r",
    created_at: new Date().toISOString(),
    updated_at: new Date().toISOString(),
  };
}

/** The board's memax-v2: four targets, one drifted, six agents. */
async function board() {
  const space = h.fake.addSpace("memax-v2", "Memax V2");
  const claude = h.fake.addTarget(space, "claude_md");
  const agents = h.fake.addTarget(space, "agents_md");
  const cursor = h.fake.addTarget(space, "cursor_mdc");
  const gpt = h.fake.addTarget(space, "chatgpt");
  h.link(space);
  writeMemaxYmlSpace(h.repo, "memax-v2");
  h.fake.compile(claude.id, { "CLAUDE.md": "@AGENTS.md\n" });
  h.fake.compile(agents.id, { "AGENTS.md": "# Brief\n" });
  h.fake.compile(cursor.id, { ".cursor/rules/memax-web.mdc": "- x [M-1]\n" });
  h.fake.compile(gpt.id, {});
  await h.daemon().syncOnce();
  writeFileSync(
    join(h.repo, ".cursor/rules/memax-web.mdc"),
    "- edited [M-1]\n",
  );
  h.fake.compile(cursor.id, { ".cursor/rules/memax-web.mdc": "- y [M-1]\n" });
  await h.daemon().syncOnce();
  h.fake.keptCount = 214;
  h.fake.reviewTotal = 5;
  h.fake.agents.push(
    agent("claude-code", "Claude Code", "write", space),
    agent("codex", "Codex", "propose", space),
    agent("cursor", "Cursor", "read", space),
    agent("chatgpt", "ChatGPT", "propose", space),
    agent("claude", "Claude", "propose", space),
    agent("gemini-cli", "Gemini CLI", "propose", space, "paused"),
    agent("opencode", "OpenCode", "write", space, "disconnected"),
  );
  return { space, claude, agents, cursor, gpt };
}

describe("memax status", () => {
  it("prints the board's lines", async () => {
    await board();
    const { report, targets } = await buildStatus(h.memax, {
      cwd: h.repo,
      paths: h.paths,
    });
    const text = plain(renderStatus(report, targets));
    expect(text).toContain("memax-v2 · Project · 214 kept · 5 waiting on you");
    expect(text).toMatch(/● CLAUDE\.md\s+in sync\s+\d\d:\d\d/);
    expect(text).toMatch(/● AGENTS\.md\s+in sync\s+\d\d:\d\d/);
    expect(text).toMatch(/○ \.cursor\/rules\s+drifted\s+1 local edit/);
    expect(text).toMatch(/● ChatGPT project\s+in sync\s+copy it from the app/);
    expect(text).toContain(
      "CC write · CX propose · CU read · GPT propose · CL propose · GM paused",
    );
    expect(text).not.toContain("OC");
    // Files first, as on the board; ChatGPT last.
    expect(text.indexOf("ChatGPT")).toBeGreaterThan(text.indexOf(".cursor"));
    expect(text).toContain(
      "The daemon isn't running, so files wait to be written: memax daemon start",
    );
  });

  it("reports the same in JSON", async () => {
    const { space, cursor } = await board();
    const { report } = await buildStatus(h.memax, {
      cwd: h.repo,
      paths: h.paths,
    });
    const json = JSON.parse(JSON.stringify(report));
    expect(json).toMatchObject({
      space: { id: space.id, slug: "memax-v2", kind: "project" },
      kept: 214,
      kept_more: false,
      waiting: 5,
      daemon: { running: false, linked_here: true },
    });
    expect(
      json.targets.find((t: { id: string }) => t.id === cursor.id),
    ).toMatchObject({ sync_state: "drifted", open_drift: 1 });
    expect(json.agents.map((a: { mark: string }) => a.mark)).toEqual([
      "CC",
      "CX",
      "CU",
      "GPT",
      "CL",
      "GM",
    ]);
  });

  it("counts kept memories only so far, and keeps going without them", async () => {
    const space = h.fake.addSpace("memax-v2");
    h.fake.keptCount = 900;
    h.fake.fail(/\/review$/, 500, "internal_error");
    const { report, targets } = await buildStatus(h.memax, {
      cwd: h.repo,
      paths: h.paths,
      space: space.slug,
    });
    expect(report).toMatchObject({ kept: 600, kept_more: true, waiting: null });
    const text = plain(renderStatus(report, targets));
    expect(text).toContain("memax-v2 · Project · 600+ kept");
    expect(text).toContain("No targets yet");
  });

  it("says how to pick a space outside a linked repository", async () => {
    h.fake.addSpace("memax-v2");
    await expect(
      buildStatus(h.memax, { cwd: h.repo, paths: h.paths }),
    ).rejects.toThrow(
      /--space <slug>, or link this repository with memax link/,
    );
  });
});

describe("memax compile", () => {
  const deps = (
    out: string[],
    extra: Partial<CompileDeps> = {},
  ): CompileDeps => ({
    memax: h.memax,
    paths: h.paths,
    cwd: h.repo,
    out: (l) => out.push(l),
    pollMs: 20,
    oneShot: () =>
      OneShotDelivery.open({
        paths: h.paths,
        api: h.api,
        deviceId: "device-1",
        log: h.log,
        version: "test",
        apiUrl: h.fake.url,
      }),
    ...extra,
  });

  /** Plays the server's compile worker: each requested target compiles. */
  function worker(contents: Record<string, Record<string, string>>) {
    const timer = setInterval(() => {
      for (const e of h.fake.targets.values()) {
        const t = e.target;
        if (t.sync_state === "compiling" && t.compiled_gen < t.dirty_gen)
          h.fake.compile(t.id, contents[t.id] ?? {});
      }
    }, 15);
    return () => clearInterval(timer);
  }

  it("asks every target to compile, writes the files here, and reports them", async () => {
    const space = h.fake.addSpace("memax-v2");
    const agents = h.fake.addTarget(space, "agents_md");
    const gpt = h.fake.addTarget(space, "chatgpt");
    const off = h.fake.addTarget(space, "gemini_md");
    h.fake.setState(off.id, "off");
    h.link(space);
    const stop = worker({
      [agents.id]: { "AGENTS.md": "# Brief, compiled now\n" },
    });
    const out: string[] = [];
    try {
      expect(await compile({}, deps(out))).toBe(0);
    } finally {
      stop();
    }
    // Asked for at once, so they may arrive in either order.
    const compiles = h.fake.calls("POST", /:compile$/);
    expect(compiles.map((c) => c.path).sort()).toEqual(
      [
        `/v2/targets/${agents.id}:compile`,
        `/v2/targets/${gpt.id}:compile`,
      ].sort(),
    );
    expect(compiles[0].headers["x-memax-via"]).toBe("cli");
    expect(readFileSync(join(h.repo, "AGENTS.md"), "utf8")).toBe(
      "# Brief, compiled now\n",
    );
    const text = plain(out);
    expect(text).toContain("✓ memax-v2 is compiled into 1 file.");
    expect(text).toMatch(/● AGENTS\.md\s+in sync/);
    expect(text).toMatch(/- GEMINI\.md\s+off/);
    expect(text).toContain("Written here by memax compile.");
  });

  it("reports in JSON, and leaves the writing to a running daemon", async () => {
    const space = h.fake.addSpace("memax-v2");
    const agents = h.fake.addTarget(space, "agents_md");
    h.link(space);
    const d = h.daemon();
    await d.start();
    const stop = worker({ [agents.id]: { "AGENTS.md": "# Brief\n" } });
    const out: string[] = [];
    try {
      expect(
        await compile(
          { format: "json" },
          deps(out, {
            oneShot: async () => {
              throw new Error("must not deliver");
            },
          }),
        ),
      ).toBe(0);
    } finally {
      stop();
    }
    const json = JSON.parse(out.join("\n"));
    expect(json).toMatchObject({
      space: "memax-v2",
      requested: [agents.id],
      writer: "daemon",
      timed_out: false,
    });
    expect(json.targets[0]).toMatchObject({
      label: "AGENTS.md",
      sync_state: "in_sync",
      files: ["AGENTS.md"],
    });
  });

  it("with pending, waits for the compiles already coming and asks for none (memax init)", async () => {
    const space = h.fake.addSpace("memax-v2");
    const agents = h.fake.addTarget(space, "agents_md");
    const claude = h.fake.addTarget(space, "claude_md");
    h.fake.compile(claude.id, { "CLAUDE.md": "@AGENTS.md\n" });
    h.link(space);
    const stop = worker({ [agents.id]: { "AGENTS.md": "# Brief\n" } });
    const out: string[] = [];
    try {
      expect(await compile({ format: "json", pending: true }, deps(out))).toBe(
        0,
      );
    } finally {
      stop();
    }
    expect(h.fake.calls("POST", /:compile$/)).toEqual([]);
    const json = JSON.parse(out.join("\n"));
    expect(json.requested.sort()).toEqual([agents.id, claude.id].sort());
    // The one coming compiled, and both are written here.
    expect(readFileSync(join(h.repo, "AGENTS.md"), "utf8")).toBe("# Brief\n");
    expect(readFileSync(join(h.repo, "CLAUDE.md"), "utf8")).toBe(
      "@AGENTS.md\n",
    );
    expect(h.fake.entry(claude.id).runs).toHaveLength(1);
  });

  it("says --via pr isn't available yet, and nothing is requested", async () => {
    h.fake.addSpace("memax-v2");
    const out: string[] = [];
    expect(await compile({ via: "pr" }, deps(out))).toBe(1);
    expect(plain(out)).toContain("not available yet (Phase 4)");
    expect(h.fake.calls("POST", /:compile$/)).toHaveLength(0);
  });

  it("says nothing was written in a repository that isn't linked", async () => {
    const space = h.fake.addSpace("memax-v2");
    const agents = h.fake.addTarget(space, "agents_md");
    const stop = worker({ [agents.id]: { "AGENTS.md": "# Brief\n" } });
    const out: string[] = [];
    try {
      expect(await compile({ space: "memax-v2" }, deps(out))).toBe(0);
    } finally {
      stop();
    }
    expect(plain(out)).toContain(
      "Nothing was written here: this repository isn't linked.",
    );
  });

  it("gives up after the timeout and says so", async () => {
    const space = h.fake.addSpace("memax-v2");
    h.fake.addTarget(space, "agents_md");
    h.link(space);
    const out: string[] = [];
    expect(await compile({ timeout: "1" }, deps(out))).toBe(1);
    expect(plain(out)).toContain(
      "memax-v2 is still compiling. Check again with: memax status",
    );
  });
});
