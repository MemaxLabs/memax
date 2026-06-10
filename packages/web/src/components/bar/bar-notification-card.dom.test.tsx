// @vitest-environment jsdom

/**
 * Regression tests for the bar notification card's structural invariants.
 *
 * What these tests protect (see docs/design/memax-design-system.md § Bar
 * Notification Card):
 *
 *   1. The outer shell carries the `notif-glass` class so the shared CSS
 *      source of truth (packages/ui/src/notification-glass.css) applies.
 *   2. Dream-family kinds add the compound `is-dream` class — NOT a second
 *      sibling class — so the override is specificity-driven, not source-
 *      order-driven.
 *   3. The outer height comes from `var(--notif-outer)`. The inner row
 *      uses `h-full items-center` — content centers in the full outer
 *      (the legacy `--notif-content`/`--notif-tuck` strip-and-tuck
 *      split was for "tuck behind the bar" and was removed when the
 *      notif moved to a global bottom-anchored toast). Magic pixel
 *      heights anywhere in the card are a bug.
 *   4. The border radius comes from `var(--app-radius-surface)` so the bar and
 *      the notification always share a corner value.
 *   5. The dismiss button has `aria-label="Dismiss"` and a 44pt hit target
 *      via a pseudo-element (inferred from the `before:` class fragment).
 *
 * We mock `useBar` to inject a synthetic activeNotification; that's the
 * only hook this component reads, so the mock is minimal.
 */

import { describe, it, expect, vi, afterEach } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import type { BarNotificationType } from "@/lib/bar-types";

// Dynamic mock so each test can swap the active notification.
const mockActiveNotification = vi.hoisted(() => ({
  current: null as {
    type: BarNotificationType;
    message: string;
    detail?: string;
    onDismiss?: () => void;
    onAction?: () => void;
    actionLabel?: string;
  } | null,
}));

vi.mock("@/contexts/bar-context", () => ({
  useBar: () => ({
    activeNotification: mockActiveNotification.current,
  }),
}));

// Avoid AnimatePresence complexity in tests — render eagerly.
vi.mock("framer-motion", async () => {
  const React = await import("react");
  return {
    motion: new Proxy(
      {},
      {
        get: () => (props: Record<string, unknown>) => {
          const { children, ...rest } = props as {
            children?: React.ReactNode;
          };
          return React.createElement("div", rest, children);
        },
      },
    ),
    AnimatePresence: ({ children }: { children?: React.ReactNode }) =>
      React.createElement(React.Fragment, null, children),
  };
});

// Import AFTER the mocks are registered.
import { BarNotificationCard } from "./bar-notification-card";

afterEach(() => {
  cleanup();
  mockActiveNotification.current = null;
});

describe("BarNotificationCard", () => {
  it("applies the notif-glass class to the outer shell", () => {
    mockActiveNotification.current = {
      type: "info",
      message: "hello world",
    };
    render(<BarNotificationCard />);
    const card = screen.getByTestId("bar-notification-card");
    expect(card.classList.contains("notif-glass")).toBe(true);
  });

  it("adds the compound is-dream class for dream-family kinds", () => {
    const dreamKinds: BarNotificationType[] = [
      "dream",
      "dreaming",
      "dream_complete",
      "dream_clean",
    ];
    for (const kind of dreamKinds) {
      mockActiveNotification.current = { type: kind, message: "dreaming" };
      const { unmount } = render(<BarNotificationCard />);
      const card = screen.getByTestId("bar-notification-card");
      expect(card.classList.contains("notif-glass")).toBe(true);
      expect(card.classList.contains("is-dream")).toBe(true);
      // Must NOT also carry the legacy second-class variant.
      expect(card.classList.contains("notif-glass-dream")).toBe(false);
      unmount();
    }
  });

  it("does NOT add is-dream for non-dream kinds", () => {
    const neutralKinds: BarNotificationType[] = [
      "info",
      "success",
      "error",
      "update",
    ];
    for (const kind of neutralKinds) {
      mockActiveNotification.current = { type: kind, message: "hi" };
      const { unmount } = render(<BarNotificationCard />);
      const card = screen.getByTestId("bar-notification-card");
      expect(card.classList.contains("is-dream")).toBe(false);
      unmount();
    }
  });

  it("drives outer height from a CSS token, not a literal", () => {
    mockActiveNotification.current = { type: "info", message: "token test" };
    render(<BarNotificationCard />);
    const card = screen.getByTestId("bar-notification-card");
    // jsdom exposes the inline style attribute as the source-order string.
    const inlineStyle = card.getAttribute("style") ?? "";
    expect(inlineStyle).toContain("var(--notif-outer)");
    // Radius is owned by the .notif-glass recipe (see
    // @memaxlabs/ui/notification-glass.css), not inline.
    expect(card.classList.contains("notif-glass")).toBe(true);
    // Sanity: no leftover hardcoded pixel values on the outer shell.
    expect(inlineStyle).not.toMatch(/\b44px\b/);
    expect(inlineStyle).not.toMatch(/\b20px\b/);
  });

  it("does not split content into a separate height strip", () => {
    // Plan 26 follow-up: previous tuck/content split (30px content
    // strip + 14px tuck-bottom for "tuck behind the bar" illusion) was
    // collapsed when the notif was decoupled from the bar and globally
    // bottom-anchored. Content now centers in the full 44px outer.
    //
    // Implementation-agnostic invariant (per codex review): no
    // descendant of the card may declare a height that re-introduces
    // the strip. Specifically: no inline style references
    // `var(--notif-content)`, no literal 30px/14px/44px heights.
    // The outer's `var(--notif-outer)` is allowed (drives card height).
    mockActiveNotification.current = { type: "info", message: "content test" };
    const { container } = render(<BarNotificationCard />);
    const card = container.querySelector(
      "[data-testid='bar-notification-card']",
    ) as HTMLElement | null;
    expect(card).not.toBeNull();
    const descendants = card!.querySelectorAll<HTMLElement>("*");
    for (const node of descendants) {
      const style = node.getAttribute("style") ?? "";
      expect(style).not.toContain("var(--notif-content)");
      expect(style).not.toContain("var(--notif-tuck)");
      // No literal magic-number heights on descendants.
      expect(style).not.toMatch(/height:\s*30px/);
      expect(style).not.toMatch(/height:\s*14px/);
      expect(style).not.toMatch(/height:\s*44px/);
    }
  });

  it("dismiss button has aria-label and a pseudo-element hit expander", () => {
    const onDismiss = vi.fn();
    mockActiveNotification.current = {
      type: "info",
      message: "dismiss test",
      onDismiss,
    };
    render(<BarNotificationCard />);
    const dismiss = screen.getByTestId("bar-notification-dismiss");
    expect(dismiss.getAttribute("aria-label")).toBe("Dismiss");
    // The hit-area expander lives on the ::before pseudo via Tailwind.
    // We can't read pseudos in jsdom, so we assert the class fragment
    // that produces it. -inset-2.5 = 10px outward × all sides → 24 + 20 = 44.
    const cls = dismiss.className;
    expect(cls).toContain("before:absolute");
    expect(cls).toContain("before:-inset-2.5");
    expect(cls).toContain("before:content-['']");
  });

  it("renders nothing when activeNotification is null", () => {
    mockActiveNotification.current = null;
    const { container } = render(<BarNotificationCard />);
    expect(
      container.querySelector("[data-testid=bar-notification-card]"),
    ).toBeNull();
  });
});
