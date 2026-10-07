import type { V2 } from "memax-sdk";
import { describe, expect, it, vi } from "vitest";
import { en } from "@/i18n/locales/en";
import { zh } from "@/i18n/locales/zh";
import {
  factNote,
  foldNote,
  ledeText,
  nextText,
  whoText,
  type DreamWords,
} from "../dream-copy";
import { todayFooter } from "../today-copy";
import { createDemoDream } from "./dream-demo";
import {
  cardOf,
  dreamTimeOf,
  editionNumberOf,
  needsYouOf,
  todayDreamOf,
  type DreamEditionView,
} from "./dream";
import { editionViewOf } from "./dream-sdk";
import { createSdkSource } from "./sdk-source";
import { DEMO_SPACES } from "./demo-dataset";

// Dream's data: the /v2 edition mapped for the page, Today's card and
// "no edition last night", the schedule in the viewer's zone, and the
// page's words in both locales.

const v2 = DEMO_SPACES.find((s) => s.slug === "memax-v2")!;
const TZ = "America/Vancouver";

async function demoEdition(): Promise<DreamEditionView> {
  const e = await createDemoDream().edition({ space: v2, ref: "latest" });
  return e!;
}

function words(locale: "en" | "zh"): DreamWords {
  const t = locale === "en" ? en : zh;
  return {
    dc: t.ledger.dream,
    app: t.ledger.app,
    rc: t.ledger.records,
    locale,
    timeZone: TZ,
    agentName: (k) =>
      ({ codex: "Codex", "claude-code": "Claude Code", cursor: "Cursor" })[k] ??
      k,
  };
}

const memory = (over: Partial<V2.Memory>): V2.Memory =>
  ({
    id: "m1",
    ref: "M-0219",
    space_id: "s1",
    tenant_id: "t1",
    statement: "Background jobs run on River, not Temporal.",
    section: "decisions",
    kind: "fact",
    state: "kept",
    lifecycle: "kept",
    flags: [],
    version: 3,
    created_receipt_id: "r1",
    last_receipt_id: "r2",
    created_at: "2026-10-02T17:41:00Z",
    updated_at: "2026-10-05T10:12:00Z",
    ...over,
  }) as V2.Memory;

const receipt = (over: Partial<V2.Receipt>): V2.Receipt =>
  ({
    id: "r1",
    space_id: "s1",
    object_kind: "memory",
    object_ref: "M-0219",
    object_id: "m1",
    action: "kept",
    actor_kind: "person",
    actor_id: "me",
    via: "web",
    occurred_at: "2026-10-02T17:41:00Z",
    ...over,
  }) as V2.Receipt;

const edition: V2.DreamEdition = {
  id: "e1",
  ref: "D-0003",
  n: 3,
  space_id: "s1",
  slot: "2026-10-05T10:00:00Z",
  trigger: "schedule",
  since: "2026-10-04T10:12:00Z",
  until: "2026-10-05T10:00:00Z",
  started_at: "2026-10-05T10:11:20Z",
  finished_at: "2026-10-05T10:12:00Z",
  seconds: 40,
  notes_read: 3,
  notes_by: [{ kind: "agent", agent: "gemini-cli", count: 3 }],
  note_refs: ["N-0001", "N-0002", "N-0003"],
  fact_refs: ["M-0219"],
  counts: {
    fold: 1,
    propose: 0,
    dedupe: 0,
    conflict: 1,
    stale: 0,
    fade: 1,
    brief: 0,
  },
  undone: 0,
  needs_you: 1,
  receipt_id: "r9",
  actions: [
    {
      id: "a1",
      edition_id: "e1",
      edition_ref: "D-0003",
      n: 1,
      kind: "fold",
      memory: memory({}),
      version: 3,
      note_refs: ["N-0001", "N-0002"],
      receipt_ids: ["r2"],
      undoable: true,
      created_at: "2026-10-05T10:12:00Z",
    },
    {
      id: "a2",
      edition_id: "e1",
      edition_ref: "D-0003",
      n: 2,
      kind: "conflict",
      memory: memory({
        id: "m2",
        ref: "M-0301",
        statement: "Deploys go to Railway.",
        state: "conflict",
        flags: ["conflict"],
        created_receipt_id: "r3",
        last_receipt_id: "r4",
      }),
      related: memory({
        id: "m3",
        ref: "M-0174",
        statement: "Deploys go to Fly.io.",
        created_receipt_id: "r5",
        last_receipt_id: "r5",
      }),
      note_refs: [],
      receipt_ids: ["r4"],
      undoable: true,
      created_at: "2026-10-05T10:12:00Z",
    },
    {
      id: "a3",
      edition_id: "e1",
      edition_ref: "D-0003",
      n: 3,
      kind: "fade",
      memory: memory({
        id: "m4",
        ref: "M-0044",
        statement: "Use tabs in generated Go files.",
        state: "faded",
        lifecycle: "faded",
        created_receipt_id: "r6",
        last_receipt_id: "r7",
      }),
      note_refs: [],
      receipt_ids: ["r7"],
      undone: { receipt_id: "r8", at: "2026-10-05T15:00:00Z" },
      undoable: false,
      created_at: "2026-10-05T10:12:00Z",
    },
  ],
  surfaced: [],
};

const receipts = new Map<string, V2.Receipt>(
  [
    receipt({ id: "r1" }),
    receipt({
      id: "r2",
      action: "folded",
      actor_kind: "dream",
      actor_id: undefined,
    }),
    receipt({
      id: "r3",
      action: "proposed",
      actor_kind: "agent",
      agent: "codex",
      actor_id: "c1",
    }),
    receipt({
      id: "r4",
      action: "flagged",
      actor_kind: "dream",
      actor_id: undefined,
      source: { kind: "dream", ref: "D-0003" },
    }),
    receipt({ id: "r5", action: "kept", actor_id: "someone" }),
    receipt({ id: "r6" }),
    receipt({
      id: "r7",
      action: "faded",
      actor_kind: "dream",
      actor_id: undefined,
    }),
  ].map((r) => [r.id, r]),
);

describe("an edition over /v2", () => {
  it("maps actions, rails and who kept the other side", () => {
    const view = editionViewOf(edition, receipts, "me");
    expect(view).toMatchObject({ ref: "D-0003", n: 3, notes: 3, seconds: 40 });
    expect(view.notesBy).toEqual([
      { kind: "agent", agent: "gemini", count: 3 },
    ]);
    const [fold, conflict, fade] = view.actions;
    // Dream's folded receipt isn't a rail verb: the rail keeps the keep.
    expect(fold!.memory!.receipt).toMatchObject({
      action: "kept",
      by: { kind: "person", self: true },
    });
    expect(fold!.notes).toEqual(["N-0001", "N-0002"]);
    expect(conflict!.related).toEqual({
      ref: "M-0174",
      keptBy: { kind: "person", self: false },
    });
    expect(fade!.undone).toEqual({ at: "2026-10-05T15:00:00Z" });
    expect(fade!.undoable).toBe(false);
    expect(needsYouOf(view).map((m) => m.ref)).toEqual(["M-0301"]);
  });

  it("makes Today's card: the conflict first, then the folds, and an undone fade dropped", () => {
    const card = cardOf(editionViewOf(edition, receipts, "me"));
    expect(card).toMatchObject({ n: 3, ref: "D-0003", notes: 3, facts: 1 });
    expect(card.lines.map((l) => l.kind)).toEqual(["conflict", "merged"]);
    expect(card.lines[1]).toMatchObject({
      text: "Background jobs run on River, not Temporal.",
      meta: { kind: "folded", notes: 2, into: "M-0219" },
    });
  });

  it("sums up what faded on the card, for the catalogue to word", async () => {
    const card = cardOf(await demoEdition());
    expect(card.lines).toContainEqual({
      kind: "faded",
      text: null,
      count: 11,
      meta: { kind: "restorable" },
    });
    expect(card.lines.length).toBeLessThanOrEqual(3);
  });
});

describe("Today's Dream", () => {
  const view = editionViewOf(edition, receipts, "me");
  const schedule = {
    cadence: "nightly" as const,
    timeZone: TZ,
    nextAt: "2026-10-06T10:00:00Z",
  };

  it("shows last night's edition, says a quiet night, or no edition yet", () => {
    const now = new Date("2026-10-05T21:40:00Z");
    expect(todayDreamOf(view, schedule, now).kind).toBe("edition");
    const older = { ...view, finishedAt: "2026-10-03T10:12:00Z" };
    expect(todayDreamOf(older, schedule, now).kind).toBe("quiet");
    expect(todayDreamOf(null, schedule, now).kind).toBe("unavailable");
    // No schedule yet: last night means the last day and a half.
    expect(todayDreamOf(view, null, now).kind).toBe("edition");
  });

  it("says when Dream runs in the viewer's night, and the day when weekly", () => {
    expect(dreamTimeOf(schedule, TZ)).toEqual({ at: "03:00", weekday: null });
    // British Columbia stays on UTC−7 from November 2026.
    const winter = { ...schedule, nextAt: "2026-11-20T10:00:00Z" };
    expect(dreamTimeOf(winter, TZ).at).toBe("03:00");
    expect(dreamTimeOf(winter, "America/Los_Angeles").at).toBe("02:00");
    const weekly = { ...schedule, cadence: "weekly" as const };
    expect(dreamTimeOf(weekly, TZ)).toEqual({ at: "03:00", weekday: 2 });
    expect(dreamTimeOf(null, TZ)).toEqual({ at: null, weekday: null });
    const t = en.ledger.today;
    expect(
      todayFooter(
        t,
        {
          compiledAt: null,
          dreamAt: "03:00",
          dreamWeekday: 2,
          kept: null,
          space: "x",
        },
        { now: new Date(), timeZone: TZ, locale: "en" },
      ),
    ).toBe("Dream runs weekly, on Tuesday at 03:00");
  });

  it("reads the latest edition through memax.v2.dream for Today", async () => {
    const client = {
      v2: {
        review: { list: vi.fn().mockResolvedValue({ items: [], total: 0 }) },
        gates: { list: vi.fn().mockResolvedValue({ items: [] }) },
        agents: { list: vi.fn().mockResolvedValue({ items: [] }) },
        receipts: {
          list: vi.fn().mockResolvedValue({
            items: [...receipts.values()],
            has_more: false,
          }),
        },
        reads: {
          list: vi.fn().mockResolvedValue({ items: [], has_more: false }),
        },
        dream: {
          editions: vi.fn().mockResolvedValue({
            items: [edition],
            has_more: false,
            schedule: {
              cadence: "nightly",
              time_zone: TZ,
              next_at: "2026-10-06T10:00:00Z",
            },
          }),
          edition: vi.fn().mockResolvedValue(edition),
        },
      },
    };
    const source = createSdkSource({
      client: client as never,
      viewer: { id: "me", initials: "ZZ", name: "Ziyang", timeZone: TZ },
    });
    const today = await source.today.get({ space: { ...v2, onV2: true } });
    expect(client.v2.dream.edition).toHaveBeenCalledWith(
      "memax-v2",
      "D-0003",
      expect.anything(),
    );
    expect(today.dreamAt).toBe("03:00");
    expect(today.dream.kind).toMatch(/edition|quiet/);
  });
});

describe("an edition's words", () => {
  it("says who wrote the notes and the window, in en and zh", async () => {
    const e = await demoEdition();
    expect(whoText(words("en"), e.notesBy)).toBe("three agents and two chats");
    expect(ledeText(words("en"), e)).toBe(
      "Dream read 34 notes from three agents and two chats between 18:00 and 03:00. Everything it changed is listed here, and every change can be undone.",
    );
    expect(ledeText(words("zh"), e)).toBe(
      "Dream 读了 18:00 到 03:00 之间来自3个 Agent和 2段对话的 34 条笔记。它改动的一切都列在这里，每一处都可以撤销。",
    );
    expect(
      ledeText(words("en"), { ...e, since: "2026-10-01T10:00:00Z" }),
    ).toContain("since Oct 1");
    expect(ledeText(words("en"), { ...e, notes: 0 })).toMatch(/no new notes/);
  });

  it("names the notes a fold read, as a range when they run together", async () => {
    const e = await demoEdition();
    const [river, mcp] = e.actions;
    expect(foldNote(words("en"), river!)).toBe(
      "Dream folded 9 notes into it: N-1180 to N-1188",
    );
    expect(foldNote(words("en"), mcp!)).toBe("Dream folded 4 notes into it");
    expect(foldNote(words("zh"), river!)).toBe(
      "Dream 把 9 条笔记归入了它：N-1180 到 N-1188",
    );
  });

  it("says where a new fact came from and where it stands", async () => {
    const e = await demoEdition();
    const facts = e.actions.filter((a) => a.kind === "propose");
    expect(facts.map((a) => factNote(words("en"), a))).toEqual([
      "From 5 notes by Codex and Cursor · waiting in Review",
      "From 3 notes by Claude Code · waiting in Review",
      "From 4 notes in two chats · waiting in Review",
      "From 6 notes by Claude Code, which may write here",
    ]);
    // /v2 counts authors per edition, not per action.
    expect(factNote(words("en"), { ...facts[0]!, noteAuthors: null })).toBe(
      "From 5 notes · waiting in Review",
    );
  });

  it("says when the next edition comes, in the viewer's zone", () => {
    expect(nextText(words("en"), "2026-10-06T10:00:00Z")).toBe(
      "Next edition Tuesday at 03:00.",
    );
    expect(nextText(words("zh"), "2026-10-06T10:00:00Z")).toBe(
      "下一期在星期二 03:00。",
    );
  });

  it("reads an edition's number from its ID or its number", () => {
    expect(editionNumberOf("D-0214")).toBe(214);
    expect(editionNumberOf("214")).toBe(214);
    expect(editionNumberOf("latest")).toBeNull();
  });
});
