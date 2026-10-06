import { describe, expect, it } from "vitest";
import { en } from "@/i18n/locales/en";
import { zh } from "@/i18n/locales/zh";
import {
  actorName,
  dateTime,
  failureText,
  noteText,
  undoFailureText,
  undoneText,
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

  it("says why a busy Keep or a conflict settlement didn't go through", () => {
    expect(
      failureText(
        EN,
        { kind: "busy", retryAfter: 1, ref: "M-0430", judge: false },
        { ...ctx, command: "keep", locale: "en" },
      ),
    ).toBe(
      "M-0430 wasn't kept. Another change is holding it. Try again in a moment.",
    );
    expect(
      failureText(
        EN,
        { kind: "busy", retryAfter: 2, ref: "M-0430", judge: true },
        { ...ctx, command: "keep", locale: "en" },
      ),
    ).toBe(
      "M-0430 wasn't kept. Memax is still checking it against the decision in force. Try again in a moment.",
    );
    expect(
      failureText(
        EN,
        { kind: "in-conflict", with: "M-0174" },
        { ...ctx, command: "keep", locale: "en" },
      ),
    ).toBe(
      "M-0430 wasn't kept. It contradicts M-0174, a decision in force. Compare both sides to settle it.",
    );
    expect(
      failureText(
        ZH,
        { kind: "in-conflict", with: null },
        { ...ctx, command: "keep", locale: "zh" },
      ),
    ).toBe("M-0430 没保留上。它和一条现行的决策矛盾。对比两边，定下来。");
    expect(
      failureText(
        ZH,
        { kind: "refused", code: "decision_needs_web", message: null },
        { ref: "M-0431", space: "memax-v2", command: "resolve", locale: "zh" },
      ),
    ).toBe(
      "M-0431 没裁定成。memax-v2 里的决策要在网页上登录才能保留。退出后在这个页面重新登录，再来保留。",
    );
  });
});

describe("what an undo says (no mark)", () => {
  it.each([
    [
      "keep",
      false,
      "Undid the keep. M-0430 is back in Review.",
      "已撤销保留。M-0430 回到了审阅。",
    ],
    [
      "reject",
      false,
      "Undid the rejection. M-0430 is back in Review.",
      "已撤销拒绝。M-0430 回到了审阅。",
    ],
    [
      "edit",
      true,
      "Undid the edit and keep. M-0430 is back in Review.",
      "已撤销编辑和保留。M-0430 回到了审阅。",
    ],
    [
      "edit",
      false,
      "Undid your edit. M-0430 reads as it did before.",
      "已撤销你的编辑。M-0430 恢复成原来的文字。",
    ],
    [
      "resolve",
      false,
      "Undid the settlement. M-0430 is back in Review as a conflict.",
      "已撤销这次裁定。M-0430 作为冲突回到了审阅。",
    ],
    [
      "fold",
      false,
      "Unfolded M-0430. It's back in Review.",
      "已取消合并 M-0430。它回到了审阅。",
    ],
  ] as const)("%s (kept %s)", (command, kept, english, chinese) => {
    expect(undoneText(EN, command, "M-0430", { kept })).toBe(english);
    expect(undoneText(ZH, command, "M-0430", { kept })).toBe(chinese);
  });

  const refused = (
    reason:
      | "window_passed"
      | "already_undone"
      | "not_undoable"
      | "later_changes",
    ref: string | null = "M-0430",
  ) => ({ kind: "undo-refused", reason, ref }) as const;

  it.each([
    [
      refused("window_passed"),
      "keep",
      "M-0430 was decided more than 10 minutes ago, so it can't be undone. Change it on its page instead.",
    ],
    [
      refused("window_passed"),
      "fold",
      "M-0430 was folded more than 14 days ago, so it can't be unfolded. Change it on its page instead.",
    ],
    [
      refused("already_undone"),
      "keep",
      "That was already undone. M-0430 is as it was before.",
    ],
    [
      refused("not_undoable"),
      "edit",
      "That change to M-0430 can't be undone. Change it on its page instead.",
    ],
    [
      refused("later_changes", "M-0431"),
      "resolve",
      "M-0431 changed after this, so undoing it would lose that change. Undo that first, or change M-0430 on its page.",
    ],
    [
      refused("later_changes"),
      "keep",
      "M-0430 changed after this, so undoing it would lose that change. Change it on its page instead.",
    ],
    [
      refused("later_changes", "B-0043"),
      "keep",
      "The Brief cites M-0430 now. Take it out of the Brief first, then undo.",
    ],
    [
      { kind: "refused", code: "undo_by_decider", message: "Only…" } as const,
      "keep",
      "Only the person who decided M-0430 can undo it. Change it instead, or ask them.",
    ],
    [
      {
        kind: "refused",
        code: "viewer",
        message: "Viewers can't undo decisions.",
      } as const,
      "keep",
      "Memax refused the undo: Viewers can't undo decisions.",
    ],
    [
      { kind: "unreachable" } as const,
      "keep",
      "Undo didn't reach Memax, so nothing changed. Try again.",
    ],
    [
      { kind: "not-found" } as const,
      "keep",
      "M-0430 isn't here anymore, so there's nothing to undo.",
    ],
  ] as const)("%j (%s)", (failure, command, english) => {
    expect(undoFailureText(EN, failure, { command, ref: "M-0430" })).toBe(
      english,
    );
    // Every refusal has its own Chinese.
    const chinese = undoFailureText(ZH, failure, { command, ref: "M-0430" });
    expect(chinese).not.toBe(english);
    expect(chinese).toMatch(/[一-鿿]/);
  });
});
