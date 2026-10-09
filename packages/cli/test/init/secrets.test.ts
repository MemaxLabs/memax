// The local secret scan: secretlint's preset over the file, the server's
// refusal patterns over each statement, both before anything is sent.
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { describe, expect, it } from "vitest";
import {
  ruleName,
  scanSecrets,
  secretsIn,
  serverRefusals,
} from "../../src/lib/init/secrets.js";

const corpus = JSON.parse(
  readFileSync(
    join(
      import.meta.dirname,
      "..",
      "..",
      "..",
      "server",
      "internal",
      "secrets",
      "testdata",
      "credentials.json",
    ),
    "utf8",
  ),
) as { cases: Array<{ text: string; secret: boolean }> };

describe("the server's refusal patterns, ported", () => {
  it("agree with the server on the shared corpus", () => {
    expect(corpus.cases.length).toBeGreaterThanOrEqual(10);
    for (const c of corpus.cases) {
      expect({
        text: c.text,
        secret: serverRefusals(c.text).length > 0,
      }).toEqual({ text: c.text, secret: c.secret });
    }
  });
});

describe("scanSecrets", () => {
  it("finds vendor tokens secretlint knows, by line, and names the rule, not the value", async () => {
    const token = "ghp_" + "a1B2c3D4e5F6g7H8i9J0k1L2m3N4o5P6q7R8";
    const content = [
      "# Deploy",
      "- Use the staging cluster.",
      `- The bot token is ${token}.`,
      "- Postgres: postgres://app:supersecret@db.internal:5432/app",
    ].join("\n");
    const hits = await scanSecrets(content, "CLAUDE.md");
    const lines = [...new Set(hits.map((h) => h.line))].sort();
    expect(lines).toEqual([3, 4]);
    expect(hits.find((h) => h.line === 3)?.rule).toMatch(
      /GitHub token|vendor API key/,
    );
    expect(hits.find((h) => h.line === 4)?.rule).toBe("database password");
    expect(JSON.stringify(hits)).not.toContain(token);
  });

  it("marks a private key's whole block", async () => {
    const content = [
      "- keys:",
      "-----BEGIN RSA PRIVATE KEY-----",
      "MIIEowIBAAKCAQEA",
      "-----END RSA PRIVATE KEY-----",
      "- done",
    ].join("\n");
    const lines = new Set(
      (await scanSecrets(content, "x.md")).map((h) => h.line),
    );
    expect([...lines].sort()).toEqual([2, 3, 4]);
  });

  it("finds nothing in an ordinary agent file", async () => {
    const content =
      "# Acme\n- Run tests with `pnpm test`.\n- API keys live in 1Password.\n";
    expect(await scanSecrets(content, "AGENTS.md")).toEqual([]);
  });
});

describe("secretsIn", () => {
  it("covers a statement's lines and its joined words", () => {
    const hits = [{ line: 5, rule: "GitHub token" }];
    expect(secretsIn(hits, 4, 6, "harmless")).toEqual(["GitHub token"]);
    expect(secretsIn(hits, 1, 2, "harmless")).toEqual([]);
    // Words split across continuation lines are caught once joined.
    expect(secretsIn([], 1, 2, "api_key = 0123456789abcdef0123")).toEqual([
      "credential assignment",
    ]);
  });

  it("names secretlint's rules for people", () => {
    expect(ruleName("@secretlint/secretlint-rule-github")).toBe("GitHub token");
    expect(ruleName("@secretlint/secretlint-rule-new-vendor")).toBe(
      "new vendor credential",
    );
  });
});
