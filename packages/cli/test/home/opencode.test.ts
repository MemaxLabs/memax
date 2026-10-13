// OpenCode and the Personal space: memax connect opencode on a linked
// machine names the canonical copy in OpenCode's instructions, in the file
// whose list OpenCode uses, comments kept; never where OpenCode already
// reads the Personal space from ~/.claude/CLAUDE.md (it would load twice);
// and memax unlink --home takes the entry out again.
import {
  existsSync,
  mkdirSync,
  readFileSync,
  rmSync,
  writeFileSync,
} from "node:fs";
import { join } from "node:path";
import type { V2 } from "memax-sdk";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { renderConnect } from "../../src/commands/connect.js";
import { linkHome, unlinkHome } from "../../src/commands/link-home.js";
import {
  readHomeLink,
  writeHomeLink,
  type HomeFile,
} from "../../src/lib/daemon/home-link.js";
import { OneShotDelivery } from "../../src/lib/daemon/oneshot.js";
import {
  agentFor,
  runConnect,
  type ConnectDeps,
} from "../../src/lib/connect/run.js";
import {
  openCodeDir,
  removeInstruction,
} from "../../src/lib/connect/opencode.js";
import { harness, type Harness } from "../daemon/harness.js";
import { installInit } from "../init/fake-init.js";

// eslint-disable-next-line no-control-regex
const plain = (lines: string[]) =>
  lines.join("\n").replace(/\x1b\[[0-9;]*m/g, "");

let h: Harness;
let personal: V2.Space;
let machine: V2.Machine;
let canonical: V2.Target;
let dir: string;
let entry: string;

function file(t: V2.Target): HomeFile {
  return {
    target_id: t.id,
    kind: t.kind,
    path: t.path!,
    abs: join(h.home, t.path!),
    ...(t.settings.read_by?.length ? { read_by: t.settings.read_by } : {}),
  };
}

function link(files: V2.Target[]) {
  writeHomeLink(h.paths, {
    space_id: personal.id,
    space_slug: personal.slug,
    machine_id: machine.id,
    person_id: "ana",
    home: h.home,
    files: files.map(file),
  });
}

beforeEach(async () => {
  h = await harness();
  installInit(h.fake);
  mkdirSync(join(h.home, ".claude"), { recursive: true });
  personal = h.fake.addPersonalSpace();
  machine = h.fake.addMachine(personal, "device-1", "work-mbp");
  const rules = h.fake.addHomeTarget(personal, machine, "claude_user");
  rules.settings.read_by = ["claude-code"];
  canonical = h.fake.addHomeTarget(personal, machine, "agents_md");
  link([rules, canonical]);
  dir = join(h.home, ".config", "opencode");
  mkdirSync(dir, { recursive: true });
  entry = join(h.home, ".memax", "personal", "AGENTS.md");
  mkdirSync(join(h.home, ".memax", "personal"), { recursive: true });
  writeFileSync(entry, "<!-- Compiled by Memax from personal. -->\n");
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

const opencode = agentFor("opencode")!;
const read = (name: string) => readFileSync(join(dir, name), "utf8");
const updates = () => h.fake.calls("PATCH", /^\/v2\/targets\//);
const readers = () => h.fake.entry(canonical.id).target.settings.read_by;
const linkedReaders = () =>
  readHomeLink(h.paths)!.files.find((f) => f.kind === "agents_md")!.read_by;

describe("memax connect opencode on a linked machine", () => {
  it("names the canonical copy in the file OpenCode edits, comments kept; again changes nothing", async () => {
    const mine = '{\n  // my model\n  "model": "anthropic/claude"\n}\n';
    writeFileSync(join(dir, "opencode.json"), mine);
    const { d, compiles } = deps();
    const r = await runConnect(opencode, {}, d);
    expect(r.home).toEqual({
      state: "added",
      path: "~/.config/opencode/opencode.json",
      note: "~/.memax/personal/AGENTS.md, your Personal space, added to its instructions",
    });
    expect(read("opencode.json")).toBe(
      `{\n  // my model\n  "model": "anthropic/claude",\n  "instructions": [\n    ${JSON.stringify(entry)}\n  ]\n}\n`,
    );
    // Fading knows OpenCode reads it, and so does home.json.
    expect(readers()).toEqual(["opencode"]);
    expect(linkedReaders()).toEqual(["opencode"]);
    // The canonical was on disk already: nothing to compile.
    expect(compiles).toEqual([]);
    expect(plain(renderConnect(r, "https://memax.app"))).toContain(
      "Home     ~/.config/opencode/opencode.json: ~/.memax/personal/AGENTS.md, your Personal space, added to its instructions",
    );

    const before = read("opencode.json");
    const again = await runConnect(opencode, {}, d);
    expect(again.home).toMatchObject({
      state: "present",
      note: "its instructions name ~/.memax/personal/AGENTS.md, your Personal space",
    });
    expect(read("opencode.json")).toBe(before);
    expect(updates()).toHaveLength(1);
  });

  it("adds to the list OpenCode uses: the last of its files that sets one", async () => {
    writeFileSync(
      join(dir, "config.json"),
      '{ "instructions": ["~/notes.md"] }\n',
    );
    writeFileSync(join(dir, "opencode.jsonc"), "{\n  // nothing yet\n}\n");
    await runConnect(opencode, {}, deps().d);
    expect(read("config.json")).toBe(
      `{ "instructions": ["~/notes.md", ${JSON.stringify(entry)}] }\n`,
    );
    expect(read("opencode.jsonc")).toBe("{\n  // nothing yet\n}\n");
  });

  it("starts opencode.jsonc where there's no settings file, and reads XDG_CONFIG_HOME", async () => {
    const xdg = join(h.home, "xdg");
    mkdirSync(join(xdg, "opencode"), { recursive: true });
    const r = await runConnect(
      opencode,
      {},
      deps({ env: { XDG_CONFIG_HOME: xdg } }).d,
    );
    expect(r.home?.path).toBe("~/xdg/opencode/opencode.jsonc");
    expect(
      JSON.parse(readFileSync(join(xdg, "opencode", "opencode.jsonc"), "utf8")),
    ).toEqual({ instructions: [entry] });
    expect(existsSync(join(dir, "opencode.jsonc"))).toBe(false);
  });

  it("takes a ~/ entry the person wrote for the same file as present", async () => {
    writeFileSync(
      join(dir, "opencode.json"),
      '{ "instructions": ["~/.memax/personal/AGENTS.md"] }\n',
    );
    const r = await runConnect(opencode, {}, deps().d);
    expect(r.home?.state).toBe("present");
    expect(read("opencode.json")).toBe(
      '{ "instructions": ["~/.memax/personal/AGENTS.md"] }\n',
    );
    expect(readers()).toEqual(["opencode"]);
  });

  describe("where the Personal space is in ~/.claude/CLAUDE.md (claude_user's fallback)", () => {
    let fallback: V2.Target;
    beforeEach(() => {
      fallback = h.fake.addHomeTarget(
        personal,
        machine,
        "claude_user",
        ".claude/CLAUDE.md",
      );
      fallback.settings.read_by = ["claude-code"];
      link([fallback, canonical]);
    });

    it("writes no entry, since OpenCode reads that file, and takes out one Memax wrote before", async () => {
      writeFileSync(
        join(dir, "opencode.jsonc"),
        `{\n  "instructions": [\n    "~/notes.md", // mine\n    ${JSON.stringify(entry)}\n  ]\n}\n`,
      );
      h.fake.entry(canonical.id).target.settings.read_by = ["opencode"];
      const r = await runConnect(opencode, {}, deps().d);
      expect(r.home).toEqual({
        state: "present",
        path: "~/.claude/CLAUDE.md",
        note: "OpenCode reads your Personal space there, so Memax took ~/.memax/personal/AGENTS.md out of its instructions (~/.config/opencode/opencode.jsonc): it would load twice",
      });
      expect(read("opencode.jsonc")).toBe(
        '{\n  "instructions": [\n    "~/notes.md" // mine\n  ]\n}\n',
      );
      expect(readers()).toEqual([]);
      expect(h.fake.entry(fallback.id).target.settings.read_by).toEqual([
        "claude-code",
        "opencode",
      ]);
      const files = readHomeLink(h.paths)!.files;
      expect(
        files.find((f) => f.kind === "agents_md")!.read_by,
      ).toBeUndefined();
      expect(files.find((f) => f.kind === "claude_user")!.read_by).toEqual([
        "claude-code",
        "opencode",
      ]);
    });

    it.each([
      ["OpenCode has its own AGENTS.md, which it reads instead", {}, true],
      [
        "OPENCODE_DISABLE_CLAUDE_CODE_PROMPT is set",
        { OPENCODE_DISABLE_CLAUDE_CODE_PROMPT: "1" },
        false,
      ],
      [
        "OPENCODE_DISABLE_CLAUDE_CODE is set",
        { OPENCODE_DISABLE_CLAUDE_CODE: "true" },
        false,
      ],
    ])("writes the entry when %s", async (_, env, ownAgents) => {
      if (ownAgents) writeFileSync(join(dir, "AGENTS.md"), "# Mine\n");
      const r = await runConnect(opencode, {}, deps({ env }).d);
      expect(r.home?.state).toBe("added");
      expect(JSON.parse(read("opencode.jsonc"))).toEqual({
        instructions: [entry],
      });
    });
  });

  it("adds the canonical copy to the link when it isn't in it, and compiles it", async () => {
    const rules = h.fake.entry(
      readHomeLink(h.paths)!.files.find((f) => f.kind === "claude_user")!
        .target_id,
    ).target;
    link([rules]);
    rmSync(entry);
    const { d, compiles } = deps();
    const r = await runConnect(opencode, {}, d);
    expect(h.fake.calls("POST", /\/machines$/).at(-1)?.body).toMatchObject({
      files: [{ kind: "agents_md", path: ".memax/personal/AGENTS.md" }],
    });
    expect(r.home).toMatchObject({ state: "added", compile: "compiled" });
    expect(compiles).toEqual(["personal"]);
    expect(linkedReaders()).toEqual(["opencode"]);
  });

  it("leaves a settings file that isn't JSON as it is, and says so", async () => {
    writeFileSync(join(dir, "opencode.json"), "{ not json");
    const r = await runConnect(opencode, {}, deps().d);
    expect(r.home).toMatchObject({
      state: "refused",
      path: "~/.config/opencode/opencode.json",
    });
    expect(r.home?.note).toContain(
      "isn't valid JSON, so Memax didn't change it",
    );
    expect(read("opencode.json")).toBe("{ not json");
    expect(readers()).toBeUndefined();
  });

  it("says when OpenCode isn't set up here, and how to link the home", async () => {
    rmSync(dir, { recursive: true });
    const r = await runConnect(opencode, {}, deps().d);
    expect(plain(renderConnect(r, "https://memax.app"))).toContain(
      "OpenCode isn't set up in this home folder (~/.config/opencode)",
    );
    rmSync(h.paths.home);
    const off = await runConnect(opencode, { space: "personal" }, deps().d);
    expect(off.home).toEqual({ state: "not_linked" });
    expect(plain(renderConnect(off, "https://memax.app"))).toContain(
      "memax link --home, then this again, adds your Personal space to OpenCode's instructions",
    );
  });
});

describe("taking the entry out", () => {
  it("removes only Memax's entry, and a list it leaves empty", () => {
    writeFileSync(
      join(dir, "config.json"),
      `{ "instructions": ["~/notes.md", ${JSON.stringify(entry)}] }\n`,
    );
    writeFileSync(
      join(dir, "opencode.jsonc"),
      `{\n  // mine\n  "model": "a/b",\n  "instructions": [${JSON.stringify(entry)}]\n}\n`,
    );
    writeFileSync(
      join(dir, "opencode.json"),
      '{ "instructions": ["~/.memax/personal/AGENTS.md"] }\n',
    );
    const e = { home: h.home, env: {} };
    expect(openCodeDir(e)).toBe(dir);
    expect(removeInstruction(e, entry)).toEqual([
      join(dir, "config.json"),
      join(dir, "opencode.jsonc"),
    ]);
    expect(read("config.json")).toBe('{ "instructions": ["~/notes.md"] }\n');
    // An empty list would hide config.json's, so it goes.
    expect(read("opencode.jsonc")).toBe('{\n  // mine\n  "model": "a/b"\n}\n');
    // The person's own entry, in their own words, stays.
    expect(read("opencode.json")).toBe(
      '{ "instructions": ["~/.memax/personal/AGENTS.md"] }\n',
    );
  });

  it("memax unlink --home takes it out with the layer", async () => {
    await runConnect(opencode, {}, deps().d);
    expect(JSON.parse(read("opencode.jsonc")).instructions).toEqual([entry]);
    const out: string[] = [];
    const oneShot = () =>
      OneShotDelivery.open({
        paths: h.paths,
        api: h.api,
        deviceId: "device-1",
        log: h.log,
        version: "test",
        apiUrl: h.fake.url,
      });
    expect(
      await unlinkHome({
        memax: h.memax,
        paths: h.paths,
        out: (l) => out.push(l),
        oneShot,
        env: {},
      }),
    ).toBe(0);
    expect(JSON.parse(read("opencode.jsonc"))).toEqual({});
    expect(plain(out)).toContain(
      "~/.config/opencode/opencode.jsonc  ~/.memax/personal/AGENTS.md taken out of OpenCode's instructions",
    );
  });

  it("memax link --home says memax connect opencode adds it", async () => {
    rmSync(h.paths.home);
    h.fake.machines.splice(0);
    const out: string[] = [];
    await linkHome(
      { yes: true, timeout: "1" },
      {
        memax: h.memax,
        paths: h.paths,
        cwd: h.home,
        out: (l) => out.push(l),
        interactive: true,
        home: h.home,
        env: {},
        platform: "linux",
        deviceId: "4b0e7a2c-5d1f-4c8e-9a6b-3f2d1e0c9b8a",
        personId: "ana",
        confirm: async () => true,
        oneShot: async () => null,
        pollMs: 20,
      },
    );
    expect(plain(out)).toContain(
      "OpenCode reads your Personal space once its instructions name ~/.memax/personal/AGENTS.md: memax connect opencode adds it.",
    );
  });
});
