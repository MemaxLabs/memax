import { describe, expect, it } from "vitest";
import { upsertYamlMcpServersBlock } from "./setup-mcp.js";

const ENTRY = {
  command: "memax",
  args: ["mcp", "serve", "--agent", "hermes"],
};

describe("upsertYamlMcpServersBlock", () => {
  it("creates the block in an empty file", () => {
    const out = upsertYamlMcpServersBlock("", ENTRY)!;
    expect(out).toContain("mcp_servers:\n  memax:");
    expect(out).toContain('command: "memax"');
  });

  it("appends the block after existing top-level keys", () => {
    const out = upsertYamlMcpServersBlock("model: hermes-4\n", ENTRY)!;
    expect(out.indexOf("model: hermes-4")).toBeLessThan(
      out.indexOf("mcp_servers:"),
    );
  });

  it("inserts memax without touching other servers", () => {
    const existing = [
      "mcp_servers:",
      "  filesystem:",
      '    command: "npx"',
      '    args: ["-y", "@modelcontextprotocol/server-filesystem"]',
      "",
      "model: hermes-4",
      "",
    ].join("\n");
    const out = upsertYamlMcpServersBlock(existing, ENTRY)!;
    expect(out).toContain("  filesystem:");
    expect(out).toContain("  memax:");
    expect(out).toContain("model: hermes-4");
    expect(out.indexOf("  memax:")).toBeLessThan(out.indexOf("model:"));
  });

  it("replaces an existing memax entry instead of duplicating", () => {
    const existing = [
      "mcp_servers:",
      "  memax:",
      '    command: "old-memax"',
      '    args: ["stale"]',
      "  filesystem:",
      '    command: "npx"',
      "",
    ].join("\n");
    const out = upsertYamlMcpServersBlock(existing, ENTRY)!;
    expect(out.match(/ {2}memax:/g)).toHaveLength(1);
    expect(out).not.toContain("old-memax");
    expect(out).toContain("  filesystem:");
  });

  it("adopts 4-space child indentation from the existing block", () => {
    const existing = [
      "mcp_servers:",
      "    filesystem:",
      '        command: "npx"',
      "    memax:",
      '        command: "old-memax"',
      "",
      "model: hermes-4",
    ].join("\n");
    const out = upsertYamlMcpServersBlock(existing, ENTRY)!;
    // New entry uses the block's 4-space indent; the old entry is removed.
    expect(out).toContain('    memax:\n        command: "memax"');
    expect(out).not.toContain("old-memax");
    expect(out.match(/^ {4}memax:/gm)).toHaveLength(1);
    expect(out).toContain("    filesystem:");
  });

  it("adopts tab indentation from the existing block", () => {
    const existing = [
      "mcp_servers:",
      "\tfilesystem:",
      '\t\tcommand: "npx"',
    ].join("\n");
    const out = upsertYamlMcpServersBlock(existing, ENTRY)!;
    expect(out).toContain('\tmemax:\n\t\tcommand: "memax"');
  });

  it("bails out on flow-style mcp_servers instead of corrupting", () => {
    const out = upsertYamlMcpServersBlock(
      'mcp_servers: { filesystem: { command: "npx" } }\n',
      ENTRY,
    );
    expect(out).toBeNull();
  });

  it("ignores comment lines when adopting child indentation", () => {
    const existing = [
      "mcp_servers:",
      "  # local tools",
      "    filesystem:",
      '        command: "npx"',
      "    memax:",
      '        command: "old-memax"',
    ].join("\n");
    const out = upsertYamlMcpServersBlock(existing, ENTRY)!;
    expect(out.match(/^ {4}memax:/gm)).toHaveLength(1);
    expect(out).not.toContain("old-memax");
    expect(out).toContain("  # local tools");
  });
});
