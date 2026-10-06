import { describe, expect, it } from "vitest";
import { en } from "@/i18n/locales/en";
import { zh } from "@/i18n/locales/zh";
import {
  actorName,
  dateTime,
  failureText,
  noteText,
  railTime,
  stampOf,
} from "./records-copy";

const EN = en.ledger.records;
const ZH = zh.ledger.records;
const TZ = "America/Vancouver";
const NOW = new Date("2026-10-05T14:40:00-07:00");
const at = (time: string, day = "2026-10-05") => `${day}T${time}:00-07:00`;
const names: Record<string, string> = {
  codex: "Codex",
  "claude-code": "Claude Code",
};
const agentName = (key: string) => names[key] ?? key;

describe("rail times", () => {
  it.each([
    [at("14:26"), "14 min ago", "14 分钟前"],
    [at("13:40"), "1 h ago", "1 小时前"],
    [at("11:40"), "3 h ago", "3 小时前"],
    [at("03:12"), "03:12", "03:12"],
    [at("10:58", "2026-10-02"), "Oct 2", "10月2日"],
    [at("14:40"), "just now", "刚刚"],
  ])("%s", (iso, english, chinese) => {
    expect(railTime(EN, iso, NOW, TZ, "en")).toBe(english);
    expect(railTime(ZH, iso, NOW, TZ, "zh")).toBe(chinese);
  });

  it("dates lineage with the clock", () => {
    expect(dateTime(EN, at("10:41", "2026-10-02"), TZ, "en")).toBe(
      "Oct 2, 10:41",
    );
    expect(dateTime(ZH, at("10:41", "2026-10-02"), TZ, "zh")).toBe(
      "10月2日 10:41",
    );
  });
});

describe("actors", () => {
  it("names agents, the viewer and people the source can't name", () => {
    expect(actorName({ kind: "agent", agent: "codex" }, EN, agentName)).toBe(
      "Codex",
    );
    expect(actorName({ kind: "person", self: true }, EN, agentName)).toBe(
      "You",
    );
    expect(actorName({ kind: "person", self: false }, EN, agentName)).toBe(
      "A teammate",
    );
    expect(
      actorName({ kind: "person", self: false, name: "Jiahao" }, ZH, agentName),
    ).toBe("Jiahao");
  });

  it("stamps a person only with initials it knows", () => {
    expect(
      stampOf({ kind: "person", self: true }, EN, { initials: "ZZ" }),
    ).toEqual({ person: "ZZ", name: "You" });
    expect(stampOf({ kind: "person", self: false }, EN)).toBeNull();
    expect(stampOf({ kind: "dream" }, EN)).toEqual({ agent: "dream" });
  });
});

describe("row notes (Memories.png)", () => {
  const conflict = {
    kind: "conflict" as const,
    with: "M-0174",
    keptBy: { kind: "person" as const, self: false, name: "Jiahao" },
    askedBy: "codex",
  };

  it.each([
    [
      conflict,
      "Conflicts with M-0174, kept by Jiahao. Codex asked you to decide.",
    ],
    [
      {
        kind: "stale" as const,
        changedAt: at("16:00", "2026-09-11"),
        source: "PR #198",
      },
      "Its source changed on Sep 11 · PR #198",
    ],
    [
      {
        kind: "merged" as const,
        into: "M-0219",
        by: { kind: "dream" as const },
      },
      "Merged into M-0219 by Dream",
    ],
    [
      { kind: "faded" as const, days: 60 },
      "No agent has read it in 60 days. Restore it, or let it rest.",
    ],
  ])("%j", (note, text) => {
    expect(noteText(EN, note, agentName, TZ, "en")).toBe(text);
  });

  it("joins Chinese sentences without a space", () => {
    expect(noteText(ZH, conflict, agentName, TZ, "zh")).toBe(
      "和 M-0174 冲突，那条是 Jiahao 保留的。Codex 在等你拿主意。",
    );
  });
});

describe("why a command didn't go through", () => {
  const ctx = { command: "keep" as const, ref: "M-0430", space: "memax-v2" };

  it("words policy refusals by code, with what to do", () => {
    expect(
      failureText(
        EN,
        { kind: "refused", code: "external_needs_review", message: null },
        { ...ctx, locale: "en" },
      ),
    ).toBe(
      "M-0430 wasn't kept. It quotes an outside source, so only a web sign-in can keep it. Sign out, sign in again on this page, then keep it.",
    );
    expect(
      failureText(
        EN,
        { kind: "refused", code: "owners_keep", message: null },
        { ...ctx, locale: "en" },
      ),
    ).toBe(
      "M-0430 wasn't kept. Only owners keep in memax-v2. Ask an owner to keep it.",
    );
    expect(
      failureText(
        ZH,
        { kind: "refused", code: "viewer", message: null },
        { ...ctx, locale: "zh" },
      ),
    ).toBe("M-0430 没保留上。查看者可以提议和评论，保留要由成员来。");
  });

  it("falls back to the server's message for a code it doesn't know", () => {
    expect(
      failureText(
        EN,
        { kind: "refused", code: "brand_new", message: "Plan limit reached." },
        { ...ctx, locale: "en" },
      ),
    ).toBe("M-0430 wasn't kept. Memax refused it: Plan limit reached.");
    // The catalogue's own fallback keys are never a policy code.
    expect(
      failureText(
        EN,
        { kind: "refused", code: "other", message: null },
        { ...ctx, locale: "en" },
      ),
    ).toBe("M-0430 wasn't kept. Memax refused it.");
  });

  it.each([
    [
      { kind: "unreachable" } as const,
      "It didn't reach Memax, so nothing changed. Try again.",
    ],
    [
      { kind: "rate-limited", retryAfter: 5 } as const,
      "That was a lot at once. Wait 5 seconds, then try again.",
    ],
    [
      { kind: "decided" } as const,
      "It was already decided, so it's gone from your queue.",
    ],
  ])("%j", (failure, reason) => {
    expect(
      failureText(EN, failure, { ...ctx, command: "reject", locale: "en" }),
    ).toBe(`M-0430 wasn't rejected. ${reason}`);
  });
});
