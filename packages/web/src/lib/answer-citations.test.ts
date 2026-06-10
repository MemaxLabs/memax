import { describe, expect, it } from "vitest";
import { parseAnswerCitations } from "@/lib/answer-citations";

describe("parseAnswerCitations", () => {
  it("returns empty for empty text", () => {
    expect(parseAnswerCitations("", 5)).toEqual([]);
  });

  it("returns plain text when no citations", () => {
    expect(parseAnswerCitations("hello world", 5)).toEqual([
      { kind: "text", text: "hello world" },
    ]);
  });

  it("parses single citation [1]", () => {
    expect(parseAnswerCitations("see [1] here", 3)).toEqual([
      { kind: "text", text: "see " },
      { kind: "citation", index: 1 },
      { kind: "text", text: " here" },
    ]);
  });

  it("parses range citation [1-3] into individual nodes", () => {
    expect(parseAnswerCitations("see [1-3]", 5)).toEqual([
      { kind: "text", text: "see " },
      { kind: "citation", index: 1 },
      { kind: "citation", index: 2 },
      { kind: "citation", index: 3 },
    ]);
  });

  it("parses consecutive citations [1][2][3]", () => {
    expect(parseAnswerCitations("[1][2][3]", 3)).toEqual([
      { kind: "citation", index: 1 },
      { kind: "citation", index: 2 },
      { kind: "citation", index: 3 },
    ]);
  });

  it("degrades out-of-range [0] to plain text", () => {
    expect(parseAnswerCitations("[0] test", 3)).toEqual([
      { kind: "text", text: "[0]" },
      { kind: "text", text: " test" },
    ]);
  });

  it("degrades [99] > sourceCount to plain text", () => {
    expect(parseAnswerCitations("see [99]", 3)).toEqual([
      { kind: "text", text: "see " },
      { kind: "text", text: "[99]" },
    ]);
  });

  it("handles partial token [1 as plain text (streaming)", () => {
    expect(parseAnswerCitations("see [1", 3)).toEqual([
      { kind: "text", text: "see [1" },
    ]);
  });

  it("handles partial range [1- as plain text (streaming)", () => {
    expect(parseAnswerCitations("see [1-", 3)).toEqual([
      { kind: "text", text: "see [1-" },
    ]);
  });

  it("handles mixed prose and citations", () => {
    const result = parseAnswerCitations(
      "Rate limiting [2] and caching [1] are key.",
      3,
    );
    expect(result).toEqual([
      { kind: "text", text: "Rate limiting " },
      { kind: "citation", index: 2 },
      { kind: "text", text: " and caching " },
      { kind: "citation", index: 1 },
      { kind: "text", text: " are key." },
    ]);
  });

  it("degrades range where end > sourceCount", () => {
    expect(parseAnswerCitations("[2-5]", 3)).toEqual([
      { kind: "text", text: "[2-5]" },
    ]);
  });

  it("degrades inverted range [3-1]", () => {
    expect(parseAnswerCitations("[3-1]", 5)).toEqual([
      { kind: "text", text: "[3-1]" },
    ]);
  });

  it("streaming: appending chars produces consistent output", () => {
    const full = "see [1] here";
    for (let i = 1; i <= full.length; i++) {
      const partial = full.slice(0, i);
      const nodes = parseAnswerCitations(partial, 3);
      // Should never throw, and every node should have content
      for (const node of nodes) {
        if (node.kind === "text") {
          expect(node.text.length).toBeGreaterThan(0);
        } else {
          expect(node.index).toBeGreaterThanOrEqual(1);
        }
      }
    }
    // Final parse should have the citation
    const final = parseAnswerCitations(full, 3);
    expect(final).toEqual([
      { kind: "text", text: "see " },
      { kind: "citation", index: 1 },
      { kind: "text", text: " here" },
    ]);
  });
});
