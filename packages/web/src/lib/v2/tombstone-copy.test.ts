import { describe, expect, it } from "vitest";
import { en } from "@/i18n/locales/en";
import { zh } from "@/i18n/locales/zh";
import { DEMO_TOMBSTONES } from "./data/demo-memories-data";
import type { TombstoneView } from "./data/memories";
import {
  forgottenLabel,
  goneLines,
  reachLines,
  tombstoneLines,
  tombstoneMeta,
  type TombstoneContext,
} from "./tombstone-copy";

const names: Record<string, string> = {
  codex: "Codex",
  "claude-code": "Claude Code",
  cursor: "Cursor",
  opencode: "OpenCode",
  gemini: "Gemini CLI",
};
const ctx = (locale: "en" | "zh"): TombstoneContext => {
  const t = locale === "en" ? en : zh;
  return {
    rc: t.ledger.records,
    app: t.ledger.app,
    timeZone: "America/Vancouver",
    locale,
    you: { initials: "ZZ" },
    agentName: (key) => names[key] ?? key,
  };
};
const EN = en.ledger.memory.tombstone;
const ZH = zh.ledger.memory.tombstone;
const board = DEMO_TOMBSTONES["memax-v2/M-0201"]!;

describe("Tombstone.png in words", () => {
  it("heads it as the board does", () => {
    expect(forgottenLabel(EN, board, ctx("en"))).toBe(
      "Forgotten Oct 3, 10:12, at your request",
    );
    expect(tombstoneMeta(EN, board, ctx("en"))).toBe(
      "Kept Sep 21 · read 23 times before it was forgotten",
    );
    expect(forgottenLabel(ZH, board, ctx("zh"))).toContain("按你的要求忘记");
  });

  it("tells how it was forgotten, step by step", () => {
    const lines = tombstoneLines(EN, board, ctx("en"));
    expect(lines.map((l) => [l.title, l.time])).toEqual([
      ["You asked to forget it", "Oct 3, 10:12"],
      ["Removed from Memax", "10:12"],
      ["CLAUDE.md, AGENTS.md and Cursor rules rewritten", "10:12"],
      ["ChatGPT project instructions updated", "10:12"],
      ["Claude Code told on its next read", "10:31"],
      ["Codex told on its next read", "11:02"],
      ["Cursor told on its next read", "12:30"],
      ["OpenCode told on its next read", "13:05"],
      ["Gemini CLI is paused", "waiting"],
    ]);
    expect(lines[0]!.person).toBe("ZZ");
    expect(lines[0]!.detail).toBe(
      "From its page. Your note: personal, and not something agents need.",
    );
    expect(lines[1]!.state).toBe("forgotten");
    expect(lines[1]!.detail).toBe(
      "The statement, its sources, its search entry and its embedding.",
    );
    expect(lines[4]!.agent).toBe("claude-code");
    expect(lines[8]!.state).toBe("off");
    expect(lines[8]!.detail).toBe(
      "It is told before it can read anything else.",
    );
  });

  it("says what is gone, and what Memax can't reach", () => {
    expect(goneLines(EN, board, ctx("en"))).toEqual([
      "The statement and its two sources",
      "Its search index entry and embedding",
      "Its lines in 4 compiled files",
      "Cached copies in 5 agents, on their next read",
    ]);
    expect(reachLines(EN, board, ctx("en"))).toEqual([
      "Earlier commits of CLAUDE.md, AGENTS.md and .cursor/rules/memax.mdc in MemaxLabs/memax",
      "What Claude Code, Codex and Cursor copied into their own memory",
      "Database backups, for 7 days. Memax never restores one without forgetting it again.",
      "Voyage AI, which processed the words for search",
      "Wherever you pasted ChatGPT project",
    ]);
    expect(reachLines(ZH, board, ctx("zh"))[2]).toBe(
      "数据库备份，保留 7 天。Memax 恢复任何备份时都会先再次忘记它。",
    );
  });

  it("says where a Forget still stands while it propagates", () => {
    const working: TombstoneView = {
      ...board,
      status: "propagating",
      by: { kind: "person", self: false },
      requestedBy: null,
      note: null,
      steps: [
        { ...board.steps[0]!, at: board.at },
        {
          ...board.steps[2]!,
          status: "waiting",
          reason: "delivery",
          at: null,
        },
        {
          ...board.steps[2]!,
          key: "held",
          status: "held",
          reason: "hand_edit",
          at: null,
          target: {
            label: ".cursor/rules",
            kind: "cursor_mdc",
            delivery: "local",
          },
        },
        { ...board.steps[8]!, status: "waiting", at: null },
      ],
    };
    const lines = tombstoneLines(EN, working, ctx("en"));
    expect(lines.map((l) => l.title)).toEqual([
      "A teammate asked to forget it",
      "Removed from Memax",
      "CLAUDE.md compiled without it",
      ".cursor/rules has a hand edit",
      "Written to the forget ledger",
    ]);
    expect(lines[0]!.detail).toBe("From its page.");
    expect(lines[2]!.state).toBe("working");
    expect(lines[3]!.state).toBe("proposed");
  });

  it("says why a carried memory went", () => {
    const carried: TombstoneView = {
      ...board,
      ref: "M-0202",
      carried: { reason: "cites", primary: "M-0201" },
    };
    const [asked] = tombstoneLines(EN, carried, ctx("en"));
    expect(asked!.title).toBe("Forgotten with M-0201");
    expect(asked!.detail).toBe("It cites M-0201, so it carried its words.");
  });
});
