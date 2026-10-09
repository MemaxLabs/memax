import { describe, expect, it } from "vitest";
import type { Notification } from "memax-sdk";
import { classifyDrawerRows } from "./notification-drawer";

function row(overrides: Partial<Notification>): Notification {
  return {
    id: "n1",
    audience: "user",
    kind: "hub_member_joined",
    status: "pending",
    priority: 0,
    source_kind: "",
    created_at: "2026-09-01T00:00:00Z",
    ...overrides,
  } as Notification;
}

describe("classifyDrawerRows", () => {
  it("routes broadcasts / news / receipts and counts unseen", () => {
    const buckets = classifyDrawerRows([
      row({
        id: "b1",
        kind: "system_notice",
        source_kind: "system_notice",
      }),
      row({
        id: "w1",
        kind: "system_notice",
        source_kind: "onboarding_welcome",
      }),
      row({ id: "h1", kind: "hub_member_joined" }),
      row({
        id: "h2",
        kind: "hub_member_joined",
        seen_at: "2026-09-02T00:00:00Z",
      }),
      row({ id: "r1", kind: "dream_run_completed" }),
      row({ id: "r2", kind: "hub_invite_accepted", status: "resolved" }),
      // Decision kinds are the board's business, never the drawer's.
      row({ id: "d1", kind: "hub_invite" }),
      row({ id: "c1", kind: "review_contradiction" }),
    ]);
    expect(buckets.broadcasts.map((n) => n.id)).toEqual(["b1"]);
    // Onboarding welcome stays a pinned board card, not a broadcast.
    expect(buckets.news.map((n) => n.id)).toEqual(["h1", "h2"]);
    // Resolved rows drop out; decision kinds never enter.
    expect(buckets.receipts.map((n) => n.id)).toEqual(["r1"]);
    // b1 + h1 + r1 unseen; h2 seen.
    expect(buckets.unseen).toBe(3);
  });

  it("routes agent_connected into news (wow moment, drawer-only)", () => {
    const buckets = classifyDrawerRows([
      row({ id: "a1", kind: "agent_connected", source_kind: "agent" }),
    ]);
    expect(buckets.news.map((n) => n.id)).toEqual(["a1"]);
    expect(buckets.unseen).toBe(1);
  });

  it("sorts each section newest first", () => {
    const buckets = classifyDrawerRows([
      row({ id: "old", created_at: "2026-08-01T00:00:00Z" }),
      row({ id: "new", created_at: "2026-09-10T00:00:00Z" }),
    ]);
    expect(buckets.news.map((n) => n.id)).toEqual(["new", "old"]);
  });

  it("routes onboarding rows to their own bucket, checklist first, uncounted", () => {
    const buckets = classifyDrawerRows([
      row({
        id: "w",
        kind: "system_notice",
        source_kind: "onboarding_welcome",
        created_at: "2026-09-02T00:00:00Z",
      }),
      row({ id: "c", kind: "checklist", source_kind: "onboarding" }),
      row({
        id: "b",
        kind: "system_notice",
        source_kind: "release",
      }),
    ]);
    expect(buckets.onboarding.map((n) => n.id)).toEqual(["c", "w"]);
    expect(buckets.broadcasts.map((n) => n.id)).toEqual(["b"]);
    expect(buckets.unseen).toBe(1);
  });
});
