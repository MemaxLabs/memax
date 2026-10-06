import { describe, expect, it } from "vitest";
import { en } from "@/i18n/locales/en";
import { zh } from "@/i18n/locales/zh";
import { memoryReadsText, reachMeta, reachText } from "./reads-copy";

const pe = en.ledger.memory.page;
const pz = zh.ledger.memory.page;

describe("a memory's reads", () => {
  it("counts them as Memory.png does, and says nothing it wasn't told", () => {
    const say = (reads: number | null, readsUnobserved?: boolean) =>
      memoryReadsText(pe, { reads, readsUnobserved }, "en");
    expect(say(214)).toBe("Read 214 times");
    expect(say(1)).toBe("Read once");
    expect(say(1284)).toBe("Read 1,284 times");
    expect(say(0)).toBe("Not read yet");
    expect(say(null)).toBeNull();
    // In a file whose loads Memax can't see, the count is a floor.
    expect(say(214, true)).toBe("Read at least 214 times");
    expect(say(1, true)).toBe("Read at least once");
    expect(say(0, true)).toBe("No reads counted yet");
    expect(memoryReadsText(pz, { reads: 214 }, "zh")).toBe("被读过 214 次");
    expect(
      memoryReadsText(pz, { reads: 214, readsUnobserved: true }, "zh"),
    ).toBe("至少被读过 214 次");
  });

  it("says what it reaches, leaving out what isn't known or is none", () => {
    expect(reachText(pe, { files: 4, agents: 5 }, "en")).toBe(
      "reaches 4 files and 5 agents",
    );
    expect(reachMeta(pe, { files: 4, agents: 5 }, "en")).toBe(
      "4 files · 5 agents",
    );
    // The targets didn't load, or it isn't kept: only the agents.
    expect(reachText(pe, { files: null, agents: 1 }, "en")).toBe(
      "reaches 1 agent",
    );
    expect(reachMeta(pe, { files: null, agents: 5 }, "en")).toBe("5 agents");
    expect(reachText(pe, { files: 1, agents: 0 }, "en")).toBe("reaches 1 file");
    expect(reachText(pe, { files: 0, agents: 0 }, "en")).toBeNull();
    expect(reachMeta(pe, null, "en")).toBeNull();
    expect(reachText(pz, { files: 4, agents: 5 }, "zh")).toBe(
      "覆盖 4 个文件和 5 个 Agent",
    );
    expect(reachText(pz, { files: null, agents: 5 }, "zh")).toBe(
      "覆盖 5 个 Agent",
    );
  });
});
