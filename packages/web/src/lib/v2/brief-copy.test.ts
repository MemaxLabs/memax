import { describe, expect, it } from "vitest";
import { en } from "@/i18n/locales/en";
import { zh } from "@/i18n/locales/zh";
import {
  briefEyebrow,
  commandReason,
  driftLines,
  driftNotice,
  driftTitle,
  driftWhen,
  formatKb,
  linesWord,
  marginLine,
  parseKb,
  targetMeta,
} from "./brief-copy";
import type { BriefRow } from "./data/brief";
import { DEMO_DRIFT, DEMO_TARGETS } from "./data/demo-targets-data";
import { createDemoSource } from "./data/demo-source";
import { DEMO_OVERVIEWS } from "./data/demo-dataset";
import {
  agentDay,
  todayFooter,
  todayHeadline,
  todayLedeOf,
} from "./today-copy";

const now = new Date("2026-10-05T14:40:00-07:00");
const when = { now, timeZone: "America/Vancouver", locale: "en" as const };
const whenZh = { ...when, locale: "zh" as const };
const targets = DEMO_TARGETS["memax-v2"]!;
const cursor = targets.find((t) => t.kind === "cursor_mdc")!;
const agentName = (key: string) =>
  ({ cursor: "Cursor", codex: "Codex", "claude-code": "Claude Code" })[key] ??
  key;
const name = () => "Jiahao";

const row = (over: Partial<BriefRow>): BriefRow => ({
  key: "M-0219",
  kind: "memory",
  ref: "M-0219",
  text: "",
  cites: [],
  state: "kept",
  placed: true,
  version: 1,
  receipt: null,
  source: null,
  scope: [],
  changed: null,
  ...over,
});

describe("the Brief's words", () => {
  it("says who rewrote or revised it, in the eyebrow", () => {
    const at = "2026-10-05T03:12:00-07:00";
    expect(
      briefEyebrow(en.ledger.app, en.ledger.brief, {
        space: "memax-v2",
        by: { kind: "dream" },
        at,
        facts: 14,
        name,
        when,
      }),
    ).toBe("Brief · memax-v2 · rewritten by Dream at 03:12 · 14 facts");
    expect(
      briefEyebrow(en.ledger.app, en.ledger.brief, {
        space: "memax-v2",
        by: { kind: "person", self: true },
        at: "2026-10-04T16:02:00-07:00",
        facts: 1,
        name,
        when,
      }),
    ).toBe("Brief · memax-v2 · revised by you on Oct 4 · 1 fact");
    expect(
      briefEyebrow(zh.ledger.app, zh.ledger.brief, {
        space: "memax-v2",
        by: { kind: "dream" },
        at,
        facts: 14,
        name,
        when: whenZh,
      }),
    ).toBe("简报 · memax-v2 · Dream 于 03:12 重写 · 14 条事实");
  });

  it("writes each fact's margin: source, source changed, in Review, ≠", () => {
    const b = en.ledger.brief;
    expect(marginLine(b, row({ source: "PR #212" }))).toEqual({
      ids: "M-0219",
      rest: "PR #212",
      conflict: false,
    });
    expect(marginLine(b, row({ state: "stale" })).rest).toBe("source changed");
    expect(
      marginLine(b, row({ kind: "waiting", state: "proposed" })).rest,
    ).toBe("in Review");
    expect(
      marginLine(
        b,
        row({
          kind: "prose",
          ref: null,
          state: "conflict",
          cites: ["M-0431", "M-0174"],
        }),
      ),
    ).toEqual({ ids: "M-0431 ≠ M-0174", rest: null, conflict: true });
    expect(
      marginLine(
        b,
        row({ kind: "prose", ref: null, cites: ["M-0001", "M-0012"] }),
      ).ids,
    ).toBe("M-0001, M-0012");
  });

  it("words the drift notice as Brief.png does", () => {
    const item = DEMO_DRIFT[cursor.id]![0];
    expect(driftNotice(en.ledger.brief, cursor, item, agentName, when)).toEqual(
      {
        title: "Cursor's copy has one local edit.",
        body: "Someone changed the rules file by hand on Oct 4. Memax never overwrites a hand edit without asking.",
      },
    );
    const agents = {
      ...targets[0]!,
      syncState: "drifted" as const,
      openDrift: 2,
    };
    expect(
      driftNotice(en.ledger.brief, agents, undefined, agentName, when).title,
    ).toBe("AGENTS.md has 2 local edits.");
    expect(
      driftNotice(zh.ledger.brief, cursor, item, agentName, whenZh).body,
    ).toBe(
      "有人在 10月4日手动改了规则文件。没问过你，Memax 不会覆盖手动改动。",
    );
  });

  it("titles and dates a hand edit, and marks its lines", () => {
    const item = DEMO_DRIFT[cursor.id]![0]!;
    expect(driftTitle(en.ledger.brief, cursor, agentName)).toBe(
      "Cursor's rules file has a hand edit",
    );
    expect(driftWhen(en.ledger.brief, item.observedAt, when)).toBe(
      "on Oct 4 at 16:40",
    );
    const lines = driftLines(item.changes);
    expect([...lines.removed]).toEqual([9]);
    expect([...lines.added]).toEqual([9, 11]);
    const words = en.ledger.brief.resolve.pullLines;
    expect([1, 2, 3].map((n) => linesWord(words, n))).toEqual([
      "The line",
      "Both lines",
      "All 3 lines",
    ]);
  });

  it("measures sizes as the board does, and reads a typed budget", () => {
    expect(formatKb(3072, "en")).toBe("3.0 KB");
    expect(formatKb(1263, "en")).toBe("1.2 KB");
    expect(formatKb(25600, "en", "budget")).toBe("25 KB");
    expect(formatKb(20480, "en", "budget")).toBe("20 KB");
    expect(parseKb("25 KB")).toBe(25600);
    expect(parseKb("20")).toBe(20480);
    expect(parseKb("1,5kb")).toBe(1536);
    expect(parseKb("lots")).toBeNull();
  });

  it("writes a compiled file's meta line", () => {
    const agents = targets[0]!;
    expect(
      targetMeta(
        en.ledger.brief,
        {
          target: agents,
          compiledAt: agents.lastCompile!.at,
          bytes: 1263,
          lines: 27,
          facts: 9,
          open: 1,
          dropped: 0,
        },
        when,
      ),
    ).toEqual([
      "Compiled at 14:31",
      "1.2 KB of a 25 KB budget · 27 lines",
      "9 facts · 1 open question",
      "Written locally by the memax CLI",
    ]);
    const claude = targets[1]!;
    expect(
      targetMeta(
        en.ledger.brief,
        {
          target: claude,
          compiledAt: "2026-10-04T09:10:00-07:00",
          bytes: 166,
          lines: 2,
          facts: 0,
          open: 0,
          dropped: 3,
        },
        when,
      ),
    ).toEqual([
      "Compiled Oct 4 at 09:10",
      "0.2 KB of a 25 KB budget · 2 lines",
      "Imports AGENTS.md",
      "3 facts stay live over MCP",
      "Written locally by the memax CLI",
    ]);
  });

  it("says why a command didn't go through", () => {
    const rc = en.ledger.records;
    const b = en.ledger.brief;
    expect(commandReason(rc, b, { kind: "unreachable" }, "memax-v2")).toBe(
      "It didn't reach Memax, so nothing changed. Try again.",
    );
    expect(
      commandReason(
        rc,
        b,
        { kind: "refused", code: "viewer", message: null },
        "memax-v2",
      ),
    ).toBe("Viewers can propose and comment. A member keeps.");
    expect(commandReason(rc, b, { kind: "decided" }, "memax-v2")).toBe(
      b.toast.alreadyDone,
    );
  });
});

describe("Today's words", () => {
  const source = createDemoSource();
  const data = source.today.peek!("memax-v2")!;
  const overview = DEMO_OVERVIEWS["memax-v2"]!;

  it("builds the lede from real counts", () => {
    expect(todayLedeOf(en.ledger.app, "en", overview, data, agentName)).toBe(
      "Overnight, Dream folded 34 notes into 6 facts. Four proposals, one stale fact and a question from Codex are waiting on you.",
    );
    const noDream = { ...data, dream: { kind: "unavailable" as const } };
    expect(todayLedeOf(en.ledger.app, "en", overview, noDream, agentName)).toBe(
      "Four proposals, one stale fact and a question from Codex are waiting on you.",
    );
    expect(todayLedeOf(zh.ledger.app, "zh", overview, noDream, agentName)).toBe(
      "4 条提议、1 条过时的事实和 Codex 的一个问题在等你。",
    );
  });

  it("builds MobileToday.png's headline", () => {
    expect(todayHeadline(en.ledger.app, en.ledger.today, "en", data)).toBe(
      "Four proposals and a question are waiting on you.",
    );
    const one = {
      ...data,
      waiting: { ...data.waiting, proposals: 1, questions: [] },
    };
    expect(todayHeadline(en.ledger.app, en.ledger.today, "en", one)).toBe(
      "One proposal is waiting on you.",
    );
    const none = {
      ...data,
      waiting: { ...data.waiting, proposals: 0, stale: 0, questions: [] },
    };
    expect(todayHeadline(en.ledger.app, en.ledger.today, "en", none)).toBe(
      "Nothing is waiting on you.",
    );
  });

  it("words an agent's day and the footer", () => {
    const [cc, , cu] = data.agents!.rows;
    expect(agentDay(en.ledger.today, cc!)).toBe("41 read · 3 kept");
    expect(agentDay(en.ledger.today, cu!)).toBe("12 read · none");
    expect(agentDay(en.ledger.today, { ...cc!, reads: null })).toBe("3 kept");
    expect(
      todayFooter(
        en.ledger.today,
        {
          compiledAt: "2026-10-05T14:31:00-07:00",
          dreamAt: "03:00",
          kept: 214,
          space: "memax-v2",
        },
        when,
      ),
    ).toBe(
      "Compiled at 14:31 · Dream runs nightly at 03:00 · 214 memories kept in memax-v2",
    );
    expect(
      todayFooter(
        en.ledger.today,
        { compiledAt: null, dreamAt: null, kept: null, space: "x" },
        when,
      ),
    ).toBeNull();
  });
});
