import { describe, expect, it } from "vitest";
import { en } from "@/i18n/locales/en";
import { zh } from "@/i18n/locales/zh";
import {
  agentsLede,
  eyebrow,
  formatAge,
  formatAgo,
  formatDayTitle,
  formatReceiptTime,
  formatWhen,
  memoriesLede,
  spaceMeta,
  statusLabel,
  statusState,
  todayLede,
} from "./copy";
import { DEMO_NOW, DEMO_OVERVIEWS, DEMO_SPACES } from "./data/demo-dataset";
import type { SpaceOverview } from "./data/types";

const EN = en.ledger.app;
const ZH = zh.ledger.app;
const names: Record<string, string> = {
  codex: "Codex",
  cursor: "Cursor",
  "claude-code": "Claude Code",
};
const agentName = (key: string) => names[key] ?? key;
const v2 = DEMO_OVERVIEWS["memax-v2"];
const space = (slug: string) => DEMO_SPACES.find((s) => s.slug === slug)!;
const now = new Date(DEMO_NOW);
const VANCOUVER = "America/Vancouver";

describe("ledes, word for word with the boards", () => {
  it("Today (Main.png)", () => {
    expect(todayLede(EN, "en", v2, agentName)).toBe(
      "Overnight, Dream folded 34 notes into 6 facts. Four proposals, one stale fact and a question from Codex are waiting on you.",
    );
    expect(todayLede(ZH, "zh", v2, agentName)).toBe(
      "昨晚，Dream 把 34 条笔记整理成了 6 条事实。4 条提议、1 条过时的事实和 Codex 的一个问题在等你。",
    );
  });

  it("Today with one thing, many questions, or nothing", () => {
    const base: SpaceOverview = { ...v2, dream: null };
    expect(
      todayLede(
        EN,
        "en",
        {
          ...base,
          waitingBreakdown: { proposals: 1, stale: 0, questions: [] },
        },
        agentName,
      ),
    ).toBe("One proposal is waiting on you.");
    expect(
      todayLede(
        EN,
        "en",
        {
          ...base,
          waitingBreakdown: {
            proposals: 0,
            stale: 0,
            questions: ["codex", "cursor"],
          },
        },
        agentName,
      ),
    ).toBe("Two questions from Codex and Cursor are waiting on you.");
    // No breakdown (the SDK source): just the count.
    expect(
      todayLede(
        EN,
        "en",
        { ...base, waitingBreakdown: null, waiting: 12 },
        agentName,
      ),
    ).toBe("12 things are waiting on you.");
    expect(
      todayLede(EN, "en", DEMO_OVERVIEWS["memax-web"], agentName),
    ).toBeNull();
  });

  it("Memories (Memories.png)", () => {
    expect(memoriesLede(EN, "en", v2)).toBe(
      "214 kept, 5 waiting on you and 3 forgotten. Kept is the quiet default, so only the exceptions carry a mark.",
    );
    expect(memoriesLede(ZH, "zh", v2)).toBe(
      "214 条已保留、5 条在等你和 3 条已忘记。保留是默认状态，所以只有例外才带标记。",
    );
    expect(memoriesLede(EN, "en", DEMO_OVERVIEWS["memax-web"])).toBeNull();
  });

  it("Agents (Agents.png)", () => {
    expect(agentsLede(EN, "en", v2)).toBe(
      "Six agents are connected and five are active. Each one reads this context; how much each may write is up to you.",
    );
    expect(
      agentsLede(EN, "en", { ...v2, agents: { connected: 1, active: 1 } }),
    ).toMatch(/^One agent is connected and one is active\./);
    expect(agentsLede(ZH, "zh", v2)).toBe(
      "已连接 6 个 Agent，其中 5 个在用。每个都会读这份上下文；各自能写多少，由你决定。",
    );
    expect(agentsLede(EN, "en", DEMO_OVERVIEWS["memax-web"])).toBeNull();
  });
});

describe("the frame's lines", () => {
  it("status line, as the boards write it", () => {
    expect(
      statusLabel(EN, { kind: "drifted", agent: "cursor" }, agentName),
    ).toBe("Cursor file drifted");
    expect(statusLabel(EN, { kind: "in-sync", agents: 5 }, agentName)).toBe(
      "5 agents in sync",
    );
    expect(statusLabel(EN, { kind: "in-sync", agents: 1 }, agentName)).toBe(
      "1 agent in sync",
    );
    expect(statusLabel(EN, { kind: "no-agents" }, agentName)).toBe(
      "No agents yet",
    );
    expect(statusState({ kind: "drifted", agent: "cursor" })).toBe("proposed");
    expect(statusState({ kind: "in-sync", agents: 5 })).toBe("kept");
    expect(statusState({ kind: "no-agents" })).toBe("off");
  });

  it("switcher meta (SpaceSwitcher.png)", () => {
    expect(DEMO_SPACES.map((s) => spaceMeta(EN, s))).toEqual([
      "Just you · 61 memories",
      "Project · 214 memories · 5 agents",
      "Project · new, no agents yet",
      "Team · 2 people · 7 agents",
    ]);
    // Counts the API doesn't serve yet: the kind alone.
    expect(
      spaceMeta(EN, { ...space("memax-v2"), kept: null, agents: null }),
    ).toBe("Project");
  });

  it("eyebrows", () => {
    expect(eyebrow(EN, space("memax-v2"))).toBe("memax-v2 · Project space");
    expect(eyebrow(EN, space("memax-team"))).toBe(
      "Memax team · Team space · 2 people, 7 agents",
    );
    expect(eyebrow(ZH, space("memax-v2"))).toBe("memax-v2 · 项目空间");
  });
});

describe("times, in the viewer's zone", () => {
  it("Today's title", () => {
    expect(formatDayTitle(now, VANCOUVER, "en")).toBe("Monday, October 5");
    expect(formatDayTitle(now, VANCOUVER, "zh")).toBe("10月5日星期一");
    // Late on Monday in Vancouver is already Tuesday in UTC.
    expect(
      formatDayTitle(new Date("2026-10-05T23:30:00-07:00"), VANCOUVER, "en"),
    ).toBe("Monday, October 5");
  });

  it("receipt times, ages and 'last review'", () => {
    expect(
      formatReceiptTime(EN, "2026-10-05T14:31:00-07:00", now, VANCOUVER, "en"),
    ).toBe("today 14:31");
    expect(
      formatReceiptTime(EN, "2026-10-02T10:12:00-07:00", now, VANCOUVER, "en"),
    ).toBe("Oct 2");
    expect(formatAge(EN, "2026-10-05T11:40:00-07:00", now)).toBe("3 h");
    expect(formatAge(EN, "2026-10-05T14:26:00-07:00", now)).toBe("14 min");
    expect(formatAgo(EN, "2026-10-05T14:18:00-07:00", now)).toBe(
      "22 minutes ago",
    );
    expect(formatAgo(ZH, "2026-10-05T14:18:00-07:00", now)).toBe("22 分钟前");
    expect(
      formatWhen(EN, "2026-10-05T09:41:00-07:00", now, VANCOUVER, "en"),
    ).toBe("today at 09:41");
    expect(
      formatWhen(EN, "2026-10-03T09:41:00-07:00", now, VANCOUVER, "en"),
    ).toBe("Oct 3 at 09:41");
  });
});
