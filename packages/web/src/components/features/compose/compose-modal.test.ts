import { describe, it, expect } from "vitest";

// Re-import via a thin export indirection so the test doesn't need to
// pull React + Tiptap into the unit-test runtime. The pure helper sits
// alongside the React component; tests target only the helper.
//
// We use a ts-only re-export shim rather than tweaking compose-modal.tsx
// because `compose-modal.tsx` is `"use client"` — Vitest's node env can
// import it but pulls Tiptap's heavy deps. The re-export keeps the
// helper testable in isolation.
import { deriveTitleFromContent } from "./compose-title";

describe("deriveTitleFromContent", () => {
  it("returns Untitled for empty input", () => {
    expect(deriveTitleFromContent("")).toBe("Untitled");
    expect(deriveTitleFromContent("   ")).toBe("Untitled");
    expect(deriveTitleFromContent("\n\n")).toBe("Untitled");
  });

  it("uses the first non-empty line", () => {
    expect(deriveTitleFromContent("\n\nhello world\nrest")).toBe("hello world");
  });

  it("strips a leading heading marker", () => {
    expect(deriveTitleFromContent("# Big idea")).toBe("Big idea");
    expect(deriveTitleFromContent("## Smaller idea")).toBe("Smaller idea");
    expect(deriveTitleFromContent("###  Trimmed")).toBe("Trimmed");
  });

  it("strips a leading bullet or blockquote", () => {
    expect(deriveTitleFromContent("- bullet item")).toBe("bullet item");
    expect(deriveTitleFromContent("* star item")).toBe("star item");
    expect(deriveTitleFromContent("> quoted text")).toBe("quoted text");
  });

  it("strips a leading numbered list marker", () => {
    expect(deriveTitleFromContent("1. first item")).toBe("first item");
    expect(deriveTitleFromContent("12. twelfth")).toBe("twelfth");
  });

  it("caps title at 80 characters", () => {
    const long = "a".repeat(120);
    expect(deriveTitleFromContent(long).length).toBe(80);
  });

  it("falls back to the raw first line when stripping leaves nothing", () => {
    // The strip regex requires whitespace after the marker, so a bare
    // "#" doesn't match — we keep the trimmed first line as-is rather
    // than substituting "Untitled" for what the user clearly intended.
    expect(deriveTitleFromContent("#  ")).toBe("#");
  });
});
