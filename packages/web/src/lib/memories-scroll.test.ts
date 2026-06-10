import { describe, expect, it } from "vitest";
import {
  getMemoriesScrollStorageKey,
  isMemoriesPath,
  isMemoriesRootPath,
  isMemoryDetailPath,
  isTopicPath,
  shouldRestoreMemoriesScroll,
} from "@/lib/memories-scroll";

describe("memories scroll helpers", () => {
  it("classifies memories routes correctly", () => {
    expect(isMemoriesRootPath("/memories")).toBe(true);
    expect(isTopicPath("/memories/topics/topic_1")).toBe(true);
    expect(isMemoryDetailPath("/memories/memory_1")).toBe(true);
    expect(isMemoryDetailPath("/memories/topics/topic_1")).toBe(false);
    expect(isMemoriesPath("/dreams")).toBe(false);
  });

  it("keys scroll storage by hub and route", () => {
    expect(getMemoriesScrollStorageKey("hub_1", "/memories")).toBe(
      "memax-memories-scroll:v1:hub_1:/memories",
    );
  });

  it("restores on browser back and forward within memories routes", () => {
    expect(
      shouldRestoreMemoriesScroll({
        previousPath: "/memories/topics/topic_1",
        nextPath: "/memories",
        isPopNavigation: true,
      }),
    ).toBe(true);
  });

  it("restores root scroll when returning to memories normally", () => {
    expect(
      shouldRestoreMemoriesScroll({
        previousPath: "/memories/topics/topic_1",
        nextPath: "/memories",
        isPopNavigation: false,
      }),
    ).toBe(true);
  });

  it("restores topic scroll when backing out of memory detail", () => {
    expect(
      shouldRestoreMemoriesScroll({
        previousPath: "/memories/memory_1",
        nextPath: "/memories/topics/topic_1",
        isPopNavigation: false,
      }),
    ).toBe(true);
  });

  it("does not restore on forward drill into topics", () => {
    expect(
      shouldRestoreMemoriesScroll({
        previousPath: "/memories",
        nextPath: "/memories/topics/topic_1",
        isPopNavigation: false,
      }),
    ).toBe(false);
    expect(
      shouldRestoreMemoriesScroll({
        previousPath: "/memories/topics/topic_1",
        nextPath: "/memories/topics/topic_2",
        isPopNavigation: false,
      }),
    ).toBe(false);
  });
});
