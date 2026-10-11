import { mkdtempSync, readFileSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { describe, expect, it } from "vitest";
import {
  removeMcpJson,
  setupMcp,
  setupMcpOAuth,
} from "../commands/setup-mcp.js";
import type { AgentDef } from "../commands/setup-types.js";
import { parseJsoncObject, setJsonc } from "./jsonc.js";

describe("JSON settings with comments", () => {
  const mine = [
    "{",
    "\t// the person's note",
    '\t"theme": "dark", /* inline */',
    '\t"list": [1, 2,],',
    "}",
    "",
  ].join("\n");

  it("reads comments and trailing commas, and refuses what isn't an object", () => {
    expect(parseJsoncObject(mine)).toEqual({ theme: "dark", list: [1, 2] });
    expect(parseJsoncObject("")).toEqual({});
    expect(parseJsoncObject("{ not json")).toBeNull();
    expect(parseJsoncObject("[1]")).toBeNull();
  });

  it("sets one value and keeps every other byte, in the file's own indentation", () => {
    const out = setJsonc(
      mine,
      ["context", "fileName"],
      ["GEMINI.md", "AGENTS.md"],
    );
    expect(out.startsWith(mine.slice(0, mine.indexOf('\t"list"')))).toBe(true);
    expect(out).toContain('\t"list": [1, 2,]');
    expect(out).toContain('\t"context": {\n\t\t"fileName": [');
    expect(parseJsoncObject(out)).toMatchObject({
      context: { fileName: ["GEMINI.md", "AGENTS.md"] },
    });
    const back = setJsonc(out, ["context"], undefined);
    expect(back).not.toContain("context");
    expect(back).toContain("\t// the person's note");
    expect(parseJsoncObject(back)).toEqual({ theme: "dark", list: [1, 2] });
  });

  it("keeps Windows line endings and starts an empty file", () => {
    const crlf = '{\r\n  // note\r\n  "a": 1\r\n}\r\n';
    const out = setJsonc(crlf, ["b"], 2);
    expect(out).toContain("// note");
    expect(out.split("\r\n").length).toBeGreaterThan(4);
    expect(out.replace(/\r\n/g, "")).not.toContain("\n");
    expect(JSON.parse(setJsonc("", ["a", "b"], true))).toEqual({
      a: { b: true },
    });
  });
});

describe("the MCP settings writers", () => {
  const agent = (configPath: string): AgentDef => ({
    name: "Gemini CLI",
    id: "gemini",
    configPath,
    format: "json-mcpServers",
    mcpKey: "mcpServers",
    hasHooks: true,
    globalInstructionFile: null,
    detect: () => true,
    remoteEntry: (url) => ({ httpUrl: url }),
  });
  const file = (content: string) => {
    const p = join(
      mkdtempSync(join(tmpdir(), "memax-jsonc-")),
      "settings.json",
    );
    writeFileSync(p, content);
    return p;
  };
  const commented = [
    "{",
    "  // my servers",
    '  "mcpServers": {',
    '    "other": { "command": "x" }, // keep me',
    "  },",
    "}",
    "",
  ].join("\n");

  it("add Memax beside the person's servers without dropping a comment", () => {
    const p = file(commented);
    setupMcpOAuth(agent(p));
    const out = readFileSync(p, "utf8");
    expect(out).toContain("  // my servers");
    expect(out).toContain('"other": { "command": "x" }, // keep me');
    expect(parseJsoncObject(out)?.mcpServers).toMatchObject({
      other: { command: "x" },
      memax: { httpUrl: expect.stringMatching(/\/mcp$/) },
    });
    setupMcp(agent(p), { command: "memax", args: [], shell: "" });
    expect(readFileSync(p, "utf8")).toContain("// keep me");
    expect(removeMcpJson(agent(p))).toBe(true);
    const removed = readFileSync(p, "utf8");
    expect(removed).toContain("// keep me");
    // Memax's entry and the comma before it go; the comment stays.
    expect(removed).toBe(
      [
        "{",
        "  // my servers",
        '  "mcpServers": {',
        '    "other": { "command": "x" } // keep me',
        "  },",
        "}",
        "",
      ].join("\n"),
    );
  });

  it("leave a file that isn't JSON as it is, and say so", () => {
    const p = file("{ not json");
    expect(() => setupMcpOAuth(agent(p))).toThrow(/isn't valid JSON/);
    expect(readFileSync(p, "utf8")).toBe("{ not json");
  });
});
