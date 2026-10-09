import { isValidElement } from "react";
import { describe, expect, it } from "vitest";
import { wordDiff } from "../memory/diff";
import { AGENTS, monogramFor, resolveAgent } from "./agents";
import { format, formatNodes, plural } from "./format";
import { toAriaKeyshortcuts } from "./keyshortcuts";

describe("format", () => {
  it("fills placeholders and leaves unknown ones visible", () => {
    expect(format("Kept by {name}", { name: "Jiahao" })).toBe("Kept by Jiahao");
    expect(format("{a} and {b}", { a: 1 })).toBe("1 and {b}");
  });

  it("keeps punctuation in the template's own text run", () => {
    const nodes = formatNodes(
      "Your answer is kept in {space}, authored by you.",
      {
        space: "memax-v2",
      },
    );
    expect(nodes).toHaveLength(3);
    expect(nodes[0]).toBe("Your answer is kept in ");
    expect(isValidElement(nodes[1])).toBe(true);
    expect(nodes[2]).toBe(", authored by you.");
  });

  it("picks singular and plural forms", () => {
    const notes = { one: "1 note", other: "{n} notes" };
    expect(plural(notes, 1)).toBe("1 note");
    expect(plural(notes, 34)).toBe("34 notes");
    expect(plural(notes, 1204, (n) => n.toLocaleString("en-US"))).toBe(
      "1,204 notes",
    );
  });
});

describe("agents", () => {
  it("resolves the shipped agents, persons and Dream", () => {
    expect(resolveAgent(AGENTS, { agent: "codex" })).toEqual({
      mono: "CX",
      name: "Codex",
      surface: "cloud",
      kind: "agent",
    });
    expect(resolveAgent(AGENTS, { agent: "dream" }).kind).toBe("dream");
    expect(resolveAgent(AGENTS, { person: "zz", name: "You" })).toEqual({
      mono: "ZZ",
      name: "You",
      surface: "person",
      kind: "person",
    });
  });

  it("accepts unknown agents gracefully", () => {
    expect(resolveAgent(AGENTS, { agent: "windsurf" })).toEqual({
      mono: "WI",
      name: "windsurf",
      surface: "agent",
      kind: "agent",
    });
    expect(
      resolveAgent(AGENTS, { agent: "zed-agent", name: "Zed Agent" }).mono,
    ).toBe("ZA");
    expect(resolveAgent(AGENTS, {}).mono).toBe("AG");
  });

  it("derives monograms without ever spelling AI", () => {
    expect(monogramFor("Windsurf")).toBe("WI");
    expect(monogramFor("Zed Agent")).toBe("ZA");
    expect(monogramFor("aider")).toBe("AD");
    expect(monogramFor("通义灵码")).toBe("通义");
    expect(monogramFor("---")).toBe("AG");
  });
});

describe("toAriaKeyshortcuts", () => {
  it.each([
    ["K", "K"],
    ["⌘Z", "Meta+Z"],
    ["⌘↵", "Meta+Enter"],
    ["⌘K", "Meta+K"],
    ["↵", "Enter"],
    ["Esc", "Escape"],
    ["⌘⇧C", "Meta+Shift+C"],
    ["1", "1"],
  ])("%s → %s", (cap, expected) => {
    expect(toAriaKeyshortcuts(cap)).toBe(expected);
  });

  it("returns nothing for a cap it can't read", () => {
    expect(toAriaKeyshortcuts("")).toBeUndefined();
    expect(toAriaKeyshortcuts("Hyper")).toBeUndefined();
  });
});

describe("wordDiff", () => {
  it("finds the changed words and keeps the rest", () => {
    const parts = wordDiff(
      "MCP write tools must ask for confirmation through elicitation.",
      "MCP write tools must ask for confirmation with input_required.",
    );
    expect(parts).toEqual([
      { op: "eq", text: "MCP write tools must ask for confirmation " },
      { op: "del", text: "through" },
      { op: "add", text: "with" },
      { op: "eq", text: " " },
      { op: "del", text: "elicitation." },
      { op: "add", text: "input_required." },
    ]);
  });

  it("reassembles both sides", () => {
    const before = "Deploy the v2 API to Railway.";
    const after = "Deploy the v2 API to Fly.io in iad and ams.";
    const parts = wordDiff(before, after);
    expect(
      parts
        .filter((p) => p.op !== "add")
        .map((p) => p.text)
        .join(""),
    ).toBe(before);
    expect(
      parts
        .filter((p) => p.op !== "del")
        .map((p) => p.text)
        .join(""),
    ).toBe(after);
  });
});
