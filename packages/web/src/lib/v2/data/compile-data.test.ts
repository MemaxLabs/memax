import { describe, expect, it } from "vitest";
import type { V2 } from "memax-sdk";
import { blocksOf, factCount, numberSources } from "./brief";
import {
  buildBriefView,
  proseAuthorsOf,
  type BriefInputMemory,
} from "./brief-view";
import { briefMemoryOf, structureOf, versionOf } from "./brief-sdk";
import { createDemoSource } from "./demo-source";
import { DEMO_SPACES } from "./demo-dataset";
import { DEMO_TARGETS } from "./demo-targets-data";
import {
  reachesTarget,
  syncLineOf,
  targetSlug,
  targetStatus,
  type TargetView,
} from "./targets";
import { compileOf, driftItemOf, targetOf } from "./targets-sdk";
import { pickWaiting, startOfDay } from "./today";
import { agentsTodayOf } from "./today-sdk";
import type { ReviewItem } from "./review";

const v2 = DEMO_SPACES.find((s) => s.slug === "memax-v2")!;
const demoTargets = DEMO_TARGETS["memax-v2"]!;
const ZZ_ID = "0192a7c0-0000-7000-8000-0000000000a1";

const memory = (
  ref: string,
  fields: Partial<BriefInputMemory> = {},
): BriefInputMemory => ({
  ref,
  text: `${ref} words.`,
  section: "decisions",
  state: "kept",
  lifecycle: "kept",
  version: 1,
  receipt: null,
  source: null,
  scope: [],
  ...fields,
});

describe("the Brief as the page shows it", () => {
  const view = buildBriefView({
    id: "b",
    ref: "B-0002",
    version: 2,
    by: { kind: "dream" },
    at: "2026-10-05T10:12:00Z",
    reason: null,
    structure: {
      title: "T",
      summary: null,
      sections: [
        {
          key: "decisions",
          heading: "Decisions",
          items: [
            { ref: "M-1" },
            { ref: "M-gone" },
            { text: "Line one.", cites: ["M-1"] },
            { text: "Line two.", cites: ["M-1"] },
          ],
        },
        {
          key: "open",
          heading: "Open",
          items: [{ text: "Undecided.", cites: ["M-9", "M-8"] }],
        },
      ],
    },
    memories: [
      memory("M-1"),
      // Kept after the version: at the end of its own section.
      memory("M-2"),
      memory("M-3", { section: "preferences" }),
      memory("M-gone", { lifecycle: "other", state: "forgotten" }),
      memory("M-8", { state: "conflict" }),
      memory("M-9", {
        state: "conflict",
        lifecycle: "proposed",
        receipt: {
          by: { kind: "agent", agent: "codex" },
          action: "proposed",
          at: "x",
        },
      }),
      memory("M-5", { lifecycle: "proposed", state: "proposed" }),
    ],
  });

  it("places facts as the compiler does, and adds what's waiting", () => {
    expect(view.sections.map((s) => [s.key, s.rows.map((r) => r.key)])).toEqual(
      [
        ["decisions", ["M-1", "prose-0", "prose-1", "M-2", "M-5"]],
        ["open", ["prose-2"]],
        ["preferences", ["M-3"]],
      ],
    );
    const decisions = view.sections[0]!.rows;
    expect(decisions.find((r) => r.key === "M-2")).toMatchObject({
      placed: false,
    });
    expect(decisions.find((r) => r.key === "M-5")).toMatchObject({
      kind: "waiting",
      state: "proposed",
    });
    expect(view.sections[2]!.heading).toBe("Preferences");
  });

  it("sets prose that rests on a conflict as waiting, with the proposal's receipt", () => {
    const open = view.sections[1]!.rows[0]!;
    expect(open).toMatchObject({
      kind: "prose",
      state: "conflict",
      cites: ["M-9", "M-8"],
    });
    expect(open.receipt?.by).toEqual({ kind: "agent", agent: "codex" });
  });

  it("reads consecutive kept prose as one paragraph", () => {
    expect(blocksOf(view.sections[0]!.rows).map((b) => b.kind)).toEqual([
      "row",
      "paragraph",
      "row",
      "row",
    ]);
  });

  it("numbers kept sources in reading order, and counts facts", () => {
    expect([...numberSources(view.sections, view.memories)]).toEqual([
      ["M-1", 1],
      ["M-2", 2],
      ["M-3", 3],
    ]);
    // M-1, M-2, M-9, M-8, M-3; the waiting M-5 isn't one yet.
    expect(factCount(view)).toBe(5);
  });

  it("finds who wrote each line of prose: the oldest version that has it", () => {
    const line = (text: string) => ({
      title: "T",
      summary: null,
      sections: [{ key: "d", heading: "D", items: [{ text, cites: ["M-1"] }] }],
    });
    const authors = proseAuthorsOf(line("Same."), [
      { structure: line("Same."), by: { kind: "dream" }, at: "3" },
      { structure: line("Same."), by: { kind: "person", self: true }, at: "2" },
      {
        structure: line("Other."),
        by: { kind: "person", self: false },
        at: "1",
      },
    ]);
    expect(authors.get("prose-0")).toEqual({
      by: { kind: "person", self: true },
      action: "wrote",
      at: "2",
    });
  });

  it("matches the demo's Brief.png: the stale fact, the waiting proposal, the conflict", () => {
    const brief = createDemoSource().brief.peek?.(v2.slug);
    const rows = brief!.sections.flatMap((s) => s.rows);
    expect(rows.find((r) => r.ref === "M-0187")?.state).toBe("stale");
    expect(rows.find((r) => r.ref === "M-0430")?.kind).toBe("waiting");
    expect(rows.find((r) => r.key === "prose-2")?.state).toBe("conflict");
    expect(rows.find((r) => r.ref === "M-0219")?.changed).toBe(
      "We do not use Temporal.",
    );
  });
});

const run = (over: Partial<V2.CompileRun> = {}): V2.CompileRun => ({
  id: "r1",
  ref: "C-0881",
  target_id: "t1",
  space_id: "s1",
  brief: "B-0043",
  brief_version: 6,
  generation: 3,
  status: "delivered",
  input_sha256: "a".repeat(64),
  bytes: 1263,
  lines: 27,
  refs: ["M-0219"],
  dropped_for_budget: ["M-0300"],
  files: [
    {
      path: "AGENTS.md",
      sha256: "b".repeat(64),
      drift_sha256: "b".repeat(64),
      bytes: 1263,
      lines: 27,
      refs: ["M-0219"],
      cites: ["M-0219", "M-0431"],
      dropped_for_budget: [],
    },
  ],
  warnings: [],
  enqueued_at: "2026-10-05T21:30:58Z",
  started_at: "2026-10-05T21:31:00Z",
  compiled_at: "2026-10-05T21:31:01Z",
  receipt_id: "rc1",
  ...over,
});

const target = (over: Partial<V2.Target>): V2.Target => ({
  id: "t1",
  space_id: "s1",
  tenant_id: "n1",
  kind: "agents_md",
  path: "AGENTS.md",
  label: "AGENTS.md",
  settings: { include: "kept_and_open", stale: "mark", size_budget: 25600 },
  delivery: "local",
  sync_state: "in_sync",
  version: 4,
  dirty_gen: 3,
  compiled_gen: 3,
  dirty_at: "2026-10-05T21:30:58Z",
  open_drift: 0,
  created_receipt_id: "rc0",
  last_receipt_id: "rc1",
  created_at: "2026-09-28T00:00:00Z",
  updated_at: "2026-10-05T21:31:01Z",
  last_compile: run(),
  ...over,
});

describe("targets from /v2", () => {
  it("maps a target and its latest run", () => {
    const all = [
      target({}),
      target({
        id: "t2",
        kind: "cursor_mdc",
        path: ".cursor/rules",
        label: ".cursor/rules",
        last_compile: run({ files: [] }),
      }),
    ];
    const agents = targetOf(all[0]!, all);
    expect(agents).toMatchObject({
      slug: "agents-md",
      reads: null,
      settings: { include: "kept_and_open", stale: "mark", sizeBudget: 25600 },
      version: 4,
    });
    expect(compileOf(all[0]!.last_compile!)).toEqual({
      ref: "C-0881",
      status: "delivered",
      at: "2026-10-05T21:31:01Z",
      bytes: 1263,
      lines: 27,
      refs: ["M-0219"],
      cites: ["M-0219", "M-0431"],
      dropped: ["M-0300"],
      files: ["AGENTS.md"],
    });
    // D2: Cursor with nothing scoped writes no file and reads AGENTS.md.
    const cursor = targetOf(all[1]!, all);
    expect(cursor.reads).toBe("AGENTS.md");
    expect(targetStatus(cursor)).toEqual({ kind: "reads", file: "AGENTS.md" });
  });

  it("says D3: ChatGPT is live over its connector, never in sync", () => {
    const chatgpt = targetOf(
      target({
        kind: "chatgpt",
        path: undefined,
        label: "ChatGPT project",
        delivery: "copy",
      }),
      [],
    );
    expect(targetStatus(chatgpt)).toEqual({ kind: "live" });
    expect(targetStatus({ ...chatgpt, syncState: "off" })).toEqual({
      kind: "off",
    });
  });

  it("words the rest of the states", () => {
    const base = demoTargets[0]!;
    expect(targetStatus({ ...base, syncState: "pending_delivery" })).toEqual({
      kind: "pending",
    });
    expect(
      targetStatus({ ...base, syncState: "drifted", openDrift: 2 }),
    ).toEqual({
      kind: "drifted",
      edits: 2,
    });
    expect(
      targetStatus({
        ...base,
        syncState: "held",
        holding: ["M-0450", "M-0451"],
      }),
    ).toEqual({ kind: "held", proposals: 2 });
    expect(
      targetSlug({ id: "x", kind: "claude_md" }, [
        { kind: "claude_md" },
        { kind: "claude_md" },
      ]),
    ).toBe("x");
  });

  it("feeds the rail: a drifted file first, then a held one, compiling, waiting, in sync", () => {
    expect(syncLineOf(demoTargets)).toEqual({
      kind: "drifted",
      agent: "cursor",
    });
    const calm = demoTargets.map(
      (t): TargetView => ({ ...t, syncState: "in_sync", openDrift: 0 }),
    );
    expect(syncLineOf(calm)).toEqual({ kind: "files-in-sync", files: 3 });
    expect(
      syncLineOf(
        calm.map((t, i) => (i === 0 ? { ...t, syncState: "compiling" } : t)),
      ),
    ).toEqual({ kind: "compiling" });
    expect(
      syncLineOf(
        calm.map((t, i) =>
          i === 1 ? { ...t, syncState: "pending_delivery" } : t,
        ),
      ),
    ).toEqual({ kind: "waiting-delivery", files: 1 });
    // A pull holds its file: the proposals it waits on, counted once.
    const held = calm.map((t, i) =>
      i === 0
        ? { ...t, syncState: "held" as const, holding: ["M-0450", "M-0451"] }
        : i === 1
          ? { ...t, syncState: "held" as const, holding: ["M-0451"] }
          : t,
    );
    expect(syncLineOf(held)).toEqual({ kind: "held", proposals: 2 });
    expect(
      syncLineOf(
        held.map((t, i) => (i === 2 ? { ...t, syncState: "compiling" } : t)),
      ),
    ).toEqual({ kind: "held", proposals: 2 });
    expect(
      syncLineOf([
        { ...calm[0]!, syncState: "drifted", openDrift: 1 },
        held[1]!,
      ]),
    ).toEqual({ kind: "drifted", file: "AGENTS.md" });
    expect(
      syncLineOf([{ ...calm[0]!, syncState: "drifted", openDrift: 1 }]),
    ).toEqual({ kind: "drifted", file: "AGENTS.md" });
    expect(syncLineOf([])).toBeNull();
  });

  it("knows which files a memory reaches, through the shims that import AGENTS.md", () => {
    const reaches = (ref: string) =>
      demoTargets
        .filter((t) => reachesTarget(ref, t, demoTargets))
        .map((t) => t.kind);
    expect(reaches("M-0219")).toEqual([
      "agents_md",
      "claude_md",
      "cursor_mdc",
      "chatgpt",
    ]);
    expect(reaches("M-0430")).toEqual([]);
  });

  it("maps a hand edit, change by change", () => {
    const item = driftItemOf({
      compiled: "a\n",
      observed: "b\n",
      observation: {
        id: "o1",
        target_id: "t1",
        space_id: "s1",
        path: ".cursor/rules/memax-packages-web.mdc",
        observed_sha256: "c".repeat(64),
        observer_kind: "device",
        observer_id: "d1",
        commit: "a41e9c2",
        bytes: 2,
        status: "open",
        observed_at: "2026-10-04T23:40:00Z",
        receipt_id: "rc2",
        changeset: {
          changes: [
            {
              kind: "edit",
              ref: "M-0441",
              refs: ["M-0441"],
              old_text: "a",
              new_text: "b",
              old_line: 9,
              new_line: 9,
            },
            {
              kind: "new",
              text: "c",
              line: 11,
              section: "Conventions",
              paths: [],
              cites: [],
            },
            { kind: "remove", refs: ["M-0442"], old_text: "d", old_line: 10 },
          ],
          drift: {
            changed: true,
            header_edited: false,
            frontmatter_edited: false,
            layout_edited: false,
            managed_block: null,
            hidden_characters: 1,
          },
        },
      },
    });
    expect(item.changes).toEqual([
      {
        kind: "edit",
        ref: "M-0441",
        oldText: "a",
        newText: "b",
        oldLine: 9,
        newLine: 9,
      },
      { kind: "new", text: "c", line: 11, section: "Conventions" },
      { kind: "remove", ref: "M-0442", oldText: "d", oldLine: 10 },
    ]);
    expect(item).toMatchObject({
      commit: "a41e9c2",
      hiddenCharacters: 1,
      compiledAt: null,
    });
  });
});

describe("the Brief from /v2", () => {
  const brief: V2.Brief = {
    id: "b1",
    version_id: "v6",
    ref: "B-0043",
    version: 6,
    parent_version: 5,
    space_id: "s1",
    tenant_id: "n1",
    title: "Memax V2 engineering brief",
    sections: [
      {
        key: "decisions",
        heading: "Decisions",
        items: [{ ref: "M-0219" }, { text: "Prose.", cites: ["M-0219"] }],
      },
    ],
    facts: 1,
    current: true,
    receipt_id: "rc1",
    receipt: {
      id: "rc1",
      seq: 1,
      tenant_id: "n1",
      space_id: "s1",
      object_kind: "brief",
      object_id: "b1",
      object_ref: "B-0043",
      action: "revised",
      actor_kind: "person",
      actor_id: ZZ_ID,
      via: "web",
      reason: "Tidy",
      occurred_at: "2026-10-05T10:12:00Z",
      recorded_at: "2026-10-05T10:12:00Z",
      stream_id: "b1",
      stream_version: 6,
    },
    created_at: "2026-10-05T10:12:00Z",
  };

  it("reads a version's structure and who wrote it", () => {
    expect(structureOf(brief)).toEqual({
      title: "Memax V2 engineering brief",
      summary: null,
      sections: [
        {
          key: "decisions",
          heading: "Decisions",
          items: [{ ref: "M-0219" }, { text: "Prose.", cites: ["M-0219"] }],
        },
      ],
    });
    expect(versionOf(brief, ZZ_ID)).toMatchObject({
      ref: "B-0043",
      parent: 5,
      current: true,
      by: { kind: "person", self: true },
      reason: "Tidy",
    });
  });

  it("reads a memory's margin and scope", () => {
    const m = briefMemoryOf(
      {
        id: "m1",
        ref: "M-0441",
        space_id: "s1",
        tenant_id: "n1",
        statement: "Run tests with `pnpm test`.",
        section: "conventions",
        kind: "fact",
        state: "stale",
        lifecycle: "kept",
        flags: ["stale"],
        trust: "person",
        version: 3,
        conditions: [],
        scope: { paths: ["packages/web/**"] },
        created_receipt_id: "c1",
        last_receipt_id: "c1",
        created_at: "x",
        updated_at: "x",
      },
      new Map([
        [
          "c1",
          {
            id: "c1",
            seq: 2,
            tenant_id: "n1",
            space_id: "s1",
            object_kind: "memory",
            object_id: "m1",
            object_ref: "M-0441",
            action: "kept",
            actor_kind: "person",
            actor_id: ZZ_ID,
            via: "web",
            source: { kind: "pr", ref: "PR #212" },
            occurred_at: "2026-10-04T16:05:00Z",
            recorded_at: "2026-10-04T16:05:00Z",
            stream_id: "m1",
            stream_version: 1,
          },
        ],
      ]),
      ZZ_ID,
    );
    expect(m).toMatchObject({
      ref: "M-0441",
      state: "stale",
      lifecycle: "kept",
      version: 3,
      source: "PR #212",
      scope: ["packages/web/**"],
      receipt: { by: { kind: "person", self: true }, action: "kept" },
    });
  });
});

describe("Today", () => {
  const item = (ref: string, state: ReviewItem["state"]): ReviewItem => ({
    ref,
    version: 1,
    statement: ref,
    section: "decisions",
    state,
    lifecycle: state === "stale" ? "kept" : "proposed",
    external: false,
    by: null,
    action: "proposed",
    at: "x",
    session: null,
    updates: null,
    conflictsWith: null,
    judge: null,
    intoSpace: null,
  });

  it("shows one of each kind first, then the rest in Review's order", () => {
    const queue = [
      item("M-1", "proposed"),
      item("M-2", "proposed"),
      item("M-3", "conflict"),
      item("M-4", "stale"),
    ];
    expect(pickWaiting(queue).map((i) => i.ref)).toEqual(["M-3", "M-1", "M-4"]);
    expect(pickWaiting(queue.slice(0, 2)).map((i) => i.ref)).toEqual([
      "M-1",
      "M-2",
    ]);
  });

  it("starts the day at midnight where the viewer is", () => {
    expect(
      startOfDay(
        new Date("2026-10-05T21:40:00Z"),
        "America/Vancouver",
      ).toISOString(),
    ).toBe("2026-10-05T07:00:00.000Z");
  });

  it("counts each agent's writes today from the receipts", () => {
    const receipt = (
      actor: string,
      action: V2.ReceiptAction,
      at: string,
    ): V2.Receipt => ({
      id: `${actor}-${at}`,
      seq: 1,
      tenant_id: "n1",
      space_id: "s1",
      object_kind: "memory",
      object_id: "m",
      object_ref: "M-1",
      action,
      actor_kind: "agent",
      actor_id: actor,
      agent: "codex",
      via: "mcp",
      occurred_at: at,
      recorded_at: at,
      stream_id: "m",
      stream_version: 1,
    });
    const day = new Date("2026-10-05T07:00:00Z");
    const source = createDemoSource();
    const connections = source.agentsPeek!.spaceAgents("memax-v2")!;
    const codex = connections.find((c) => c.agent === "codex")!;
    const rows = agentsTodayOf(
      connections,
      [
        receipt(codex.id, "proposed", "2026-10-05T20:00:00Z"),
        receipt(codex.id, "proposed", "2026-10-05T21:00:00Z"),
        receipt(codex.id, "kept", "2026-10-04T21:00:00Z"),
      ],
      day,
    );
    expect(rows.find((r) => r.id === codex.id)).toMatchObject({
      reads: null,
      proposed: 2,
      kept: 0,
    });
  });
});
