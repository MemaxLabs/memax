// memax switch against a fake /v2: --dry-run changes nothing, it asks
// first (or needs --yes), waits for a switch running in the background,
// resumes a failed one, and --back switches back. The one-release notice
// of memax agents sync is here too.
import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import type { Memax, V2 } from "memax-sdk";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import {
  renderSwitch,
  switchExitCode,
  previewLines,
} from "../../src/commands/switch.js";
import { spaceOnV2Notice } from "../../src/commands/agent-configs.js";
import type { DaemonPaths } from "../../src/lib/daemon/paths.js";
import {
  runSwitch,
  type SwitchDeps,
  type SwitchOptions,
} from "../../src/lib/switch/run.js";

const SPACE: V2.Space = {
  id: "0195e1a0-0000-7000-8000-000000000001",
  tenant_id: "0195e1a0-0000-7000-8000-0000000000aa",
  slug: "acme-web",
  name: "acme-web",
  kind: "team",
  role: "owner",
};

function status(over: Partial<V2.SpaceSwitch> = {}): V2.SpaceSwitch {
  return {
    space: { ...SPACE },
    state: "v1",
    step: "space",
    attempts: 0,
    background: false,
    preview: {
      kind: "team",
      kinds: ["team", "project"],
      suggested_repository: "acme/web",
      members: [
        {
          person_id: "p1",
          name: "Ziyang",
          v1_role: "owner",
          role: "owner",
          can_forget: true,
        },
        {
          person_id: "p2",
          name: "Ada",
          v1_role: "admin",
          role: "member",
          can_forget: true,
        },
        {
          person_id: "p3",
          name: "Lin",
          v1_role: "viewer",
          role: "viewer",
          can_forget: false,
        },
      ],
      notes: {
        total: 112,
        person: 90,
        agent: 22,
        candidates: 84,
        fold: 25,
        kept: 3,
        long: 3,
        secret: 1,
        archived: 2,
        format: 0,
        external: 0,
        seeds: 4,
      },
      personas: 0,
      configs: [
        {
          path: "CLAUDE.md",
          agent: "claude-code",
          scope: "project:https://github.com/acme/web",
          targets: ["claude_md"],
        },
      ],
      targets: ["agents_md", "claude_md"],
      agents: [
        {
          credential: "api_key",
          name: "Claude Code",
          agent: "claude-code",
          person_id: "p1",
          autonomy: "propose",
          connected: false,
        },
      ],
      gates: 1,
      dream_runs: 4,
      plan: "pro",
      empty: false,
    },
    progress: {
      notes: 0,
      personas: 0,
      configs: 0,
      targets: [],
      proposed: 0,
      folded: 0,
      existing: 0,
      refused: 0,
      imports: [],
      connected: 0,
      already_connected: 0,
      notified: 0,
      gates_moved: 0,
      gates_left: 0,
    },
    ...over,
  } as V2.SpaceSwitch;
}

const SWITCHED = (): V2.SpaceSwitch =>
  status({
    space: { ...SPACE, kind: "project", v2_enabled_at: "2026-10-07T09:00:00Z" },
    state: "switched",
    step: "done",
    attempts: 1,
    import_id: "0195e1a0-0000-7000-8000-0000000000ff",
    progress: {
      notes: 112,
      personas: 0,
      configs: 1,
      targets: ["agents_md", "claude_md"],
      proposed: 84,
      folded: 0,
      existing: 0,
      refused: 0,
      imports: ["0195e1a0-0000-7000-8000-0000000000ff"],
      connected: 1,
      already_connected: 0,
      notified: 1,
      gates_moved: 1,
      gates_left: 0,
    },
  });

interface Fake {
  memax: Memax;
  calls: string[];
  keys: string[];
  /** What switchStatus answers, in turn (the last one repeats). */
  statuses: V2.SpaceSwitch[];
  started: V2.SpaceSwitch;
}

function fake(initial: V2.SpaceSwitch): Fake {
  const f: Fake = {
    calls: [],
    keys: [],
    statuses: [initial],
    started: SWITCHED(),
    memax: undefined as unknown as Memax,
  };
  const next = () =>
    f.statuses.length > 1 ? f.statuses.shift()! : f.statuses[0];
  f.memax = {
    v2: {
      spaces: {
        list: async () => ({ items: [SPACE] }),
        switchStatus: async (space: string) => {
          f.calls.push(`status ${space}`);
          return next();
        },
        switchToV2: async (
          space: string,
          o: { idempotencyKey: string; kind?: string; repository?: string },
        ) => {
          f.calls.push(`v2 ${space} ${o.kind ?? ""} ${o.repository ?? ""}`);
          f.keys.push(o.idempotencyKey);
          return f.started;
        },
        switchToV1: async (space: string, o: { idempotencyKey: string }) => {
          f.calls.push(`v1 ${space}`);
          f.keys.push(o.idempotencyKey);
          return status({ state: "off", step: "done" });
        },
      },
    },
  } as unknown as Memax;
  return f;
}

let cwd: string;
beforeEach(() => {
  cwd = mkdtempSync(join(tmpdir(), "memax-switch-"));
});
afterEach(() => rmSync(cwd, { recursive: true, force: true }));

function deps(f: Fake, over: Partial<SwitchDeps> = {}): SwitchDeps {
  let t = 0;
  return {
    memax: f.memax,
    paths: {} as DaemonPaths,
    cwd,
    confirm: null,
    sleep: async (ms) => {
      t += ms;
    },
    now: () => t,
    ...over,
  };
}

const text = (lines: string[]) =>
  // eslint-disable-next-line no-control-regex -- strips chalk's colours
  lines.join("\n").replace(/\u001b\[[0-9;]*m/g, "");

describe("memax switch", () => {
  it("--dry-run shows what moves and changes nothing", async () => {
    const f = fake(status());
    const o: SwitchOptions = { space: "acme-web", dryRun: true };
    const r = await runSwitch(o, deps(f));
    expect(r.outcome).toBe("preview");
    expect(f.calls).toEqual([`status ${SPACE.id}`]);
    const out = text(renderSwitch(r, o, "https://memax.app"));
    expect(out).toContain("switch acme-web to V2");
    expect(out).toContain("(dry run)");
    expect(out).toContain(
      "112 V1 memories become notes (N-): searchable, never compiled, nothing lost",
    );
    expect(out).toContain(
      "84 are yours, one statement each: keep them in one go",
    );
    expect(out).toContain("25 for Dream to fold into proposals");
    expect(out).toContain(
      "3 stay notes, never proposed (2 archived, 1 holds a credential)",
    );
    expect(out).toContain("1 owner, 1 member who can forget, 1 viewer");
    expect(out).toContain("Claude Code at Propose");
    expect(out).toContain(
      "AGENTS.md, CLAUDE.md; two-way sync of 1 agent file stops",
    );
    expect(out).toContain("4 V1 Dream runs, kept read-only");
    expect(out).toContain("(or --as project)");
    expect(out).toContain("Nothing changed.");
    expect(switchExitCode(r)).toBe(0);
  });

  it("asks first; with nobody to ask it needs --yes", async () => {
    const f = fake(status());
    const r = await runSwitch({ space: "acme-web" }, deps(f));
    expect(r.outcome).toBe("needs_yes");
    expect(switchExitCode(r)).toBe(2);
    expect(f.calls.some((c) => c.startsWith("v2"))).toBe(false);

    let asked = 0;
    const no = await runSwitch(
      { space: "acme-web" },
      deps(f, {
        confirm: async () => {
          asked++;
          return false;
        },
      }),
    );
    expect(no.outcome).toBe("declined");
    expect(asked).toBe(1);
    expect(f.calls.some((c) => c.startsWith("v2"))).toBe(false);
  });

  it("switches, waits for the background, and says where Review is", async () => {
    const running = status({
      state: "running",
      step: "candidates",
      background: true,
    });
    const f = fake(status());
    f.started = running;
    f.statuses.push(running, SWITCHED());
    const o: SwitchOptions = {
      space: "acme-web",
      yes: true,
      as: "project",
      repository: "acme/web",
    };
    const r = await runSwitch(o, deps(f));
    expect(r.outcome).toBe("switched");
    expect(f.calls).toContain(`v2 ${SPACE.id} project acme/web`);
    const out = text(renderSwitch(r, o, "https://memax.app"));
    expect(out).toContain("acme-web is on V2");
    expect(out).toContain(
      "84 of yours wait to be kept in one go: https://memax.app/acme-web/review?filter=import&import=0195e1a0-0000-7000-8000-0000000000ff",
    );
    expect(out).toContain(
      "1 agent connected; 1 told on their next MCP response",
    );
    expect(out).toContain("V1 two-way sync is off for this space");
    expect(switchExitCode(r)).toBe(0);
  });

  it("gives up waiting, and a failed switch resumes when run again", async () => {
    const running = status({
      state: "running",
      step: "notes",
      background: true,
    });
    const f = fake(status());
    f.started = running;
    f.statuses = [status(), running];
    const r = await runSwitch(
      { space: "acme-web", yes: true, wait: "3" },
      deps(f),
    );
    expect(r.outcome).toBe("running");
    expect(text(renderSwitch(r, { yes: true }, ""))).toContain(
      "still switching (notes)",
    );

    const failed = status({
      state: "failed",
      step: "configs",
      error: "internal",
      attempts: 1,
    });
    const g = fake(failed);
    g.started = failed;
    const stopped = await runSwitch({ space: "acme-web", yes: true }, deps(g));
    expect(stopped.outcome).toBe("failed");
    expect(switchExitCode(stopped)).toBe(1);
    expect(text(renderSwitch(stopped, { yes: true }, ""))).toContain(
      "stopped at configs (internal); run memax switch --space acme-web again to resume it",
    );
    g.started = SWITCHED();
    const resumed = await runSwitch({ space: "acme-web", yes: true }, deps(g));
    expect(resumed.outcome).toBe("switched");
    expect(g.keys).toHaveLength(2);
    expect(new Set(g.keys).size).toBe(2);
  });

  it("does nothing for a space on V2 already", async () => {
    const f = fake(SWITCHED());
    const r = await runSwitch({ space: "acme-web", yes: true }, deps(f));
    expect(r.outcome).toBe("already");
    expect(f.calls).toEqual([`status ${SPACE.id}`]);
  });

  it("--back switches back to V1, and says so on V1", async () => {
    const f = fake(SWITCHED());
    const r = await runSwitch(
      { space: "acme-web", back: true, yes: true },
      deps(f),
    );
    expect(r.outcome).toBe("switched_back");
    expect(f.calls).toContain(`v1 ${SPACE.id}`);
    expect(text(renderSwitch(r, { back: true }, ""))).toContain(
      "acme-web is back on V1: every surface serves it as before",
    );

    const g = fake(status());
    const v1 = await runSwitch(
      { space: "acme-web", back: true, yes: true },
      deps(g),
    );
    expect(v1.outcome).toBe("on_v1");
    expect(g.calls.some((c) => c.startsWith("v1"))).toBe(false);
  });

  it("names a space it doesn't know", async () => {
    const f = fake(status());
    await expect(
      runSwitch({ space: "nope", dryRun: true }, deps(f)),
    ).rejects.toThrow(/nope isn't one of your spaces/);
  });

  it("previews a personal space without kinds to choose", () => {
    const st = status();
    st.preview.kind = "personal";
    st.preview.kinds = ["personal"];
    st.preview.personas = 2;
    const out = text(previewLines(st).map(([, l, t]) => `${l} ${t}`));
    expect(out).toContain("switches as a personal space");
    expect(out).not.toContain("--as");
    expect(out).toContain("2 personas become notes");
  });
});

describe("memax agents sync, for spaces on V2", () => {
  it("says once that two-way sync is off", () => {
    expect(spaceOnV2Notice(1).join("\n")).toContain(
      "Two-way sync is off for 1 agent file of spaces on V2",
    );
    expect(spaceOnV2Notice(3).join("\n")).toContain("3 agent files");
  });
});
