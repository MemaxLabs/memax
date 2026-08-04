import { describe, expect, it } from "vitest";
import { upsertYamlMcpServersBlock } from "./setup-mcp.js";

const ENTRY = [
  "  memax:",
  '    command: "memax"',
  '    args: ["mcp", "serve", "--agent", "hermes"]',
];

describe("upsertYamlMcpServersBlock", () => {
  it("creates the block in an empty file", () => {
    const out = upsertYamlMcpServersBlock("", ENTRY);
    expect(out).toContain("mcp_servers:\n  memax:");
    expect(out).toContain('command: "memax"');
  });

  it("appends the block after existing top-level keys", () => {
    const out = upsertYamlMcpServersBlock("model: hermes-4\n", ENTRY);
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
    const out = upsertYamlMcpServersBlock(existing, ENTRY);
    expect(out).toContain("  filesystem:");
    expect(out).toContain("  memax:");
    expect(out).toContain("model: hermes-4");
    // memax inserted inside the block, before the next root key
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
    const out = upsertYamlMcpServersBlock(existing, ENTRY);
    expect(out.match(/ {2}memax:/g)).toHaveLength(1);
    expect(out).not.toContain("old-memax");
    expect(out).toContain("  filesystem:");
  });
});
