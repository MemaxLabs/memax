import { existsSync, readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";
import { en } from "@/i18n/locales/en";
import { zh } from "@/i18n/locales/zh";
import { DOCS_TOOLS, DOCS_TREE } from "./docs";

// The Docs page links only to pages the docs site has, and lists every
// MCP tool the server serves (packages/server/internal/handler/
// mcp_tools.json, which the CLI's catalogue is held to as well).

const packages = path.resolve(
  path.dirname(fileURLToPath(import.meta.url)),
  "../../../../../..",
);
const docsContent = path.join(packages, "docs-site/content/docs");
const catalogue = JSON.parse(
  readFileSync(
    path.join(packages, "server/internal/handler/mcp_tools.json"),
    "utf8",
  ),
) as { profiles: { agent: { tools: { name: string }[] } } };

function isDocsPage(urlPath: string): boolean {
  const base = path.join(docsContent, ...urlPath.split("/").filter(Boolean));
  return existsSync(`${base}.mdx`) || existsSync(path.join(base, "index.mdx"));
}

describe("the Docs page", () => {
  it("links to pages the docs site has", () => {
    const paths = DOCS_TREE.flatMap((g) => g.pages)
      .map((p) => p.path)
      .filter((p): p is string => p !== null);
    expect(paths.length).toBeGreaterThan(10);
    expect(paths.filter((p) => !isDocsPage(p))).toEqual([]);
    expect(isDocsPage("/cli/init")).toBe(true); // its Next link
    expect(isDocsPage("/concepts/security")).toBe(true); // Security's
  });

  it("lists every MCP tool, once", () => {
    const served = catalogue.profiles.agent.tools.map((t) => t.name);
    expect([...DOCS_TOOLS].sort()).toEqual([...served].sort());
  });

  it("says what each tool does, in both languages", () => {
    for (const words of [en, zh]) {
      for (const tool of DOCS_TOOLS) {
        expect(words.ledger.public.docs.tools[tool], tool).toBeTruthy();
      }
    }
  });
});
