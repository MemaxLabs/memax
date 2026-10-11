// memax connect on a machine whose home is linked (personal-layer design
// §6.1, §6.5, §7.2): the agent's home file joins the link, or moves to the
// file the agent reads now; an unlinked machine is never linked again; and
// the agent is connected to the Personal space too.
import { execFileSync } from "node:child_process";
import { mkdirSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import type { V2 } from "memax-sdk";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { renderConnect } from "../../src/commands/connect.js";
import { readHomeLink, writeHomeLink } from "../../src/lib/daemon/home-link.js";
import {
  agentFor,
  runConnect,
  type ConnectDeps,
} from "../../src/lib/connect/run.js";
import { harness, type Harness } from "../daemon/harness.js";
import { installInit, type InitState } from "../init/fake-init.js";

// eslint-disable-next-line no-control-regex
const plain = (lines: string[]) =>
  lines.join("\n").replace(/\x1b\[[0-9;]*m/g, "");

let h: Harness;
let st: InitState;
let personal: V2.Space;
let machine: V2.Machine;
beforeEach(async () => {
  h = await harness();
  st = installInit(h.fake);
  mkdirSync(join(h.home, ".claude"), { recursive: true });
  mkdirSync(join(h.home, ".codex"), { recursive: true });
  personal = h.fake.addPersonalSpace();
  // This machine's home, linked with Claude Code's file only.
  machine = h.fake.addMachine(personal, "device-1", "work-mbp");
  const rules = h.fake.addHomeTarget(personal, machine, "claude_user");
  writeHomeLink(h.paths, {
    space_id: personal.id,
    space_slug: personal.slug,
    machine_id: machine.id,
    person_id: "ana",
    home: h.home,
    files: [
      {
        target_id: rules.id,
        kind: "claude_user",
        path: rules.path!,
        abs: join(h.home, rules.path!),
      },
    ],
  });
});
afterEach(() => h.cleanup());

function deps(over: Partial<ConnectDeps> = {}) {
  const compiles: string[] = [];
  const d: ConnectDeps = {
    memax: h.memax,
    paths: h.paths,
    cwd: h.home,
    home: h.home,
    platform: "linux",
    hasCredentials: () => true,
    hasMcp: () => true,
    writeMcp: async () => "written",
    compile: async (space) => {
      compiles.push(space);
      return { timedOut: false, writer: "compile", targets: [] };
    },
    env: {},
    ...over,
  };
  return { d, compiles };
}

function connection(
  agent: V2.AgentKind,
  spaces: V2.Space[],
): V2.AgentConnection {
  return {
    id: `conn-${agent}`,
    agent,
    display_name: agent,
    state: "active",
    spaces: spaces.map((s) => ({
      space_id: s.id,
      slug: s.slug,
      name: s.name,
      kind: s.kind,
      autonomy: "propose",
      reads_7d: 0,
      writes_7d: 0,
      updated_at: new Date().toISOString(),
    })),
  } as unknown as V2.AgentConnection;
}

const links = () => h.fake.calls("POST", /\/machines$/);

describe("memax connect on a linked machine", () => {
  it("adds Antigravity's ~/.gemini/AGENTS.md, and --agents-md makes Gemini CLI a reader of it", async () => {
    mkdirSync(join(h.home, ".gemini", "antigravity-cli"), { recursive: true });
    h.fake.agents.push(connection("gemini-cli", [personal]));
    const gemini = agentFor("gemini")!;
    const first = await runConnect(gemini, {}, deps().d);
    expect(first.home).toMatchObject({
      state: "added",
      path: "~/.gemini/AGENTS.md",
      note: "a new file, with one Memax block",
    });
    expect(links().at(-1)?.body).toMatchObject({
      files: [
        {
          kind: "gemini_user",
          path: ".gemini/AGENTS.md",
          read_by: ["antigravity"],
        },
      ],
    });
    const file = () =>
      readHomeLink(h.paths)!.files.find((f) => f.kind === "gemini_user")!;
    expect(file().read_by).toEqual(["antigravity"]);

    // Gemini CLI reads it too once its context.fileName lists AGENTS.md:
    // the target's read_by and home.json follow, so the hook prints it.
    const both = await runConnect(gemini, { agentsMd: true }, deps().d);
    expect(both.contextFile?.state).toBe("written");
    expect(both.home).toMatchObject({
      state: "present",
      note: "read by Antigravity and Gemini CLI",
    });
    expect(file().read_by).toEqual(["antigravity", "gemini-cli"]);
    const target = [...h.fake.targets.values()].find(
      (e) => e.target.kind === "gemini_user",
    )!.target;
    expect(target.settings.read_by).toEqual(["antigravity", "gemini-cli"]);
    expect(links()).toHaveLength(1);

    // Again: nothing changes.
    const again = await runConnect(gemini, { agentsMd: true }, deps().d);
    expect(again.home).toEqual({
      state: "present",
      path: "~/.gemini/AGENTS.md",
    });
    expect(again.contextFile?.state).toBe("present");
  });

  it("adds Copilot's block beside Claude Code's file, and says VS Code's Local agent loads both", async () => {
    mkdirSync(join(h.home, ".copilot"), { recursive: true });
    writeFileSync(
      join(h.home, ".copilot", "copilot-instructions.md"),
      "Use tabs.\n",
    );
    h.fake.agents.push(connection("copilot", [personal]));
    const r = await runConnect(agentFor("copilot")!, {}, deps().d);
    expect(r.home).toMatchObject({
      state: "added",
      path: "~/.copilot/copilot-instructions.md",
    });
    expect(r.home?.note).toBe(
      "one Memax block; your 1 line stays as it is. VS Code's Local agent reads ~/.claude and ~/.copilot both, so it loads your Personal space twice; its Copilot agent, which VS Code recommends, and Copilot CLI read it once.",
    );
    expect(links().at(-1)?.body).toMatchObject({
      files: [
        {
          kind: "copilot_user",
          path: ".copilot/copilot-instructions.md",
          read_by: ["copilot"],
        },
      ],
    });
  });

  it("says Gemini CLI alone needs --agents-md to read the Personal space", async () => {
    mkdirSync(join(h.home, ".gemini"), { recursive: true });
    const r = await runConnect(agentFor("gemini")!, {}, deps().d);
    expect(r.home).toEqual({ state: "absent" });
    expect(plain(renderConnect(r, "https://memax.app"))).toContain(
      "Gemini CLI reads your Personal space from ~/.gemini/AGENTS.md once it reads AGENTS.md: add --agents-md",
    );
    expect(links()).toHaveLength(0);
  });

  it("adds the agent's home file, keeping the machine's name, and writes it; again changes nothing", async () => {
    writeFileSync(join(h.home, ".codex", "AGENTS.md"), "# Mine\n- tabs\n");
    const { d, compiles } = deps();
    const r = await runConnect(agentFor("codex")!, {}, d);
    expect(r.home).toMatchObject({
      state: "added",
      path: "~/.codex/AGENTS.md",
      note: "one Memax block; your 2 lines stay as they are",
      compile: "compiled",
    });
    expect(links()[0].body).toEqual({
      device_id: "device-1",
      name: "work-mbp",
      platform: "darwin",
      files: [
        { kind: "codex_user", path: ".codex/AGENTS.md", read_by: ["codex"] },
      ],
    });
    // The rules file it already had is untouched.
    expect(
      readHomeLink(h.paths)!
        .files.map((f) => f.path)
        .sort(),
    ).toEqual([".claude/rules/memax-personal.md", ".codex/AGENTS.md"]);
    expect(compiles).toEqual(["personal"]);
    expect(plain(renderConnect(r, "https://memax.app"))).toContain(
      "Home     ~/.codex/AGENTS.md: one Memax block; your 2 lines stay as they are; written",
    );

    const again = await runConnect(agentFor("codex")!, {}, d);
    expect(again.home).toMatchObject({
      state: "present",
      path: "~/.codex/AGENTS.md",
    });
    expect(links()).toHaveLength(1);
  });

  it("moves Codex's block to AGENTS.override.md once Codex reads that, stopping the old file", async () => {
    const { d } = deps();
    await runConnect(agentFor("codex")!, {}, d);
    const old = readHomeLink(h.paths)!.files.find(
      (f) => f.kind === "codex_user",
    )!;
    writeFileSync(join(h.home, ".codex", "AGENTS.override.md"), "# For now\n");
    const r = await runConnect(agentFor("codex")!, {}, d);
    expect(r.home).toMatchObject({
      state: "moved",
      path: "~/.codex/AGENTS.override.md",
      note: "from ~/.codex/AGENTS.md",
    });
    const now = readHomeLink(h.paths)!.files.filter(
      (f) => f.kind === "codex_user",
    );
    expect(now.map((f) => f.path)).toEqual([".codex/AGENTS.override.md"]);
    expect(h.fake.entry(old.target_id).target.sync_state).toBe("off");
  });

  it("never links an unlinked machine again", async () => {
    h.fake.unlinkMachine(machine.id);
    const { d } = deps();
    const r = await runConnect(agentFor("codex")!, {}, d);
    expect(r.home?.state).toBe("unlinked");
    expect(links()).toHaveLength(0);
  });

  it("refuses a home file in a dotfiles repository", async () => {
    mkdirSync(join(h.home, ".codex", ".git"));
    const { d } = deps();
    const r = await runConnect(agentFor("codex")!, {}, d);
    expect(r.home).toMatchObject({
      state: "refused",
      note: expect.stringContaining("a git repository"),
    });
    expect(links()).toHaveLength(0);
  });

  it("connects the agent to Personal too, from a repository linked to a project", async () => {
    execFileSync("git", ["init", "-q", h.repo]);
    const project = h.fake.addSpace("acme-web");
    h.link(project);
    h.fake.agents.push(connection("claude-code", [project]));
    const { d } = deps({ cwd: h.repo });
    const r = await runConnect(agentFor("claude-code")!, {}, d);
    expect(r.connection).toMatchObject({ state: "already", space: "acme-web" });
    expect(r.personal).toEqual({
      state: "connected",
      space: "personal",
      autonomy: "propose",
    });
    expect(st.connected).toContainEqual({
      agent: "claude-code",
      space: "personal",
      autonomy: "propose",
    });
    expect(r.home?.state).toBe("present");
  });

  it("outside a repository, the Personal space is the space", async () => {
    h.fake.agents.push(connection("codex", []));
    const { d } = deps();
    const r = await runConnect(agentFor("codex")!, {}, d);
    expect(r.connection).toMatchObject({
      state: "connected",
      space: "personal",
    });
    expect(r.personal).toBeUndefined();
  });

  it("says how to link the home where it isn't", async () => {
    const { rmSync } = await import("node:fs");
    rmSync(h.paths.home);
    const { d } = deps();
    const r = await runConnect(agentFor("codex")!, { space: "personal" }, d);
    expect(r.home).toEqual({ state: "not_linked" });
    expect(plain(renderConnect(r, "https://memax.app"))).toContain(
      "memax link --home writes your Personal space into its own file here",
    );
  });
});
