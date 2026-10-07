// memax connect <agent> in a temporary home (never the real ~): each
// agent's hook in its own settings, other settings left alone, running it
// twice changes nothing, and the connection and the first compile against
// the fake /v2 server.
import { execFileSync } from "node:child_process";
import { mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import type { V2 } from "memax-sdk";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import {
  hookCommand,
  hookInstalled,
  installHook,
} from "../../src/lib/connect/hooks.js";
import {
  agentFor,
  runConnect,
  type ConnectDeps,
} from "../../src/lib/connect/run.js";
import type { AgentEntry } from "../../src/lib/init/agents.js";
import { connectFailed, renderConnect } from "../../src/commands/connect.js";
import { installInit } from "../init/fake-init.js";
import { harness, type Harness } from "../daemon/harness.js";

let h: Harness;
beforeEach(async () => {
  h = await harness();
  installInit(h.fake);
});
afterEach(() => h.cleanup());

const read = (p: string) => readFileSync(join(h.home, p), "utf8");
const json = (p: string) => JSON.parse(read(p));

/** Each agent with a hook: where it lands, and the entry it gets. */
const HOOKED: Array<[string, string, (j: any) => unknown]> = [
  [
    "claude-code",
    ".claude/settings.json",
    (j) => j.hooks.SessionStart.at(-1).hooks[0].command,
  ],
  [
    "codex",
    ".codex/hooks.json",
    (j) => j.hooks.SessionStart.at(-1).hooks[0].command,
  ],
  [
    "gemini-cli",
    ".gemini/settings.json",
    (j) => j.hooks.SessionStart.at(-1).hooks[0].command,
  ],
  ["cursor", ".cursor/hooks.json", (j) => j.hooks.sessionStart.at(-1).command],
  [
    "copilot",
    ".copilot/hooks/memax.json",
    (j) => j.hooks.sessionStart.at(-1).bash,
  ],
];

describe("the session-start hook, per agent", () => {
  for (const [kind, file, command] of HOOKED) {
    it(`installs ${kind}'s once, keeping the rest of ${file}`, () => {
      mkdirSync(join(h.home, file, ".."), { recursive: true });
      const theirs = {
        model: "keep-me",
        hooks: {
          [kind === "cursor" || kind === "copilot"
            ? "sessionStart"
            : "SessionStart"]: [
            kind === "cursor" || kind === "copilot"
              ? { command: "echo theirs", bash: "echo theirs" }
              : {
                  matcher: "startup",
                  hooks: [{ type: "command", command: "echo theirs" }],
                },
          ],
          Stop: [{ hooks: [{ type: "command", command: "echo stop" }] }],
        },
      };
      writeFileSync(join(h.home, file), JSON.stringify(theirs, null, 2));
      expect(hookInstalled(kind, h.home)).toBe(false);
      expect(installHook(kind, h.home)).toBe("written");
      const after = read(file);
      const j = JSON.parse(after);
      expect(command(j)).toBe(hookCommand(kind));
      expect(j.model).toBe("keep-me");
      expect(JSON.stringify(j)).toContain("echo theirs");
      expect(JSON.stringify(j.hooks.Stop)).toContain("echo stop");
      expect(hookInstalled(kind, h.home)).toBe(true);
      // Again: nothing changes, byte for byte.
      expect(installHook(kind, h.home)).toBe("present");
      expect(read(file)).toBe(after);
    });
  }

  it("writes the shapes and units each agent documents", () => {
    for (const [kind] of HOOKED) installHook(kind, h.home);
    expect(json(".claude/settings.json").hooks.SessionStart[0]).toEqual({
      matcher: "startup|resume|clear|compact",
      hooks: [
        { type: "command", command: hookCommand("claude-code"), timeout: 10 },
      ],
    });
    expect(
      json(".gemini/settings.json").hooks.SessionStart[0].hooks[0],
    ).toMatchObject({
      name: "memax",
      timeout: 10_000, // milliseconds
    });
    expect(json(".cursor/hooks.json")).toEqual({
      version: 1,
      hooks: {
        sessionStart: [{ command: hookCommand("cursor"), timeout: 10 }],
      },
    });
    expect(json(".copilot/hooks/memax.json")).toEqual({
      version: 1,
      hooks: {
        sessionStart: [
          { type: "command", bash: hookCommand("copilot"), timeoutSec: 10 },
        ],
      },
    });
  });

  it("replaces an older Memax hook instead of adding a second", () => {
    mkdirSync(join(h.home, ".claude"), { recursive: true });
    writeFileSync(
      join(h.home, ".claude/settings.json"),
      JSON.stringify({
        hooks: {
          SessionStart: [
            {
              matcher: "startup",
              hooks: [
                { type: "command", command: "memax hook session-start" },
                { type: "command", command: "echo mine" },
              ],
            },
          ],
        },
      }),
    );
    expect(installHook("claude-code", h.home)).toBe("written");
    const groups = json(".claude/settings.json").hooks.SessionStart;
    expect(groups).toHaveLength(2);
    expect(groups[0].hooks).toEqual([
      { type: "command", command: "echo mine" },
    ]);
    expect(groups[1].hooks[0].command).toBe(hookCommand("claude-code"));
  });

  it("leaves Claude Code to the Memax plugin when it is on", () => {
    mkdirSync(join(h.home, ".claude"), { recursive: true });
    writeFileSync(
      join(h.home, ".claude/settings.json"),
      JSON.stringify({ enabledPlugins: { "memax@memax": true } }),
    );
    expect(installHook("claude-code", h.home)).toBe("plugin");
    expect(json(".claude/settings.json").hooks).toBeUndefined();
  });

  it("refuses to rewrite a settings file it can't read", () => {
    mkdirSync(join(h.home, ".cursor"), { recursive: true });
    writeFileSync(join(h.home, ".cursor/hooks.json"), "{ not json");
    expect(installHook("cursor", h.home)).toMatchObject({
      error: expect.stringContaining("isn't valid JSON"),
    });
    expect(read(".cursor/hooks.json")).toBe("{ not json");
  });

  it("runs nothing where the CLI isn't installed", () => {
    expect(hookCommand("codex")).toBe(
      "command -v memax >/dev/null 2>&1 && memax hook session-start --agent codex || true",
    );
  });
});

function deps(over: Partial<ConnectDeps> = {}) {
  const mcp = new Set<string>();
  const compiles: string[] = [];
  const d: ConnectDeps = {
    memax: h.memax,
    paths: h.paths,
    cwd: h.repo,
    home: h.home,
    platform: "linux",
    hasCredentials: () => true,
    hasMcp: (a: AgentEntry) => mcp.has(a.kind),
    writeMcp: async (a: AgentEntry) => {
      mcp.add(a.kind);
      return "written";
    },
    compile: async (space) => {
      compiles.push(space);
      return {
        timedOut: false,
        writer: "compile",
        targets: [
          {
            label: "AGENTS.md",
            kind: "agents_md",
            sync_state: "in_sync",
            files: ["AGENTS.md"],
          },
        ],
      };
    },
    ...over,
  };
  return { d, mcp, compiles };
}

function connection(
  agent: V2.AgentKind,
  spaces: V2.AgentSpace[] = [],
): V2.AgentConnection {
  const now = new Date().toISOString();
  return {
    id: `conn-${agent}`,
    person_id: "p-1",
    agent,
    display_name: agent,
    surface: "cli",
    credential: { kind: "oauth_grant", id: "g-1", active: true },
    max_autonomy: "write",
    state: "active",
    spaces,
    reads_7d: 0,
    writes_7d: 0,
    created_receipt_id: "r-1",
    last_receipt_id: "r-1",
    created_at: now,
    updated_at: now,
  } as unknown as V2.AgentConnection;
}

describe("memax connect", () => {
  it("knows the board's agent ids, gemini included", () => {
    expect(agentFor("gemini")?.kind).toBe("gemini-cli");
    expect(agentFor("Claude-Code")?.kind).toBe("claude-code");
    expect(agentFor("vim")).toBeNull();
  });

  for (const [kind, file] of HOOKED) {
    it(`connects ${kind}, and a second run changes nothing`, async () => {
      execFileSync("git", ["init", "-q", h.repo]);
      const space = h.fake.addSpace("acme-web");
      h.link(space);
      h.fake.agents.push(connection(kind as V2.AgentKind));
      const t = h.fake.addTarget(space, "agents_md");
      const { d, compiles } = deps({ cwd: h.repo });
      const a = agentFor(kind)!;

      const first = await runConnect(a, { space: "acme-web" }, d);
      expect(first.mcp).toBe("written");
      expect(first.hook.outcome).toBe("written");
      const start =
        kind === "cursor" || kind === "gemini-cli" ? "read" : "propose";
      expect(first.connection).toEqual({
        state: "connected",
        space: "acme-web",
        autonomy: start,
      });
      expect(compiles).toEqual(["acme-web"]);
      const settings = read(file);

      // The daemon delivered meanwhile: the files are in sync.
      h.fake.compile(t.id, { "AGENTS.md": "# compiled\n" });
      h.fake.setState(t.id, "in_sync");
      const second = await runConnect(a, { space: "acme-web" }, d);
      expect(second.mcp).toBe("present");
      expect(second.hook.outcome).toBe("present");
      expect(second.connection).toEqual({
        state: "already",
        space: "acme-web",
        autonomy: start,
      });
      expect(second.compile.state).toBe("in_sync");
      expect(compiles).toEqual(["acme-web"]);
      expect(read(file)).toBe(settings);
      expect(h.fake.calls("PATCH", /\/v2\/agents\//)).toHaveLength(1);
    });
  }

  it("never raises an agent that is lower elsewhere", async () => {
    const space = h.fake.addSpace("acme-web");
    const other = h.fake.addSpace("personal");
    h.fake.agents.push(
      connection("codex", [
        {
          space_id: other.id,
          slug: other.slug,
          name: other.name,
          kind: "personal",
          autonomy: "read",
          reads_7d: 0,
          writes_7d: 0,
          updated_at: new Date().toISOString(),
        },
      ]),
    );
    const r = await runConnect(
      agentFor("codex")!,
      { space: space.slug },
      deps().d,
    );
    expect(r.connection.autonomy).toBe("read");
    expect(h.fake.calls("PATCH", /\/v2\/agents\//)[0].body).toMatchObject({
      autonomy: "read",
    });
  });

  it("says an agent without a connection connects on its first sign-in", async () => {
    h.fake.addSpace("acme-web");
    const r = await runConnect(
      agentFor("cursor")!,
      { space: "acme-web" },
      deps().d,
    );
    expect(r.connection).toEqual({
      state: "on_sign_in",
      space: "acme-web",
      autonomy: "read",
    });
    expect(r.compile.state).toBe("not_linked");
  });

  it("writes the MCP settings and the hook before sign-in", async () => {
    const r = await runConnect(
      agentFor("claude-code")!,
      {},
      deps({ hasCredentials: () => false }).d,
    );
    expect(r.mcp).toBe("written");
    expect(r.hook.outcome).toBe("written");
    expect(r.connection.state).toBe("signed_out");
  });

  it("says where an agent has no session-start hook", async () => {
    const r = await runConnect(
      agentFor("opencode")!,
      {},
      deps({ hasCredentials: () => false }).d,
    );
    expect(r.hook).toEqual({
      outcome: "unsupported",
      file: null,
      note: "OpenCode has no session-start hook; it reads AGENTS.md and Memax over MCP",
    });
    const w = await runConnect(
      agentFor("windsurf")!,
      {},
      deps({ hasCredentials: () => false }).d,
    );
    expect(w.hook.outcome).toBe("unsupported");
  });

  it("leaves Claude Code's MCP server and hook to the plugin", async () => {
    mkdirSync(join(h.home, ".claude"), { recursive: true });
    writeFileSync(
      join(h.home, ".claude/settings.json"),
      JSON.stringify({ enabledPlugins: { "memax@memax": true } }),
    );
    const { d, mcp } = deps({ hasCredentials: () => false });
    const r = await runConnect(agentFor("claude-code")!, {}, d);
    expect(r.mcp).toBe("plugin");
    expect(r.hook.outcome).toBe("plugin");
    expect(mcp.size).toBe(0);
  });

  it("prints one line a step", async () => {
    h.fake.addSpace("acme-web");
    h.fake.agents.push(connection("codex"));
    const r = await runConnect(
      agentFor("codex")!,
      { space: "acme-web" },
      deps().d,
    );
    const plain = renderConnect(r, "https://memax.app").map((l) =>
      l.replace(/\u001b\[[0-9;]*m/g, ""),
    );
    expect(plain).toEqual([
      "",
      "  Memax · connect Codex",
      "",
      "  ✓ MCP      settings written; it signs in to Memax on first use",
      "  ✓ Hook     session start, in ~/.codex/hooks.json",
      "  ○          Codex asks you to trust it once: run /hooks in Codex",
      "  ✓ Space    acme-web, at Propose",
      "  - Compile  this repository isn't linked; memax link writes the compiled files here",
      "",
    ]);
    expect(connectFailed(r)).toBe(false);
  });

  it("adds GEMINI.md for Gemini CLI only when asked, and once", async () => {
    execFileSync("git", ["init", "-q", h.repo]);
    const space = h.fake.addSpace("acme-web");
    h.link(space);
    h.fake.agents.push(connection("gemini-cli"));
    h.fake.addTarget(space, "agents_md");
    const gemini = agentFor("gemini")!;
    const created = () => h.fake.calls("POST", /\/v2\/spaces\/[^/]+\/targets$/);

    const plain = await runConnect(gemini, { space: "acme-web" }, deps().d);
    expect(plain.shim).toEqual({ state: "not_asked" });
    expect(created()).toHaveLength(0);
    expect(
      renderConnect(plain, "https://memax.app")
        .map((l) => l.replace(/\u001b\[[0-9;]*m/g, ""))
        .find((l) => l.includes("GEMINI.md")),
    ).toBe(
      "  - GEMINI.md not compiled: Antigravity CLI reads AGENTS.md. For Gemini CLI, add --gemini-md",
    );

    const asked = await runConnect(
      gemini,
      { space: "acme-web", geminiMd: true },
      deps().d,
    );
    expect(asked.shim).toEqual({ state: "created", path: "GEMINI.md" });
    expect(created()).toHaveLength(1);
    const shim = [...h.fake.targets.values()].find(
      (e) => e.target.kind === "gemini_md",
    )!.target;
    expect(shim.settings.user_owned).toBeUndefined();

    const again = await runConnect(
      gemini,
      { space: "acme-web", geminiMd: true },
      deps().d,
    );
    expect(again.shim).toEqual({ state: "present", path: "GEMINI.md" });
    expect(created()).toHaveLength(1);

    // Other agents never get the line.
    h.fake.agents.push(connection("codex"));
    const codex = await runConnect(
      agentFor("codex")!,
      { space: "acme-web" },
      deps().d,
    );
    expect(codex.shim).toBeUndefined();
  });

  it("keeps a GEMINI.md someone wrote theirs, with one Memax block", async () => {
    execFileSync("git", ["init", "-q", h.repo]);
    writeFileSync(join(h.repo, "GEMINI.md"), "# My Gemini notes\n");
    const space = h.fake.addSpace("acme-web");
    h.link(space);
    h.fake.agents.push(connection("gemini-cli"));
    const r = await runConnect(
      agentFor("gemini")!,
      { space: "acme-web", geminiMd: true },
      deps().d,
    );
    expect(r.shim?.state).toBe("created");
    const shim = [...h.fake.targets.values()].find(
      (e) => e.target.kind === "gemini_md",
    )!.target;
    expect(shim.settings.user_owned).toBe(true);
  });

  it("skips the hook on Windows, and with --no-hook", async () => {
    const win = await runConnect(
      agentFor("codex")!,
      {},
      deps({ platform: "win32", hasCredentials: () => false }).d,
    );
    expect(win.hook.outcome).toBe("windows");
    const no = await runConnect(
      agentFor("codex")!,
      { hook: false },
      deps({ hasCredentials: () => false }).d,
    );
    expect(no.hook.outcome).toBe("skipped");
  });
});
