import { describe, expect, it } from "vitest";
import {
  ariaKeyshortcuts,
  detectPlatform,
  eventKey,
  formatChord,
  keycaps,
  matchesChord,
  parseKeys,
  type KeyEventLike,
} from "./keys";

function key(key: string, init: Partial<KeyEventLike> = {}): KeyEventLike {
  return {
    key,
    code: init.code,
    metaKey: false,
    ctrlKey: false,
    altKey: false,
    shiftKey: false,
    ...init,
  };
}

describe("parseKeys", () => {
  it("reads chords, sequences and named keys", () => {
    expect(parseKeys("Mod+K")).toEqual([
      { key: "k", mod: true, shift: false, alt: false },
    ]);
    expect(parseKeys("G T").map((c) => c.key)).toEqual(["g", "t"]);
    expect(parseKeys("Mod+Shift+C")[0]).toMatchObject({
      key: "c",
      mod: true,
      shift: true,
    });
    expect(parseKeys("ArrowDown")[0].key).toBe("arrowdown");
    expect(parseKeys("Mod+Enter")[0]).toMatchObject({
      key: "enter",
      mod: true,
    });
    expect(parseKeys("?")[0].key).toBe("?");
  });

  it("refuses specs it can't read", () => {
    expect(() => parseKeys("")).toThrow();
    expect(() => parseKeys("Hyper+K")).toThrow(/modifier/);
    expect(() => parseKeys("Mod+Banana")).toThrow(/key/);
  });
});

describe("matchesChord", () => {
  const [modK] = parseKeys("Mod+K");
  const [g] = parseKeys("G");
  const [question] = parseKeys("?");
  const [modShiftC] = parseKeys("Mod+Shift+C");

  it("uses ⌘ on a Mac and Ctrl elsewhere, never both", () => {
    expect(matchesChord(key("k", { metaKey: true }), modK, "mac")).toBe(true);
    expect(matchesChord(key("k", { ctrlKey: true }), modK, "mac")).toBe(false);
    expect(matchesChord(key("k", { ctrlKey: true }), modK, "other")).toBe(true);
    expect(matchesChord(key("k", { metaKey: true }), modK, "other")).toBe(
      false,
    );
    expect(
      matchesChord(key("k", { metaKey: true, ctrlKey: true }), modK, "mac"),
    ).toBe(false);
  });

  it("keeps bare keys bare", () => {
    expect(matchesChord(key("g"), g, "mac")).toBe(true);
    expect(matchesChord(key("g", { metaKey: true }), g, "mac")).toBe(false);
    expect(matchesChord(key("G", { shiftKey: true }), g, "mac")).toBe(false);
    expect(matchesChord(key("g", { altKey: true }), g, "other")).toBe(false);
  });

  it("ignores Shift for punctuation that needs it", () => {
    expect(matchesChord(key("?", { shiftKey: true }), question, "mac")).toBe(
      true,
    );
    expect(matchesChord(key("?"), question, "other")).toBe(true);
  });

  it("requires Shift when the chord names it", () => {
    expect(
      matchesChord(
        key("C", { metaKey: true, shiftKey: true }),
        modShiftC,
        "mac",
      ),
    ).toBe(true);
    expect(matchesChord(key("c", { metaKey: true }), modShiftC, "mac")).toBe(
      false,
    );
  });
});

describe("eventKey", () => {
  it("falls back to the physical key on other layouts", () => {
    // A Russian layout types п on the G key.
    expect(eventKey(key("п", { code: "KeyG" }))).toBe("g");
    // ⌥G on a Mac types ©.
    expect(eventKey(key("©", { code: "KeyG", altKey: true }))).toBe("g");
    // ⇧1 types ! on a US layout.
    expect(eventKey(key("!", { code: "Digit1", shiftKey: true }))).toBe("1");
  });

  it("names named keys", () => {
    expect(eventKey(key("ArrowDown"))).toBe("arrowdown");
    expect(eventKey(key("Enter"))).toBe("enter");
  });
});

describe("platform labels", () => {
  it("draws Mac caps as the boards do", () => {
    expect(keycaps("Mod+K", "mac")).toEqual(["⌘K"]);
    expect(keycaps("Mod+Shift+C", "mac")).toEqual(["⌘⇧C"]);
    expect(keycaps("Mod+Enter", "mac")).toEqual(["⌘↵"]);
    expect(keycaps("Mod+Z", "mac")).toEqual(["⌘Z"]);
    expect(keycaps("G T", "mac")).toEqual(["G", "T"]);
    expect(keycaps("ArrowDown", "mac")).toEqual(["↓"]);
    expect(keycaps("?", "mac")).toEqual(["?"]);
  });

  it("spells modifiers elsewhere", () => {
    expect(keycaps("Mod+K", "other")).toEqual(["Ctrl+K"]);
    expect(keycaps("Mod+Shift+S", "other")).toEqual(["Ctrl+Shift+S"]);
    expect(keycaps("Mod+Enter", "other")).toEqual(["Ctrl+Enter"]);
    expect(keycaps("Mod+1", "other")).toEqual(["Ctrl+1"]);
    expect(keycaps("G T", "other")).toEqual(["G", "T"]);
    expect(formatChord(parseKeys("Escape")[0], "other")).toBe("Esc");
  });

  it("gives aria-keyshortcuts for chords, not sequences", () => {
    expect(ariaKeyshortcuts("Mod+K", "mac")).toBe("Meta+K");
    expect(ariaKeyshortcuts("Mod+K", "other")).toBe("Control+K");
    expect(ariaKeyshortcuts("Mod+Shift+C", "other")).toBe("Control+Shift+C");
    expect(ariaKeyshortcuts("Mod+Enter", "mac")).toBe("Meta+Enter");
    expect(ariaKeyshortcuts("R", "mac")).toBe("R");
    expect(ariaKeyshortcuts("G T", "mac")).toBeUndefined();
  });

  it("detects Apple platforms", () => {
    expect(detectPlatform({ platform: "MacIntel" })).toBe("mac");
    expect(detectPlatform({ userAgentData: { platform: "macOS" } })).toBe(
      "mac",
    );
    expect(detectPlatform({ platform: "iPhone" })).toBe("mac");
    expect(detectPlatform({ platform: "Win32" })).toBe("other");
    expect(detectPlatform({ platform: "Linux x86_64" })).toBe("other");
    expect(detectPlatform(undefined)).toBe("other");
  });
});
