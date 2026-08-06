import { describe, it, expect } from "vitest";
import type { BoardSlot } from "memax-sdk";
import { buildBoardCardContext } from "./board-card-context";

function slot(overrides: Partial<BoardSlot>): BoardSlot {
  return {
    id: "s1",
    board_id: "b1",
    slot_key: "1-wow",
    kind: "pattern",
    title: "你每次改部署配置都在周五",
    state: "fresh",
    created_at: "2026-08-05T00:00:00Z",
    updated_at: "2026-08-05T00:00:00Z",
    ...overrides,
  };
}

describe("buildBoardCardContext", () => {
  it("includes the claim and every quoted receipt", () => {
    const block = buildBoardCardContext(
      slot({
        payload: {
          body: "过去三个月，5 次部署配置改动里有 4 次发生在周五下午。",
          quotes: [
            {
              memory_id: "m1",
              when: "2026-06-02T10:00:00Z",
              excerpt: "改了 staging 的 fly 配置",
            },
            {
              memory_id: "m2",
              when: "2026-07-11T18:00:00Z",
              excerpt: "周五又动了 worker 的内存上限",
            },
          ],
        },
      }),
    );
    expect(block).toBeTruthy();
    expect(block).toContain("你每次改部署配置都在周五");
    expect(block).toContain("过去三个月");
    expect(block).toContain("改了 staging 的 fly 配置");
    expect(block).toContain("周五又动了 worker 的内存上限");
    // The envelope is the same one memory batch-copy uses, so pasting
    // into any agent behaves identically.
    expect(block).toContain('<memax-context source="memax.app"');
    expect(block).toContain('scope="board card · pattern"');
  });

  it("flattens echo's then/now pair into receipts", () => {
    const block = buildBoardCardContext(
      slot({
        kind: "echo",
        payload: {
          body: "四月的问题，八月有答案了。",
          then: {
            memory_id: "m1",
            when: "2026-04-09T00:00:00Z",
            excerpt: "persona 跟设备走还是跟云走？",
          },
          now: {
            memory_id: "m2",
            when: "2026-08-05T00:00:00Z",
            excerpt: "persona 绑定 memax agent。",
          },
        },
      }),
    );
    expect(block).toContain("persona 跟设备走还是跟云走？");
    expect(block).toContain("persona 绑定 memax agent。");
    expect(block).toContain("2026-04-09");
  });

  it("falls back to description when there is no body", () => {
    const block = buildBoardCardContext(
      slot({ payload: { description: "只有描述的卡片" } }),
    );
    expect(block).toContain("只有描述的卡片");
  });

  it("returns null for a card with nothing quotable", () => {
    // Pure-count Lane A cards (week diff, trace) carry structured
    // numbers, not prose — pasting an empty block into an agent is
    // worse than hiding the affordance.
    expect(
      buildBoardCardContext(
        slot({ kind: "week", payload: { this_week: 12, last_week: 4 } }),
      ),
    ).toBeNull();
    expect(buildBoardCardContext(slot({ payload: undefined }))).toBeNull();
  });

  it("ignores malformed quote entries", () => {
    const block = buildBoardCardContext(
      slot({
        payload: {
          body: "claim",
          quotes: [
            { memory_id: "m1" },
            { excerpt: 42 },
            { excerpt: "real one" },
          ],
        },
      }),
    );
    expect(block).toContain("real one");
    expect(block).not.toContain("42");
  });
});
