import { describe, expect, it } from "vitest";
import { en } from "@/i18n/locales/en";
import { zh } from "@/i18n/locales/zh";
import { createDemoActivity } from "../data/activity-demo";
import { activityCategory, type ActivityEntry } from "../data/activity";
import { DEMO_SPACES } from "../data/demo-dataset";
import { activityCsv, activityCsvName, csvCell } from "./csv";
import {
  activitySentences,
  dayKey,
  dayLabel,
  sentenceText,
  viaText,
  zoneLabel,
  type Names,
} from "./sentence";
import { countWeek } from "./totals";

const NOW = new Date("2026-10-05T14:40:00-07:00");
const TZ = "America/Vancouver";
const AGENTS: Record<string, string> = {
  codex: "Codex",
  "claude-code": "Claude Code",
  cursor: "Cursor",
};
const names = (locale: "en" | "zh"): Names => ({
  locale,
  agent: (k) => AGENTS[k] ?? k,
  level: (a) => ({ read: "Read", propose: "Propose", write: "Write" })[a],
  surface: (s) => s.toUpperCase(),
});

const v2 = DEMO_SPACES.find((s) => s.slug === "memax-v2")!;
const demo = createDemoActivity();

function entry(over: Partial<ActivityEntry>): ActivityEntry {
  return {
    id: "r1",
    at: "2026-10-05T14:00:00-07:00",
    actor: { kind: "you", initials: "ZZ" },
    action: "kept",
    object: { kind: "memory", ref: "M-0001", id: "m1" },
    via: [{ kind: "via", via: "web" }],
    rawVia: "web",
    session: null,
    source: null,
    reason: null,
    ...over,
  };
}

describe("Activity sentences", () => {
  it("reproduce the board's rows word for word", async () => {
    const page = await demo.activity({ space: v2 });
    const rows = page.entries.map((e) => [
      sentenceText(activitySentences(en.ledger.activity, e, names("en")), "en"),
      viaText(en.ledger.activity, e.via, names("en")),
    ]);
    expect(rows).toEqual([
      [
        "Codex asked you a question: “Which deploy target should the v2 API use?”",
        "cloud task 9f1c",
      ],
      [
        "Claude Code verified a memory against the code. Still true.",
        "session 7c2f",
      ],
      [
        "Memax compiled CLAUDE.md, AGENTS.md and the ChatGPT project. Cursor's file was left alone: it has a local edit.",
        "3 of 4 targets",
      ],
      ["You handed a session to Codex, carrying 7 memories.", "web"],
      ["Claude Code read 12 memories and the Brief.", "MCP · session 7c2f"],
      [
        "You kept “Name compiled files after the tool they serve.”",
        "Review · from Cursor",
      ],
      [
        "You rejected “Use Temporal for long-running workflows.” Reason: superseded by M-0219.",
        "Review · from Cursor",
      ],
      [
        "Dream folded 34 notes into 6 facts, flagged 1 stale and faded 11.",
        "edition 214",
      ],
      [
        "A hand edit changed .cursor/rules/memax.mdc outside Memax. It's marked drifted.",
        "repository",
      ],
      ["You edited the Brief: one fact reworded.", "web"],
      ["Cursor read 18 memories.", "MCP · IDE"],
      [
        "You forgot a memory. Removed from 4 files and 5 agents; the tombstone stays.",
        "web",
      ],
    ]);
  });

  it("say only what a receipt holds when the words aren't there", () => {
    const say = (e: ActivityEntry) =>
      sentenceText(activitySentences(en.ledger.activity, e, names("en")), "en");
    expect(say(entry({}))).toBe("You kept a memory.");
    expect(say(entry({ actor: { kind: "person" }, action: "proposed" }))).toBe(
      "A member proposed a memory.",
    );
    expect(
      say(
        entry({
          action: "autonomy_changed",
          object: { kind: "agent", ref: "codex", id: "c1" },
          detail: { kind: "autonomy", level: "read" },
        }),
      ),
    ).toBe("You set Codex to Read.");
    expect(
      say(
        entry({
          action: "disconnected",
          object: { kind: "agent", ref: "cursor", id: "c3" },
        }),
      ),
    ).toBe("You disconnected Cursor. Its credential is revoked.");
    expect(
      say(entry({ action: "rejected", reason: "duplicate of M-0002" })),
    ).toBe("You rejected a proposal. Reason: duplicate of M-0002.");
  });

  it("speak Chinese with every placeholder filled", async () => {
    const page = await demo.activity({ space: v2 });
    for (const e of page.entries) {
      const text = sentenceText(
        activitySentences(zh.ledger.activity, e, names("zh")),
        "zh",
      );
      expect(text).not.toMatch(/\{\w+\}/);
    }
    expect(
      sentenceText(
        activitySentences(zh.ledger.activity, page.entries[11]!, names("zh")),
        "zh",
      ),
    ).toBe("你忘记了一条记忆。已从 4 个文件和 5 个 Agent 里移除，墓碑会留下。");
  });
});

describe("Activity days and zones", () => {
  it("groups by the viewer's day: Today, Yesterday, then the date", () => {
    const label = (iso: string) =>
      dayLabel(en.ledger.activity, dayKey(iso, TZ), NOW, TZ, "en");
    expect(label("2026-10-05T03:12:00-07:00")).toBe("Today");
    expect(label("2026-10-04T16:40:00-07:00")).toBe("Yesterday");
    expect(label("2026-10-03T10:12:00-07:00")).toBe("Saturday, October 3");
    // 23:30 in Vancouver is already the next day in UTC: still Sunday here.
    expect(dayKey("2026-10-05T06:30:00Z", TZ)).toBe("2026-10-04");
    expect(
      dayLabel(
        zh.ledger.activity,
        dayKey("2026-10-03T10:12:00-07:00", TZ),
        NOW,
        TZ,
        "zh",
      ),
    ).toBe("10月3日星期六");
  });

  it("names the zone the times are in", () => {
    expect(zoneLabel(en.ledger.activity, TZ, "en")).toBe(
      "Times in Vancouver (PT)",
    );
    expect(zoneLabel(en.ledger.activity, "UTC", "en")).toMatch(/^Times in /);
    expect(zoneLabel(zh.ledger.activity, TZ, "zh")).toBe(
      "时间按北美太平洋时间",
    );
  });
});

describe("weekly totals from what's loaded", () => {
  it("counts the last 7 days, and says when the week isn't all loaded", async () => {
    const page = await demo.activity({ space: v2 });
    const counted = countWeek(page.entries, {
      now: NOW,
      hasMore: false,
      readsListed: true,
    });
    expect(counted).toEqual({
      totals: {
        reads: 2,
        proposals: 0,
        kept: 1,
        rejected: 1,
        forgotten: 1,
        compiles: 1,
      },
      complete: true,
    });
    const partial = countWeek(page.entries, {
      now: NOW,
      hasMore: true,
      readsListed: false,
    });
    expect(partial.complete).toBe(false);
    // Reads aren't receipts: unknown, not zero.
    expect(partial.totals.reads).toBeNull();
    // An older page loaded: the week is covered even with more to come.
    const old = entry({ at: "2026-09-20T10:00:00-07:00" });
    expect(
      countWeek([...page.entries, old], {
        now: NOW,
        hasMore: true,
        readsListed: false,
      }).complete,
    ).toBe(true);
  });
});

describe("the CSV export", () => {
  it("quotes what needs quoting and defuses formulas", () => {
    expect(csvCell("plain")).toBe("plain");
    expect(csvCell("a, b")).toBe('"a, b"');
    expect(csvCell('say "hi"')).toBe('"say ""hi"""');
    expect(csvCell("two\nlines")).toBe('"two\nlines"');
    expect(csvCell("=HYPERLINK(1)")).toBe("'=HYPERLINK(1)");
    expect(csvCell("-1+1")).toBe("'-1+1");
    expect(csvCell("@sum")).toBe("'@sum");
  });

  it("writes one line per receipt, with the API's field names and no memory text", async () => {
    const page = await demo.activity({ space: v2 });
    const csv = activityCsv(page.entries);
    expect(csv.startsWith("﻿")).toBe(true);
    const lines = csv.slice(1).trimEnd().split("\r\n");
    expect(lines[0]).toBe(
      "occurred_at,actor_kind,actor,action,object_kind,object_ref,via,session,source,reason,receipt_id",
    );
    expect(lines).toHaveLength(page.entries.length + 1);
    expect(lines[1]).toMatch(
      /^2026-10-05T14:40:00-07:00,agent,codex,asked,gate,H-0093,,9f1c,,,/,
    );
    expect(lines[7]).toContain(",person,ZZ,rejected,memory,M-0429,review,");
    expect(lines[7]).toContain("superseded by M-0219.");
    // Receipts never hold the words, and neither does the export.
    expect(csv).not.toContain("Use Temporal");
    expect(activityCsvName("memax-v2", NOW)).toBe(
      "memax-v2-activity-2026-10-05.csv",
    );
  });

  it("files every action under one filter", () => {
    expect(activityCategory(entry({ action: "read" }))).toBe("reads");
    expect(activityCategory(entry({ action: "drifted" }))).toBe("compiles");
    expect(activityCategory(entry({ action: "forgot" }))).toBe("forgets");
    expect(activityCategory(entry({ action: "autonomy_changed" }))).toBe(
      "writes",
    );
  });
});

describe("the judge's receipts", () => {
  const cases: Array<[ActivityEntry["action"], string]> = [
    ["judged", "You checked M-0431 for duplicates and conflicts."],
    ["linked", "You linked M-0431 to the memory it updates."],
    ["superseded", "You superseded M-0431 with a newer decision."],
  ];

  it.each(cases)("words %s in en and zh", (action, english) => {
    const e = entry({
      action,
      object: { kind: "memory", ref: "M-0431", id: "m1" },
    });
    const say = (locale: "en" | "zh") =>
      sentenceText(
        activitySentences(
          (locale === "en" ? en : zh).ledger.activity,
          e,
          names(locale),
        ),
        locale,
      );
    expect(say("en")).toBe(english);
    expect(say("zh")).toContain("M-0431");
    expect(say("zh")).not.toMatch(/[A-Za-z]{4,} [a-z]/); // no English left in zh
  });
});

describe("decision gate receipts", () => {
  const say = (e: ActivityEntry, locale: "en" | "zh") =>
    sentenceText(
      activitySentences(
        (locale === "en" ? en : zh).ledger.activity,
        e,
        names(locale),
      ),
      locale,
    );
  const gate = { kind: "gate" as const, ref: "G-0012", id: "g1" };

  it("words an agent's question, an answer and a withdrawal in en and zh", () => {
    const asked = entry({
      action: "asked",
      actor: { kind: "agent", agent: "codex" },
      object: gate,
    });
    const answered = entry({ action: "answered", object: gate });
    const withdrawn = entry({
      action: "withdrawn",
      actor: { kind: "agent", agent: "codex" },
      object: gate,
    });
    expect(say(asked, "en")).toBe("Codex asked you a question.");
    expect(say(answered, "en")).toBe("You answered a question.");
    expect(say(withdrawn, "en")).toBe("Codex withdrew a question (G-0012).");
    expect(say(asked, "zh")).toBe("Codex 问了你一个问题。");
    expect(say(withdrawn, "zh")).toBe("Codex 撤回了一个问题（G-0012）。");
    for (const e of [asked, answered, withdrawn]) {
      expect(say(e, "zh")).not.toMatch(/[A-Za-z]{4,} [a-z]/); // no English left in zh
      expect(activityCategory(e)).toBe("writes");
    }
  });
});

describe("the compile pipeline's receipts", () => {
  const cases: Array<[ActivityEntry["action"], string, string]> = [
    ["revised", "B-0043", "You revised the Brief (B-0043)."],
    ["configured", "AGENTS.md", "You changed how AGENTS.md is written."],
    ["requested", "AGENTS.md", "You asked for AGENTS.md to be compiled."],
    ["delivered", "AGENTS.md", "You wrote AGENTS.md to disk."],
    ["observed", "AGENTS.md", "You found a hand edit in AGENTS.md."],
    [
      "pulled",
      "AGENTS.md",
      "You pulled the hand edit in AGENTS.md into Review.",
    ],
    ["overwritten", "AGENTS.md", "You overwrote the hand edit in AGENTS.md."],
    ["stopped", "AGENTS.md", "You stopped compiling AGENTS.md."],
  ];

  it.each(cases)("words %s in en and zh", (action, ref, english) => {
    const e = entry({ action, object: { kind: "target", ref, id: "t1" } });
    const say = (locale: "en" | "zh") =>
      sentenceText(
        activitySentences(
          (locale === "en" ? en : zh).ledger.activity,
          e,
          names(locale),
        ),
        locale,
      );
    expect(say("en")).toBe(english);
    expect(say("zh")).toContain(ref);
    expect(say("zh")).not.toMatch(/[A-Za-z]{4,} [a-z]/); // no English left in zh
  });

  it("files compile-pipeline receipts under compiles, a Brief revision under writes", () => {
    for (const [action] of cases) {
      expect(activityCategory(entry({ action }))).toBe(
        action === "revised" ? "writes" : "compiles",
      );
    }
  });
});
