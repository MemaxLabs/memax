import { describe, expect, it } from "vitest";
import {
  groupTargets,
  HOME_FILE_OF_AGENT,
  homeAgent,
  homeFileStatus,
  homeGroups,
  homeSyncLine,
  isBlock,
  machineAgents,
  machineCopies,
  machineFileState,
  primaryTarget,
  reachOf,
  siblingsOf,
  spaceSyncLine,
  targetCounts,
} from "./home-files";
import {
  isOnline,
  latestLinked,
  machineName,
  orderMachines,
  validMachineName,
  type MachineView,
} from "./machines";
import type { TargetKind, TargetView } from "./targets";

// A personal space's home files, one target per file per machine, read
// as people see them: grouped by file, each machine's copy with its
// state, a file's status across machines, the counts and the status line.

const NOW = new Date("2026-10-05T14:40:00-07:00");
const minutesAgo = (n: number) =>
  new Date(NOW.getTime() - n * 60_000).toISOString();

function machine(n: number, over: Partial<MachineView> = {}): MachineView {
  return {
    id: `m${n}`,
    label: `machine-${n}`,
    name: `mbp-${n}`,
    platform: "darwin",
    linked: true,
    linkedAt: "2026-09-28T09:00:00Z",
    unlinkedAt: null,
    lastSeenAt: minutesAgo(2),
    sessionId: null,
    version: 1,
    ...over,
  };
}

function home(
  n: number,
  m: MachineView,
  kind: TargetKind,
  over: Partial<TargetView> = {},
): TargetView {
  const path =
    kind === "claude_user"
      ? ".claude/rules/memax-personal.md"
      : kind === "codex_user"
        ? ".codex/AGENTS.md"
        : ".memax/personal/AGENTS.md";
  return {
    id: `t${n}`,
    slug: `t${n}`,
    kind,
    label: `~/${path}`,
    path,
    root: "home",
    machine: { id: m.id, label: m.label, name: m.name },
    readBy:
      kind === "claude_user"
        ? ["claude-code"]
        : kind === "codex_user"
          ? ["codex"]
          : [],
    reads: null,
    syncState: "in_sync",
    delivery: "local",
    settings: { include: "kept_and_open", stale: "mark", sizeBudget: 6144 },
    version: 1,
    openDrift: 0,
    holding: [],
    lastCompile: null,
    ...over,
  };
}

const repo: TargetView = {
  ...home(0, machine(0), "agents_md"),
  id: "r1",
  slug: "agents-md",
  label: "AGENTS.md",
  path: "AGENTS.md",
  root: "repo",
  machine: null,
  readBy: [],
};

const laptop = machine(1, { name: "work-mbp" });
const desktop = machine(2, {
  name: "home-linux",
  platform: "linux",
  lastSeenAt: "2026-10-02T21:10:00-07:00",
});
const MACHINES = [laptop, desktop];

describe("Gemini's home file", () => {
  it("is a block in ~/.gemini/AGENTS.md, written for Gemini", () => {
    const gemini = { kind: "gemini_user" as const, path: ".gemini/AGENTS.md" };
    expect(isBlock(gemini)).toBe(true);
    expect(homeAgent("gemini_user")).toBe("gemini");
    expect(HOME_FILE_OF_AGENT.gemini).toBe("~/.gemini/AGENTS.md");
  });

  it("and Copilot's is a block in ~/.copilot/copilot-instructions.md", () => {
    expect(
      isBlock({
        kind: "copilot_user",
        path: ".copilot/copilot-instructions.md",
      }),
    ).toBe(true);
    expect(homeAgent("copilot_user")).toBe("copilot");
  });
});

describe("grouping home files", () => {
  it("lists each home file once, where its first copy was, with every machine's copy in machine order", () => {
    const targets = [
      home(4, desktop, "claude_user"),
      repo,
      home(1, laptop, "claude_user"),
      home(2, laptop, "codex_user"),
      home(5, desktop, "codex_user"),
    ];
    const entries = groupTargets(targets);
    expect(
      entries.map((e) => (e.kind === "repo" ? e.target.id : e.group.label)),
    ).toEqual([
      "~/.claude/rules/memax-personal.md",
      "r1",
      "~/.codex/AGENTS.md",
    ]);
    const [claude] = homeGroups(targets);
    expect(claude!.agent).toBe("claude-code");
    expect(claude!.targets.map((t) => t.machine?.name)).toEqual([
      "work-mbp",
      "home-linux",
    ]);
    expect(siblingsOf(targets[0]!, targets).map((t) => t.id)).toEqual([
      "t1",
      "t4",
    ]);
    expect(siblingsOf(repo, targets)).toEqual([repo]);
  });

  it("keeps a file at another path apart: it's another file", () => {
    const override = home(3, desktop, "codex_user", {
      path: ".codex/AGENTS.override.md",
      label: "~/.codex/AGENTS.override.md",
    });
    const groups = homeGroups([home(2, laptop, "codex_user"), override]);
    expect(groups.map((g) => g.label)).toEqual([
      "~/.codex/AGENTS.md",
      "~/.codex/AGENTS.override.md",
    ]);
  });
});

describe("one machine's copy", () => {
  it("says how it stands, offline when its compile waits on a machine that isn't online", () => {
    const state = (over: Partial<TargetView>, m: MachineView = laptop) =>
      machineFileState(home(1, m, "claude_user", over), m, NOW);
    expect(state({})).toEqual({ kind: "in_sync" });
    expect(state({ syncState: "compiling" })).toEqual({ kind: "compiling" });
    expect(state({ syncState: "pending_delivery" })).toEqual({
      kind: "waiting",
    });
    expect(state({ syncState: "pending_delivery" }, desktop)).toEqual({
      kind: "offline",
      since: "2026-10-02T21:10:00-07:00",
    });
    expect(state({ syncState: "drifted", openDrift: 1 })).toEqual({
      kind: "drifted",
      edits: 1,
    });
    expect(state({ syncState: "held", holding: ["M-1", "M-2"] })).toEqual({
      kind: "held",
      proposals: 2,
    });
    expect(state({ syncState: "off" })).toEqual({ kind: "stopped" });
    const gone = machine(3, { linked: false, unlinkedAt: minutesAgo(60) });
    expect(state({ syncState: "off" }, gone)).toEqual({
      kind: "unlinked",
      since: minutesAgo(60),
    });
  });

  it("is in sync on an offline machine when nothing new waits for it", () => {
    expect(
      machineFileState(home(1, desktop, "claude_user"), desktop, NOW),
    ).toEqual({ kind: "in_sync" });
  });

  it("names machines by name, with the label only where two share one", () => {
    const twin = machine(3, { name: "work-mbp" });
    expect(machineName(laptop, MACHINES)).toBe("work-mbp");
    expect(machineName(laptop, [...MACHINES, twin])).toBe(
      "work-mbp (machine-1)",
    );
    expect(machineName(twin, [...MACHINES, twin])).toBe("work-mbp (machine-3)");
    const copies = machineCopies(
      homeGroups([
        home(1, laptop, "claude_user"),
        home(3, twin, "claude_user"),
      ])[0]!,
      [...MACHINES, twin],
      NOW,
    );
    expect(copies.map((c) => c.name)).toEqual([
      "work-mbp (machine-1)",
      "work-mbp (machine-3)",
    ]);
  });
});

describe("a file across its machines", () => {
  const status = (...targets: TargetView[]) =>
    homeFileStatus(homeGroups(targets)[0]!, MACHINES, NOW);

  it("is in sync on every machine, naming the machine when there's one", () => {
    expect(
      status(home(1, laptop, "claude_user"), home(2, desktop, "claude_user")),
    ).toEqual({
      kind: "in_sync",
      machines: 2,
      machine: null,
    });
    expect(status(home(1, laptop, "claude_user"))).toEqual({
      kind: "in_sync",
      machines: 1,
      machine: "work-mbp",
    });
  });

  it("says a hand edit first, then a hold, then a compile", () => {
    expect(
      status(
        home(1, laptop, "codex_user", { syncState: "held", holding: ["M-9"] }),
        home(2, desktop, "codex_user", { syncState: "drifted", openDrift: 1 }),
      ),
    ).toEqual({ kind: "drifted", machines: ["home-linux"] });
    expect(
      status(
        home(1, laptop, "codex_user", { syncState: "held", holding: ["M-9"] }),
        home(2, desktop, "codex_user", { syncState: "compiling" }),
      ),
    ).toEqual({ kind: "held", machines: ["work-mbp"], proposals: 1 });
    expect(
      status(
        home(1, laptop, "codex_user", { syncState: "compiling" }),
        home(2, desktop, "codex_user", { syncState: "pending_delivery" }),
      ),
    ).toEqual({ kind: "compiling" });
  });

  it("is on 1 of 2 machines while one is offline, and names it", () => {
    expect(
      status(
        home(1, laptop, "claude_user"),
        home(2, desktop, "claude_user", { syncState: "pending_delivery" }),
      ),
    ).toEqual({
      kind: "behind",
      reached: 1,
      of: 2,
      offline: { machine: "home-linux", since: "2026-10-02T21:10:00-07:00" },
      waiting: null,
    });
    expect(
      status(
        home(1, laptop, "claude_user", { syncState: "pending_delivery" }),
        home(2, desktop, "claude_user"),
      ),
    ).toEqual({
      kind: "behind",
      reached: 1,
      of: 2,
      offline: null,
      waiting: "work-mbp",
    });
  });

  it("counts only the machines it's still written to", () => {
    expect(
      status(
        home(1, laptop, "agents_md"),
        home(2, desktop, "agents_md", { syncState: "off" }),
      ),
    ).toEqual({ kind: "in_sync", machines: 1, machine: "work-mbp" });
    expect(
      status(
        home(1, laptop, "agents_md", { syncState: "off" }),
        home(2, desktop, "agents_md", { syncState: "off" }),
      ),
    ).toEqual({ kind: "off" });
  });

  it("opens the copy that needs the person, else the first machine's", () => {
    const group = homeGroups([
      home(1, laptop, "codex_user"),
      home(2, desktop, "codex_user", { syncState: "drifted", openDrift: 1 }),
    ])[0]!;
    expect(primaryTarget(group, MACHINES, NOW).id).toBe("t2");
    const calm = homeGroups([
      home(1, laptop, "codex_user", { syncState: "off" }),
      home(2, desktop, "codex_user"),
    ])[0]!;
    expect(primaryTarget(calm, MACHINES, NOW).id).toBe("t2");
  });

  it("reads without the machines list: a waiting copy just waits", () => {
    const group = homeGroups([
      home(1, laptop, "claude_user"),
      home(2, desktop, "claude_user", { syncState: "pending_delivery" }),
    ])[0]!;
    expect(homeFileStatus(group, null, NOW)).toEqual({
      kind: "behind",
      reached: 1,
      of: 2,
      offline: null,
      waiting: "home-linux",
    });
  });
});

describe("the status line and the counts", () => {
  const all = [
    home(1, laptop, "claude_user"),
    home(2, laptop, "codex_user"),
    home(3, laptop, "agents_md"),
    home(4, desktop, "claude_user", { syncState: "pending_delivery" }),
    home(5, desktop, "codex_user", { syncState: "pending_delivery" }),
    home(6, desktop, "agents_md", { syncState: "off" }),
  ];
  const personal = { kind: "personal" as const };

  it("counts machines: in sync on every one, or on some of them", () => {
    expect(homeSyncLine(all.slice(0, 3), MACHINES, NOW)).toEqual({
      kind: "machines-in-sync",
      machines: 1,
    });
    expect(homeSyncLine(all, MACHINES, NOW)).toEqual({
      kind: "machines-behind",
      reached: 1,
      of: 2,
    });
  });

  it("says a hand edit or a hold on a machine before anything else", () => {
    const edited = [...all];
    edited[4] = { ...edited[4]!, syncState: "drifted", openDrift: 1 };
    expect(homeSyncLine(edited, MACHINES, NOW)).toEqual({
      kind: "machine-drifted",
      machine: "home-linux",
    });
    const held = [...all];
    held[1] = { ...held[1]!, syncState: "held", holding: ["M-3"] };
    expect(spaceSyncLine(personal, held, MACHINES, NOW)).toEqual({
      kind: "machine-held",
      machine: "work-mbp",
    });
  });

  it("puts a repository's drift first, and adds its files to home's when both are in sync", () => {
    const drifted = {
      ...repo,
      syncState: "drifted" as const,
      openDrift: 1,
      kind: "cursor_mdc" as const,
    };
    expect(spaceSyncLine(personal, [drifted, ...all], MACHINES, NOW)).toEqual({
      kind: "drifted",
      agent: "cursor",
    });
    expect(
      spaceSyncLine(personal, [repo, ...all.slice(0, 3)], MACHINES, NOW),
    ).toEqual({
      kind: "files-in-sync",
      files: 4,
    });
  });

  it("says a personal space with nothing written anywhere isn't on any machine", () => {
    expect(spaceSyncLine(personal, [], [], NOW)).toEqual({
      kind: "no-machines",
    });
    expect(spaceSyncLine(personal, [], null, NOW)).toEqual({
      kind: "no-machines",
    });
    const gone = MACHINES.map((m) => ({ ...m, linked: false }));
    const stopped = all.map((t) => ({ ...t, syncState: "off" as const }));
    expect(spaceSyncLine(personal, stopped, gone, NOW)).toEqual({
      kind: "no-machines",
    });
    // Linked, with every file stopped: nothing to call in sync.
    expect(spaceSyncLine(personal, stopped, MACHINES, NOW)).toBeNull();
    expect(spaceSyncLine({ kind: "project" }, [], null, NOW)).toBeNull();
  });

  it("counts a home file once, in sync when it is on every machine it's written to", () => {
    expect(targetCounts(all, MACHINES, NOW)).toEqual({ total: 3, inSync: 1 });
    expect(targetCounts([repo, ...all.slice(0, 3)], MACHINES, NOW)).toEqual({
      total: 4,
      inSync: 4,
    });
  });

  it("says how far a memory reaches: files once, and the machines they're on", () => {
    expect(reachOf(all.filter((t) => t.syncState !== "off"))).toEqual({
      files: 3,
      machines: 2,
    });
    expect(reachOf([repo])).toEqual({ files: 1, machines: 0 });
  });

  it("names the agents that read a machine's files", () => {
    expect(machineAgents(laptop, all)).toEqual(["claude-code", "codex"]);
    expect(machineAgents(desktop, all)).toEqual(["claude-code", "codex"]);
    expect(machineAgents(laptop, [{ ...all[1]!, readBy: [] }])).toEqual([
      "codex",
    ]);
  });
});

describe("machines", () => {
  it("is online when its daemon read its files in the last 15 minutes", () => {
    expect(isOnline(laptop, NOW)).toBe(true);
    expect(isOnline(desktop, NOW)).toBe(false);
    expect(isOnline(machine(3, { lastSeenAt: null }), NOW)).toBe(false);
    expect(isOnline(machine(4, { linked: false }), NOW)).toBe(false);
  });

  it("orders linked machines first, each by number, and finds the one linked last", () => {
    const gone = machine(10, { linked: false });
    const later = machine(2, { linkedAt: "2026-10-05T08:00:00Z" });
    expect(orderMachines([gone, later, laptop]).map((m) => m.label)).toEqual([
      "machine-1",
      "machine-2",
      "machine-10",
    ]);
    expect(latestLinked([laptop, later, gone])?.label).toBe("machine-2");
    expect(latestLinked([gone])).toBeNull();
  });

  it("takes a rename of 1 to 80 characters, trimmed", () => {
    expect(validMachineName("  work-mbp ")).toBe("work-mbp");
    expect(validMachineName("   ")).toBeNull();
    expect(validMachineName("x".repeat(81))).toBeNull();
    expect(validMachineName("x".repeat(80))).toHaveLength(80);
  });
});
