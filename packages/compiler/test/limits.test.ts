import { describe, expect, it } from "vitest";
import { compile, DEFAULT_BUDGET, type Memory } from "../src/index.js";
import { input, memory } from "./helpers.js";

/** `n` distinct facts of about `size` characters, most-read first by ref. */
function facts(
  n: number,
  size: number,
  extra: Partial<Memory> = {},
  text = "x",
): Memory[] {
  return Array.from({ length: n }, (_, i) => {
    const ref = `M-${String(i + 1).padStart(4, "0")}`;
    const words =
      `Fact ${i + 1} ${text.repeat(Math.max(1, Math.ceil(size / text.length)))}`.slice(
        0,
        size,
      );
    return memory(ref, `${words}.`, { read_score: n - i, ...extra });
  });
}

describe("Codex's 32 KiB cap on AGENTS.md", () => {
  it("keeps AGENTS.md under 32 KiB by default and lists what was dropped", () => {
    const result = compile(input(facts(400, 120)));
    const agents = result.files[0];
    expect(DEFAULT_BUDGET).toBe(32 * 1024);
    expect(agents.bytes).toBeLessThanOrEqual(32 * 1024);
    expect(agents.bytes).toBeGreaterThan(31 * 1024);
    expect(agents.dropped_for_budget.length).toBeGreaterThan(0);
    expect(agents.refs.length + agents.dropped_for_budget.length).toBe(400);
    expect(result.targets[0].dropped_for_budget).toEqual(
      agents.dropped_for_budget,
    );
  });

  it("drops the least-read facts first", () => {
    const agents = compile(input(facts(400, 120))).files[0];
    const kept = agents.refs.map((r) => Number(r.slice(2)));
    const dropped = agents.dropped_for_budget.map((r) => Number(r.slice(2)));
    expect(Math.max(...kept)).toBeLessThan(Math.min(...dropped));
  });

  it("warns near the cap and when facts were dropped", () => {
    const result = compile(input(facts(400, 120)));
    const messages = result.warnings.map((w) => `${w.code}: ${w.message}`);
    expect(messages).toContainEqual(
      expect.stringMatching(
        /^near_limit: AGENTS\.md at 3\d\.\d KiB is near Codex's 32 KiB cap\.$/,
      ),
    );
    expect(messages).toContainEqual(
      expect.stringMatching(
        /^dropped_for_budget: \d+ facts didn't fit the 32 KiB budget for AGENTS\.md\. The rest stay live over MCP\.$/,
      ),
    );
  });

  it("caps a larger budget at 32 KiB and says so", () => {
    const result = compile(
      input(facts(400, 120), [{ kind: "agents_md", size_budget: 40 * 1024 }]),
    );
    expect(result.files[0].bytes).toBeLessThanOrEqual(32 * 1024);
    expect(result.warnings).toContainEqual({
      code: "budget_capped",
      target: "agents_md",
      path: "AGENTS.md",
      message:
        "The 40 KiB budget for AGENTS.md is over Codex's 32 KiB cap, so it compiles to 32 KiB.",
    });
  });

  it("counts bytes, not characters", () => {
    const wide = compile(input(facts(400, 60, {}, "部署")));
    expect(wide.files[0].bytes).toBeLessThanOrEqual(32 * 1024);
    expect(wide.files[0].content.length).toBeLessThan(wide.files[0].bytes);
  });

  it("doesn't warn when the file is well under the cap", () => {
    const result = compile(input(facts(10, 60)));
    expect(result.warnings).toEqual([]);
  });

  it("fills exactly up to a small budget", () => {
    for (const budget of [1024, 1500, 2048, 4096]) {
      const agents = compile(
        input(facts(80, 50), [{ kind: "agents_md", size_budget: budget }]),
      ).files[0];
      expect(agents.bytes).toBeLessThanOrEqual(budget);
      // Each line is about 63 bytes, so the next one would not have fitted.
      expect(agents.dropped_for_budget.length).toBeGreaterThan(0);
      expect(agents.bytes + 70).toBeGreaterThan(budget);
    }
  });
});

describe("Windsurf's 12,000-character cap per rule file", () => {
  const scoped = { scope: { paths: ["packages/web/**"] } };

  it("stops at 12,000 characters whatever the byte budget", () => {
    const result = compile(
      input(facts(300, 100, scoped), [{ kind: "windsurf" }]),
    );
    const rule = result.files[0];
    expect(rule.path).toBe(".devin/rules/memax-packages-web.md");
    expect(rule.content.length).toBeLessThanOrEqual(12_000);
    expect(rule.content.length).toBeGreaterThan(11_000);
    expect(rule.dropped_for_budget.length).toBeGreaterThan(0);
    expect(result.warnings.map((w) => w.message)).toContain(
      `.devin/rules/memax-packages-web.md at ${rule.content.length.toLocaleString("en-US")} characters is near Windsurf's 12,000-character cap.`,
    );
  });

  it("counts characters, so wide text fits fewer bytes than the byte budget", () => {
    const rule = compile(
      input(facts(300, 40, scoped, "部署"), [{ kind: "windsurf" }]),
    ).files[0];
    expect(rule.content.length).toBeLessThanOrEqual(12_000);
    expect(rule.bytes).toBeGreaterThan(12_000);
  });
});

describe("line guidance", () => {
  it("warns when a Cursor rule passes 500 lines", () => {
    const result = compile(
      input(facts(520, 10, { scope: { paths: ["packages/web/**"] } }), [
        { kind: "cursor_mdc" },
      ]),
    );
    expect(result.warnings.map((w) => w.code)).toContain("over_guidance");
    expect(
      result.warnings.find((w) => w.code === "over_guidance")?.message,
    ).toMatch(
      /^\.cursor\/rules\/memax-packages-web\.mdc has 5\d\d lines; Cursor suggests under 500\.$/,
    );
  });

  it("warns when the CLAUDE.md shim passes 200 lines", () => {
    const result = compile(
      input(facts(210, 10, { scope: { agents: ["claude-code"] } }), [
        { kind: "claude_md" },
      ]),
    );
    expect(
      result.warnings.find((w) => w.code === "over_guidance")?.message,
    ).toMatch(/^CLAUDE\.md has 2\d\d lines; Claude Code suggests under 200\.$/);
  });
});
