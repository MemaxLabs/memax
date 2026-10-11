// A home link (personal-layer design §6): the daemon writes the Personal
// space into this machine's agents' own files, exactly the ones home.json
// names; never over a hand edit (rule 6); and takes the layer off again,
// byte for byte, when the machine is unlinked or signed out, never on a
// transient failure.
import {
  existsSync,
  mkdirSync,
  readFileSync,
  statSync,
  symlinkSync,
  writeFileSync,
} from "node:fs";
import { dirname, join } from "node:path";
import type { V2 } from "memax-sdk";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import type { Daemon } from "../../src/lib/daemon/daemon.js";
import { readHomeLink, writeHomeLink } from "../../src/lib/daemon/home-link.js";
import { daemonPaths, type DaemonPaths } from "../../src/lib/daemon/paths.js";
import { upsertManagedBlock } from "../../../compiler/src/index.js";
import { harness, until, type Harness } from "./harness.js";

let h: Harness;
beforeEach(async () => {
  h = await harness();
});
afterEach(async () => {
  await h.cleanup();
});

const RULES = ".claude/rules/memax-personal.md";
const CODEX = ".codex/AGENTS.md";
const CANON = ".memax/personal/AGENTS.md";
/** The person's own Codex file: CRLF, a blank line of theirs, no final newline. */
const MINE =
  "# My Codex notes\r\n\r\nPrefer rg to grep.\r\n\r\nNo final newline";
const PERSONAL_1 = [
  "<!-- Compiled by Memax from personal (C-0901). -->",
  "- Answer in British English. [M-0001]",
];
const PERSONAL_2 = [...PERSONAL_1, "- Prefer small pull requests. [M-0002]"];
const rulesFile = (lines: string[]) => lines.join("\n") + "\n";

interface Machine {
  home: string;
  paths: DaemonPaths;
  machine: V2.Machine;
  targets: Record<string, V2.Target>;
  daemon: Daemon;
}

/** A machine with Claude Code and Codex set up, linked to Personal. */
function machine(
  space: V2.Space,
  opts: { home?: string; device?: string; codex?: boolean } = {},
): Machine {
  const home = opts.home ?? h.home;
  mkdirSync(join(home, ".claude"), { recursive: true });
  if (opts.codex !== false)
    mkdirSync(join(home, ".codex"), { recursive: true });
  const paths =
    home === h.home ? h.paths : daemonPaths(join(home, ".memax", "daemon"));
  const m = h.fake.addMachine(space, opts.device);
  const targets = {
    [RULES]: h.fake.addHomeTarget(space, m, "claude_user"),
    [CODEX]: h.fake.addHomeTarget(space, m, "codex_user"),
    [CANON]: h.fake.addHomeTarget(space, m, "agents_md"),
  };
  writeHomeLink(paths, {
    space_id: space.id,
    space_slug: space.slug,
    machine_id: m.id,
    home,
    files: Object.entries(targets).map(([path, t]) => ({
      target_id: t.id,
      kind: t.kind,
      path,
      abs: join(home, path),
    })),
  });
  const daemon = h.daemon({ paths, deviceId: opts.device ?? "device-1" });
  return { home, paths, machine: m, targets, daemon };
}

/** One compile of every file of a machine, as the server compiles them. */
function compileAll(m: Machine, lines: string[]): void {
  h.fake.compile(m.targets[RULES].id, { [RULES]: rulesFile(lines) });
  h.fake.compile(m.targets[CODEX].id, { [CODEX]: lines });
  h.fake.compile(m.targets[CANON].id, { [CANON]: rulesFile(lines) });
}

const read = (m: Machine, path: string) =>
  readFileSync(join(m.home, path), "utf8");
const mode = (p: string) => statSync(p).mode & 0o777;
const snap = (m: Machine) => m.daemon.snapshot().home!;
const stateOf = (m: Machine, path: string) =>
  snap(m).targets.find((t) => t.id === m.targets[path].id);

describe("a home link", () => {
  it("writes Memax's own files whole, and one block in Codex's file, the person's bytes untouched", async () => {
    const space = h.fake.addPersonalSpace();
    const m = machine(space);
    writeFileSync(join(h.home, CODEX), MINE);
    compileAll(m, PERSONAL_1);
    await m.daemon.syncOnce();

    expect(read(m, RULES)).toBe(rulesFile(PERSONAL_1));
    expect(read(m, CANON)).toBe(rulesFile(PERSONAL_1));
    // New files are 0600; the folders Memax created, 0700.
    expect(mode(join(h.home, RULES))).toBe(0o600);
    expect(mode(join(h.home, CANON))).toBe(0o600);
    expect(mode(join(h.home, ".claude", "rules"))).toBe(0o700);
    expect(mode(join(h.home, ".memax", "personal"))).toBe(0o700);
    // The person's file keeps every byte; the block follows its last line.
    const codex = read(m, CODEX);
    expect(codex).toBe(upsertManagedBlock(MINE, PERSONAL_1));
    expect(codex.startsWith(`${MINE}\r\n<!-- memax:start -->\r\n`)).toBe(true);
    for (const t of Object.values(m.targets))
      expect(h.fake.entry(t.id).target.sync_state).toBe("in_sync");

    // The next compile replaces the block in place.
    compileAll(m, PERSONAL_2);
    await m.daemon.syncOnce();
    expect(read(m, CODEX)).toBe(upsertManagedBlock(MINE, PERSONAL_2));
    expect(read(m, RULES)).toContain("[M-0002]");
    expect(h.fake.calls("POST", /\/observations$/)).toHaveLength(0);
  });

  it("never creates an agent's own folder: a missing ~/.codex writes nothing there", async () => {
    const space = h.fake.addPersonalSpace();
    const m = machine(space, { codex: false });
    compileAll(m, PERSONAL_1);
    await m.daemon.syncOnce();
    expect(existsSync(join(h.home, ".codex"))).toBe(false);
    expect(stateOf(m, CODEX)).toMatchObject({
      state: "blocked",
      detail: expect.stringContaining("Codex isn't set up here any more"),
    });
    expect(read(m, RULES)).toBe(rulesFile(PERSONAL_1));
  });

  it("refuses a file inside a git work tree at write time: dotfiles would commit it", async () => {
    const space = h.fake.addPersonalSpace();
    const m = machine(space);
    compileAll(m, PERSONAL_1);
    await m.daemon.syncOnce();
    const before = read(m, CODEX);
    // ~/.codex became part of a dotfiles repository since the link.
    mkdirSync(join(h.home, ".codex", ".git"));
    compileAll(m, PERSONAL_2);
    await m.daemon.syncOnce();
    expect(read(m, CODEX)).toBe(before);
    expect(stateOf(m, CODEX)?.detail).toContain(
      "~/.codex is in ~/.codex, a git repository. Your Personal space would be committed with it",
    );
    expect(read(m, RULES)).toContain("[M-0002]");
  });

  it("refuses a symlinked folder on the way", async () => {
    const space = h.fake.addPersonalSpace();
    const m = machine(space);
    const elsewhere = join(dirname(h.home), "elsewhere");
    mkdirSync(elsewhere);
    symlinkSync(elsewhere, join(h.home, ".claude", "rules"));
    compileAll(m, PERSONAL_1);
    await m.daemon.syncOnce();
    expect(existsSync(join(elsewhere, "memax-personal.md"))).toBe(false);
    expect(stateOf(m, RULES)).toMatchObject({ state: "blocked" });
    expect(stateOf(m, RULES)?.detail).toContain("symlink");
  });

  it("writes only the file home.json names, so the server can't move a write", async () => {
    const space = h.fake.addPersonalSpace();
    const m = machine(space);
    // The server's target now says Codex's override file.
    h.fake.entry(m.targets[CODEX].id).target.path = ".codex/AGENTS.override.md";
    h.fake.compile(m.targets[CODEX].id, {
      ".codex/AGENTS.override.md": PERSONAL_1,
    });
    await m.daemon.syncOnce();
    expect(existsSync(join(h.home, ".codex", "AGENTS.override.md"))).toBe(
      false,
    );
    expect(existsSync(join(h.home, CODEX))).toBe(false);
    expect(stateOf(m, CODEX)?.detail).toContain(
      "isn't .codex/AGENTS.md, the file you approved for it on this machine",
    );
  });
});

describe("the warm cache's home entry (the hook's Personal section)", () => {
  it("holds this machine's home files and their compiles, and goes with the link", async () => {
    const space = h.fake.addPersonalSpace();
    const m = machine(space);
    compileAll(m, PERSONAL_1);
    await m.daemon.syncOnce();
    const warm = () =>
      JSON.parse(readFileSync(m.paths.warm, "utf8")) as {
        home?: {
          space_id: string;
          machine_id: string;
          targets: Array<{ kind: string; path: string; compile?: string }>;
        };
      };
    const home = warm().home!;
    expect(home).toMatchObject({
      space_id: space.id,
      machine_id: m.machine.id,
    });
    expect(home.targets.map((t) => [t.kind, t.path]).sort()).toEqual([
      ["agents_md", CANON],
      ["claude_user", RULES],
      ["codex_user", CODEX],
    ]);
    expect(home.targets.every((t) => /^C-\d+$/.test(t.compile ?? ""))).toBe(
      true,
    );

    h.fake.unlinkMachine(m.machine.id);
    await m.daemon.homeOff("unlinked");
    await m.daemon.syncOnce();
    expect(warm().home).toBeUndefined();
  });
});

describe("two machines (rule 6)", () => {
  it("a hand edit in one machine's block holds that machine only; the other keeps updating with its own lines intact", async () => {
    const space = h.fake.addPersonalSpace();
    const a = machine(space, { device: "device-a" });
    const homeB = join(dirname(h.home), "home-b");
    const b = machine(space, { home: homeB, device: "device-b" });
    const mineB = "## B's own lines\n- tabs\n";
    writeFileSync(join(a.home, CODEX), MINE);
    writeFileSync(join(b.home, CODEX), mineB);
    compileAll(a, PERSONAL_1);
    compileAll(b, PERSONAL_1);
    await a.daemon.syncOnce();
    await b.daemon.syncOnce();
    expect(read(b, CODEX)).toBe(upsertManagedBlock(mineB, PERSONAL_1));

    // On A, the person edits inside Memax's block.
    const edited = read(a, CODEX).replace(
      "British English",
      "Australian English",
    );
    writeFileSync(join(a.home, CODEX), edited);
    await a.daemon.checkFile(a.home, CODEX);
    expect(h.fake.entry(a.targets[CODEX].id).target.sync_state).toBe("drifted");
    // Only the block went up: none of the person's own lines.
    const sent = h.fake.calls("POST", /\/observations$/)[0].body as {
      content: string;
    };
    expect(sent.content).not.toContain("Prefer rg to grep");
    expect(sent.content).toContain("Australian English");

    compileAll(a, PERSONAL_2);
    compileAll(b, PERSONAL_2);
    await a.daemon.syncOnce();
    await b.daemon.syncOnce();
    expect(read(a, CODEX)).toBe(edited);
    expect(read(b, CODEX)).toBe(upsertManagedBlock(mineB, PERSONAL_2));
    expect(h.fake.entry(b.targets[CODEX].id).target.sync_state).toBe("in_sync");
  });
});

describe("taking the layer off (§6.6)", () => {
  async function delivered(): Promise<Machine> {
    const space = h.fake.addPersonalSpace();
    const m = machine(space);
    writeFileSync(join(h.home, CODEX), MINE);
    compileAll(m, PERSONAL_1);
    await m.daemon.syncOnce();
    expect(read(m, RULES)).toBe(rulesFile(PERSONAL_1));
    return m;
  }

  it("unlinked on the web: Memax's files go, its block comes out byte for byte, an edited file stays", async () => {
    const m = await delivered();
    // A hand edit in the canonical copy: it stays, and says why.
    const canon = read(m, CANON) + "- my own line\n";
    writeFileSync(join(h.home, CANON), canon);
    h.fake.unlinkMachine(m.machine.id);
    await m.daemon.syncOnce();

    expect(existsSync(join(h.home, RULES))).toBe(false);
    expect(read(m, CODEX)).toBe(MINE);
    expect(read(m, CANON)).toBe(canon);
    expect(stateOf(m, RULES)?.state).toBe("off_file_removed");
    expect(stateOf(m, CODEX)?.state).toBe("off_block_removed");
    expect(stateOf(m, CANON)?.state).toBe("off_file_kept");
  });

  it("signed out: off without asking the server, home.json kept; signing in puts it back with no false hand edit", async () => {
    const m = await delivered();
    const requests = h.fake.log.length;
    h.fake.fail(/./, 401, "unauthorized", 1_000);
    await m.daemon.homeOff("signed_out");
    // No request was needed to decide what was Memax's.
    expect(h.fake.log.length).toBe(requests);
    expect(existsSync(join(h.home, RULES))).toBe(false);
    expect(existsSync(join(h.home, CANON))).toBe(false);
    expect(read(m, CODEX)).toBe(MINE);
    expect(readHomeLink(m.paths)).not.toBeNull();
    expect(snap(m).signed_out).toBe(true);

    // Polls keep failing while signed out: nothing is written.
    await m.daemon.syncOnce();
    expect(existsSync(join(h.home, RULES))).toBe(false);

    // memax login: the next poll succeeds and delivers everything again.
    h.fake.clearFaults();
    await m.daemon.syncOnce();
    expect(read(m, RULES)).toBe(rulesFile(PERSONAL_1));
    expect(read(m, CODEX)).toBe(upsertManagedBlock(MINE, PERSONAL_1));
    expect(h.fake.calls("POST", /\/observations$/)).toHaveLength(0);
    expect(snap(m).signed_out).toBeUndefined();
  });

  it("a refused refresh takes it off once, through signedOut()", async () => {
    const m = await delivered();
    m.daemon.signedOut();
    m.daemon.signedOut();
    await until(
      () => !existsSync(join(h.home, CANON)) && read(m, CODEX) === MINE,
      3_000,
      "the layer off",
    );
    expect(existsSync(join(h.home, RULES))).toBe(false);
    expect(snap(m).signed_out).toBe(true);
  });

  it("never on a transient failure: 5xx, 429 and an unreachable server leave every file as it is", async () => {
    const m = await delivered();
    const before = [RULES, CODEX, CANON].map((p) => read(m, p));
    h.fake.fail(/./, 503, "unavailable", 3);
    await m.daemon.syncOnce();
    h.fake.fail(/./, 429, "rate_limited", 3, { "Retry-After": "1" });
    await m.daemon.syncOnce();
    await h.fake.stop();
    await m.daemon.syncOnce();
    expect([RULES, CODEX, CANON].map((p) => read(m, p))).toEqual(before);
    expect(snap(m).error).toBeTruthy();
  });

  it("removes a Codex file Memax made for its block: before, it wasn't there", async () => {
    const space = h.fake.addPersonalSpace();
    const m = machine(space);
    compileAll(m, PERSONAL_1);
    await m.daemon.syncOnce();
    expect(read(m, CODEX)).toBe(upsertManagedBlock("", PERSONAL_1));
    expect(mode(join(h.home, CODEX))).toBe(0o600);
    h.fake.unlinkMachine(m.machine.id);
    await m.daemon.syncOnce();
    expect(existsSync(join(h.home, CODEX))).toBe(false);
    expect(existsSync(join(h.home, ".codex"))).toBe(true);
  });

  it("keeps a file Memax made once the person wrote in it", async () => {
    const space = h.fake.addPersonalSpace();
    const m = machine(space);
    compileAll(m, PERSONAL_1);
    await m.daemon.syncOnce();
    writeFileSync(join(h.home, CODEX), read(m, CODEX) + "- mine, below\n");
    h.fake.unlinkMachine(m.machine.id);
    await m.daemon.syncOnce();
    expect(read(m, CODEX)).toBe("- mine, below\n");
  });

  it("a definitive 404 on the Personal space takes it off, home.json kept; a proxy's 404 or a 503 doesn't", async () => {
    const m = await delivered();
    const before = [RULES, CODEX, CANON].map((p) => read(m, p));
    // A 404 that isn't the API's own answer, and a transient failure.
    h.fake.fail(/\/targets$/, 404, "invalid_response");
    await m.daemon.syncOnce();
    h.fake.fail(/\/targets$/, 503, "unavailable");
    await m.daemon.syncOnce();
    expect([RULES, CODEX, CANON].map((p) => read(m, p))).toEqual(before);

    // The space forgotten or deleted, or not this session's person's.
    h.fake.fail(/\/targets$/, 404, "not_found", 1_000);
    await m.daemon.syncOnce();
    expect(existsSync(join(h.home, RULES))).toBe(false);
    expect(existsSync(join(h.home, CANON))).toBe(false);
    expect(read(m, CODEX)).toBe(MINE);
    expect(readHomeLink(m.paths)).not.toBeNull();
    expect(snap(m).gone).toBe(true);

    // It answers again: the files come back.
    h.fake.clearFaults();
    await m.daemon.syncOnce();
    expect(read(m, RULES)).toBe(rulesFile(PERSONAL_1));
    expect(snap(m).gone).toBeUndefined();
  });

  // Polls back off on errors; 15 s leaves room for a few on a busy runner.
  it(
    "the running daemon's poll takes it off on that 404 too",
    { timeout: 15_000 },
    async () => {
      const m = await delivered();
      await m.daemon.start();
      h.fake.fail(/./, 404, "not_found", 10_000);
      await until(
        () =>
          !existsSync(join(h.home, CANON)) &&
          !existsSync(join(h.home, RULES)) &&
          read(m, CODEX) === MINE,
        10_000,
        "the layer off after a 404",
      );
      expect(readHomeLink(m.paths)).not.toBeNull();
    },
  );

  it("memax unlink --home: off over the control request, home.json gone", async () => {
    const m = await delivered();
    h.fake.unlinkMachine(m.machine.id);
    const out = await m.daemon.homeOff("unlinked");
    expect(out?.targets.map((t) => t.state).sort()).toEqual([
      "off_block_removed",
      "off_file_removed",
      "off_file_removed",
    ]);
    expect(readHomeLink(m.paths)).toBeNull();
    expect(m.daemon.snapshot().home).toBeUndefined();
    expect(read(m, CODEX)).toBe(MINE);
  });
});
