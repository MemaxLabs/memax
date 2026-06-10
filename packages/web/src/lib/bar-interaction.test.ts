import { describe, expect, it } from "vitest";
import {
  deriveBarInteractionState,
  getBarClickOutsideAction,
  getRawCommandInput,
  getSendKind,
  getShouldRememberSubmit,
  parseCommittedCommand,
  parseMention,
  parseSlashCommand,
  pickDefaultNavIndex,
} from "@/lib/bar-interaction";

describe("bar interaction helpers", () => {
  it("keeps plain slash as discovery only", () => {
    expect(parseSlashCommand("/")).toEqual({ command: "", query: "" });
    expect(parseCommittedCommand("/")).toBeNull();
  });

  it("does not auto-commit partial command prefixes", () => {
    expect(parseSlashCommand("/h")).toEqual({ command: "h", query: "" });
    expect(parseCommittedCommand("/h")).toBeNull();
  });

  it("commits /hub but NOT /topic (replaced by @-mention 2026-04-21)", () => {
    expect(parseCommittedCommand("/hub memax")).toEqual({
      command: "hub",
      query: "memax",
    });
    // `/topic` no longer parses as a slash command — topic scoping moved
    // to `@`-mention. The URL route /memories/topics/{id} is unchanged.
    expect(parseCommittedCommand("/topic")).toBeNull();
    expect(parseCommittedCommand("/topic foo")).toBeNull();
  });

  it("returns raw slash text for degraded command chips", () => {
    expect(getRawCommandInput("hub")).toBe("/hub");
  });

  describe("parseMention", () => {
    it("fires only at start-of-input (not after space or mid-string)", () => {
      expect(parseMention("@foo")).toEqual({ query: "foo" });
      expect(parseMention("@")).toEqual({ query: "" });
      expect(parseMention("hello @foo")).toBeNull();
      expect(parseMention(" @foo")).toBeNull();
      expect(parseMention("support@stripe.com")).toBeNull();
    });

    it("normalizes fullwidth ＠ (common on CJK keyboards)", () => {
      expect(parseMention("＠foo")).toEqual({ query: "foo" });
      expect(parseMention("＠")).toEqual({ query: "" });
    });

    it("returns null mid-IME composition so transient buffers don't flip mode", () => {
      expect(parseMention("@foo", { composing: true })).toBeNull();
      expect(parseMention("＠foo", { composing: true })).toBeNull();
      expect(parseMention("@foo", { composing: false })).toEqual({
        query: "foo",
      });
    });

    it("returns null for unrelated prefixes", () => {
      expect(parseMention("/hub")).toBeNull();
      expect(parseMention("hi")).toBeNull();
      expect(parseMention("")).toBeNull();
    });
  });

  describe("deriveBarInteractionState mention mode", () => {
    const base = {
      stagedFileCount: 0,
      allFilesReady: true,
      scope: { kind: "recall" } as const,
      phase: "input" as const,
      isBarExpanded: false,
    };
    it("flags isMentionMode and suppresses recall surfaces for @ input", () => {
      expect(
        deriveBarInteractionState({ ...base, value: "@foo" }),
      ).toMatchObject({
        isMentionMode: true,
        showRecallSurfaces: false,
        hasLiftSignal: true,
        hasExpandContent: true,
      });
    });
    it("does NOT flag isMentionMode during IME composition", () => {
      expect(
        deriveBarInteractionState({
          ...base,
          value: "@foo",
          composing: true,
        }),
      ).toMatchObject({
        isMentionMode: false,
        showRecallSurfaces: true,
      });
    });
    it("does NOT flag isMentionMode for email-like input", () => {
      expect(
        deriveBarInteractionState({ ...base, value: "support@stripe.com" }),
      ).toMatchObject({ isMentionMode: false });
    });
  });

  it("recalls for any text (plain or multiline), remembers only when files are staged", () => {
    expect(getSendKind(0)).toBe("recall");
    expect(getSendKind(1)).toBe("remember");
  });

  it("uses force-remember only for the current submit", () => {
    expect(
      getShouldRememberSubmit({
        stagedFileCount: 0,
        forceRemember: true,
      }),
    ).toBe(true);
    expect(
      getShouldRememberSubmit({
        stagedFileCount: 0,
        forceRemember: false,
      }),
    ).toBe(false);
  });

  it("keeps committed command mode lifted and expanded on empty query", () => {
    expect(
      deriveBarInteractionState({
        value: "",
        stagedFileCount: 0,
        allFilesReady: true,
        scope: { kind: "command", command: "hub" },
        phase: "input",
        isBarExpanded: false,
      }),
    ).toMatchObject({
      isCommandScope: true,
      isCommandMode: true,
      showRecallSurfaces: false,
      hasLiftSignal: true,
      hasExpandContent: true,
      sendKind: "recall",
    });
  });

  it("suppresses recall surfaces when files are staged", () => {
    expect(
      deriveBarInteractionState({
        value: "what is this?",
        stagedFileCount: 1,
        allFilesReady: true,
        scope: { kind: "recall" },
        phase: "input",
        isBarExpanded: false,
      }),
    ).toMatchObject({
      hasFiles: true,
      isRememberMode: true,
      showRecallSurfaces: false,
      canRemember: true,
      sendKind: "remember",
    });
  });
});

describe("pickDefaultNavIndex", () => {
  // Regression: non-Pro users mistakenly saved on Enter because the bar's
  // single-item fallback auto-activated the lone `__remember` nav entry.
  // The north-star (docs/design/bar-northstar.md §4.3) says Remember is
  // NEVER the Enter default — always ⌘↵ or an explicit click/arrow-nav.
  // See activateNavigationSelection in packages/web/src/contexts/bar-context.tsx.
  it("respects an explicitly highlighted index regardless of what it is", () => {
    expect(pickDefaultNavIndex([{ id: "__remember" }], 0)).toBe(0);
    expect(pickDefaultNavIndex([{ id: "__synthesis" }, { id: "r1" }], 1)).toBe(
      1,
    );
  });

  it("returns null for empty nav items (no default)", () => {
    expect(pickDefaultNavIndex([], null)).toBeNull();
  });

  it("auto-activates a single non-remember item (Raycast/Linear pattern)", () => {
    expect(pickDefaultNavIndex([{ id: "__synthesis" }], null)).toBe(0);
    expect(pickDefaultNavIndex([{ id: "cmd:topic" }], null)).toBe(0);
    expect(pickDefaultNavIndex([{ id: "memory-abc" }], null)).toBe(0);
  });

  it("does NOT auto-activate when the single item is __remember (non-Pro bug guard)", () => {
    expect(pickDefaultNavIndex([{ id: "__remember" }], null)).toBeNull();
  });

  it("returns null for multi-item lists without an explicit highlight (falls through to handleSubmit)", () => {
    expect(
      pickDefaultNavIndex([{ id: "__synthesis" }, { id: "__remember" }], null),
    ).toBeNull();
    expect(
      pickDefaultNavIndex([{ id: "__remember" }, { id: "memory-1" }], null),
    ).toBeNull();
  });
});

describe("getBarClickOutsideAction", () => {
  const base = {
    isOverlay: false,
    aiLoading: false,
    phase: "input" as const,
    hasState: false,
  };

  it("hides overlay when the bar is opened via ⌘K on a non-bar page", () => {
    expect(getBarClickOutsideAction({ ...base, isOverlay: true })).toBe(
      "hide-overlay",
    );
  });

  it("clears AI only during active streaming, NOT on finished answer", () => {
    // Narrowed 2026-04-21: a finished askAnswer preserves via
    // suppress-with-draft like any other recall-result state. Only
    // aiLoading (stream in progress) cancels.
    expect(getBarClickOutsideAction({ ...base, aiLoading: true })).toBe(
      "clear-ai",
    );
  });

  it("suppresses the expand surface when input-phase bar holds a draft", () => {
    // Key regression guard: draft content must NEVER be wiped on tap-outside.
    expect(getBarClickOutsideAction({ ...base, hasState: true })).toBe(
      "suppress-with-draft",
    );
  });

  it("collapses an empty input-phase bar without suppressing", () => {
    expect(getBarClickOutsideAction(base)).toBe("collapse-empty");
  });

  it("suppresses (does NOT reset) when a recall surface is active", () => {
    // User ask 2026-04-21: "有 recall 结果后 点击背景应该也只是收起来 而不是回退搜索框状态."
    // Click-outside during recall-loading / recall-result must preserve,
    // not reset. No code path returns "reset" anymore.
    expect(getBarClickOutsideAction({ ...base, phase: "loading" })).toBe(
      "suppress-with-draft",
    );
    expect(getBarClickOutsideAction({ ...base, phase: "recall-result" })).toBe(
      "suppress-with-draft",
    );
  });

  it("prioritizes overlay > aiLoading > phase branches when multiple signals overlap", () => {
    expect(
      getBarClickOutsideAction({
        isOverlay: true,
        aiLoading: true,
        phase: "recall-result",
        hasState: true,
      }),
    ).toBe("hide-overlay");
    // aiLoading still wins over recall-result when a stream is active.
    expect(
      getBarClickOutsideAction({
        isOverlay: false,
        aiLoading: true,
        phase: "recall-result",
        hasState: true,
      }),
    ).toBe("clear-ai");
    // Finished answer (aiLoading=false) with recall state → suppress.
    expect(
      getBarClickOutsideAction({
        isOverlay: false,
        aiLoading: false,
        phase: "recall-result",
        hasState: true,
      }),
    ).toBe("suppress-with-draft");
  });
});
