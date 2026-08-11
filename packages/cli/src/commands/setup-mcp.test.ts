import { describe, expect, it } from "vitest";
import { mkdtempSync, readFileSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import {
  codexMcpTomlSection,
  parseCodexAuthStatus,
  upsertMemaxTomlSection,
  upsertYamlMcpServersBlock,
} from "./setup-mcp.js";
import type { AgentDef } from "./setup-types.js";

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

describe("codexMcpTomlSection", () => {
  const URL = "https://api.memax.app/mcp";

  it("renders a url-only section in OAuth mode", () => {
    const out = codexMcpTomlSection(URL);
    expect(out).toContain('[mcp_servers.memax]\ntype = "url"\nurl = "' + URL);
    expect(out).not.toContain("http_headers");
  });

  it("puts the API key under http_headers — the only table Codex reads", () => {
    const out = codexMcpTomlSection(URL, "mxk_test");
    expect(out).toContain("[mcp_servers.memax.http_headers]");
    expect(out).toContain('Authorization = "Bearer mxk_test"');
    // A plain `headers` table is silently ignored by Codex.
    expect(out).not.toContain("[mcp_servers.memax.headers]");
  });
});

describe("upsertMemaxTomlSection", () => {
  const tomlAgent = (configPath: string): AgentDef => ({
    name: "Codex CLI",
    id: "codex",
    configPath,
    format: "toml",
    mcpKey: "mcp_servers",
    hasHooks: false,
    globalInstructionFile: null,
    detect: () => true,
    remoteEntry: (url) => ({ type: "url", url }),
  });

  const tmpConfig = (): string =>
    join(mkdtempSync(join(tmpdir(), "memax-setup-")), "config.toml");

  it("creates the config file and parent directory when missing", () => {
    const path = join(
      mkdtempSync(join(tmpdir(), "memax-setup-")),
      "deep",
      "config.toml",
    );
    upsertMemaxTomlSection(
      tomlAgent(path),
      codexMcpTomlSection("https://x/mcp"),
    );
    expect(readFileSync(path, "utf-8")).toContain("[mcp_servers.memax]");
  });

  it("preserves unrelated sections", () => {
    const path = tmpConfig();
    writeFileSync(
      path,
      'model = "o3"\n\n[mcp_servers.other]\nurl = "https://other"\n',
    );
    upsertMemaxTomlSection(
      tomlAgent(path),
      codexMcpTomlSection("https://x/mcp"),
    );
    const out = readFileSync(path, "utf-8");
    expect(out).toContain('model = "o3"');
    expect(out).toContain("[mcp_servers.other]");
    expect(out).toContain("[mcp_servers.memax]");
  });

  it("replaces an existing memax section including stale sub-tables", () => {
    const path = tmpConfig();
    writeFileSync(
      path,
      '[mcp_servers.memax]\ntype = "url"\nurl = "https://old/mcp"\n\n' +
        '[mcp_servers.memax.headers]\nAuthorization = "Bearer mxk_stale"\n\n' +
        '[mcp_servers.other]\nurl = "https://other"\n',
    );
    upsertMemaxTomlSection(
      tomlAgent(path),
      codexMcpTomlSection("https://new/mcp", "mxk_fresh"),
    );
    const out = readFileSync(path, "utf-8");
    expect(out).not.toContain("mxk_stale");
    expect(out).not.toContain("https://old/mcp");
    expect(out).not.toContain("[mcp_servers.memax.headers]");
    expect(out).toContain('url = "https://new/mcp"');
    expect(out).toContain("[mcp_servers.memax.http_headers]");
    expect(out).toContain("[mcp_servers.other]");
    expect(out.match(/\[mcp_servers\.memax\]/g)).toHaveLength(1);
  });
});

describe("parseCodexAuthStatus", () => {
  const entry = (auth: unknown) =>
    JSON.stringify([{ name: "memax", auth_status: auth }]);

  it("maps not_logged_in to needs_login", () => {
    expect(parseCodexAuthStatus(entry("not_logged_in"))).toBe("needs_login");
  });

  it("treats any credentialed status as ok", () => {
    expect(parseCodexAuthStatus(entry("bearer_token"))).toBe("ok");
    expect(parseCodexAuthStatus(entry("oauth"))).toBe("ok");
  });

  it("returns unknown when memax is not configured", () => {
    expect(parseCodexAuthStatus(JSON.stringify([{ name: "other" }]))).toBe(
      "unknown",
    );
  });

  it("returns unknown when auth_status is missing (older Codex)", () => {
    expect(parseCodexAuthStatus(JSON.stringify([{ name: "memax" }]))).toBe(
      "unknown",
    );
    expect(parseCodexAuthStatus(entry(""))).toBe("unknown");
  });

  it("returns unknown on malformed output", () => {
    expect(parseCodexAuthStatus("not json")).toBe("unknown");
    expect(parseCodexAuthStatus('{"name":"memax"}')).toBe("unknown");
  });
});
