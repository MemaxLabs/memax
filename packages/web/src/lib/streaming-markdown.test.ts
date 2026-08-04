import { describe, expect, it } from "vitest";
import { stabilizeStreamingMarkdown } from "./streaming-markdown";

describe("stabilizeStreamingMarkdown", () => {
  it("passes complete markdown through untouched", () => {
    const s = "Hello **world**\n\n```ts\nconst x = 1;\n```\n\nDone `inline`.";
    expect(stabilizeStreamingMarkdown(s)).toBe(s);
  });

  it("closes an unclosed code fence", () => {
    const s = "Before\n\n```go\nfunc main() {";
    expect(stabilizeStreamingMarkdown(s)).toBe(s + "\n```");
  });

  it("closes a fence that ends with a newline without doubling it", () => {
    const s = "```\npartial\n";
    expect(stabilizeStreamingMarkdown(s)).toBe(s + "```");
  });

  it("handles sequential complete fences plus one open fence", () => {
    const s = "```\na\n```\ntext\n```js\nb";
    expect(stabilizeStreamingMarkdown(s)).toBe(s + "\n```");
  });

  it("closes an unclosed inline code span on the trailing line", () => {
    const s = "Use `memax agents sync";
    expect(stabilizeStreamingMarkdown(s)).toBe(s + "`");
  });

  it("leaves balanced inline code alone", () => {
    expect(stabilizeStreamingMarkdown("Use `a` and `b`.")).toBe(
      "Use `a` and `b`.",
    );
  });

  it("ignores escaped backticks", () => {
    const s = "A literal \\` stays put";
    expect(stabilizeStreamingMarkdown(s)).toBe(s);
  });

  it("does not treat backticks inside fences as inline spans", () => {
    const s = "```\necho `date`\n```\nfine";
    expect(stabilizeStreamingMarkdown(s)).toBe(s);
  });

  it("handles ~~~ fences", () => {
    const s = "~~~\ncode";
    expect(stabilizeStreamingMarkdown(s)).toBe(s + "\n```");
  });

  it("returns empty input unchanged", () => {
    expect(stabilizeStreamingMarkdown("")).toBe("");
  });
});
