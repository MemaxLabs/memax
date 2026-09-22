import { describe, expect, it } from "vitest";
import type { ChecklistPayload, Notification } from "memax-sdk";
import {
  findOnboardingChecklist,
  firstUnfinishedStep,
  quickStartProgress,
} from "./quick-start-dialog";

const AT = "2026-09-20T08:00:00Z";
const ALL = [
  "welcome",
  "connect_agent",
  "first_memory",
  "first_ask",
  "five_memories",
  "first_hub_invite",
  "first_dream",
];

function payload(done: string[], allDone = false): ChecklistPayload {
  return {
    items: ALL.map((id) => ({
      id,
      title: id,
      completed_at: done.includes(id) ? AT : undefined,
    })),
    required_ids: [
      "connect_agent",
      "first_memory",
      "first_ask",
      "five_memories",
      "first_dream",
    ],
    all_done_at: allDone ? AT : undefined,
  } as unknown as ChecklistPayload;
}

describe("firstUnfinishedStep", () => {
  it("starts at welcome for a fresh checklist", () => {
    expect(firstUnfinishedStep(payload([]))).toBe("welcome");
  });
  it("keeps the remember card until five_memories is also done", () => {
    expect(
      firstUnfinishedStep(
        payload(["welcome", "connect_agent", "first_memory"]),
      ),
    ).toBe("first_memory");
    expect(
      firstUnfinishedStep(
        payload(["welcome", "connect_agent", "first_memory", "five_memories"]),
      ),
    ).toBe("first_ask");
  });
  it("lands on the use-cases page once every step is done", () => {
    const all = payload(ALL, true);
    expect(firstUnfinishedStep(all)).toBe("use_cases");
    expect(quickStartProgress(all)).toEqual({
      done: 6,
      total: 6,
      next: "use_cases",
      allDone: true,
    });
  });
  it("trusts the server's all_done_at even before the client counts catch up", () => {
    expect(quickStartProgress(payload(["welcome"], true)).allDone).toBe(true);
  });
});

describe("findOnboardingChecklist", () => {
  const row = (o: Partial<Notification>) =>
    ({
      id: "x",
      kind: "checklist",
      source_kind: "onboarding",
      status: "pending",
      created_at: AT,
    }) as Notification;
  it("returns null without a pending onboarding checklist", () => {
    expect(findOnboardingChecklist(undefined)).toBeNull();
    expect(
      findOnboardingChecklist([
        { ...row({}), id: "resolved", status: "resolved" },
        { ...row({}), id: "other", source_kind: "campaign" },
      ]),
    ).toBeNull();
  });
  it("prefers the newest checklist", () => {
    expect(
      findOnboardingChecklist([
        { ...row({}), id: "old", created_at: "2026-09-01T00:00:00Z" },
        { ...row({}), id: "new", created_at: "2026-09-21T00:00:00Z" },
      ])?.id,
    ).toBe("new");
  });
});
