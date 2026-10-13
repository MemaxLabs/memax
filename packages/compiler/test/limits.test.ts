import { describe, expect, it } from "vitest";
import {
  compile,
  DEFAULT_BUDGET,
  extractManagedBlock,
  HOME_BUDGET,
  MANAGED_END,
  MANAGED_START,
  type Memory,
  type TargetSettings,
} from "../src/index.js";
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

describe("the 6 KiB budget of a personal space's files", () => {
  const personal = (memories: Memory[], targets: TargetSettings[]) => {
    const i = input(memories, targets);
    i.space = { ...i.space, slug: "ana", kind: "personal" };
    return i;
  };
  const home: TargetSettings[] = [
    { kind: "agents_md", path: ".memax/personal/AGENTS.md" },
    { kind: "claude_user" },
    { kind: "codex_user", current: "" },
  ];
  const utf8 = (s: string) => new TextEncoder().encode(s).length;
  /** A block's budget covers its lines; the two marker lines come on top. */
  const markers = utf8(`${MANAGED_START}\n${MANAGED_END}\n`);

  it("keeps every home file to 6 KiB by default and lists what was dropped", () => {
    const result = compile(personal(facts(200, 80), home));
    expect(HOME_BUDGET).toBe(6 * 1024);
    expect(result.files).toHaveLength(3);
    for (const f of result.files) {
      const body = f.user_owned
        ? utf8(extractManagedBlock(f.content) ?? "") - markers
        : f.bytes;
      expect(body, f.path).toBeLessThanOrEqual(6 * 1024);
      expect(body, f.path).toBeGreaterThan(6 * 1024 - 100);
      expect(f.dropped_for_budget.length, f.path).toBeGreaterThan(0);
      expect(f.refs.length + f.dropped_for_budget.length, f.path).toBe(200);
    }
    const rules = result.files.find((f) => f.target === "claude_user");
    expect(result.warnings.map((w) => w.message)).toContain(
      `${rules?.dropped_for_budget.length} facts didn't fit the 6 KiB budget for .claude/rules/memax-personal.md. The rest stay live over MCP.`,
    );
  });

  it("drops each section's least-read facts first, sharing the budget across sections", () => {
    const sections = ["preferences", "conventions", "decisions"];
    const spread = facts(200, 80).map((m, i) => ({
      ...m,
      section: sections[i % 3],
    }));
    const rules = compile(personal(spread, [{ kind: "claude_user" }])).files[0];
    const n = (r: string) => Number(r.slice(2));
    sections.forEach((_, s) => {
      const inSection = (r: string) => (n(r) - 1) % 3 === s;
      const kept = rules.refs.filter(inSection).map(n);
      const dropped = rules.dropped_for_budget.filter(inSection).map(n);
      // Higher refs are read less (facts() scores them n - i).
      expect(kept.length).toBeGreaterThan(15);
      expect(Math.max(...kept)).toBeLessThan(Math.min(...dropped));
    });
    for (const heading of ["Preferences", "Conventions", "Decisions"])
      expect(rules.content).toContain(`## ${heading}`);
  });

  it("takes a budget from 1 KiB up, while a project's AGENTS.md keeps 32 KiB", () => {
    for (const budget of [1024, 4096, 16 * 1024]) {
      const rules = compile(
        personal(facts(400, 80), [
          { kind: "claude_user", size_budget: budget },
        ]),
      ).files[0];
      expect(rules.bytes).toBeLessThanOrEqual(budget);
      expect(rules.bytes + 100).toBeGreaterThan(budget);
    }
    expect(compile(input(facts(400, 80))).files[0].bytes).toBeGreaterThan(
      31 * 1024,
    );
  });

  it("doesn't count the person's own lines around a block against it", () => {
    const mine = `# Mine\n\n${"My own line.\n".repeat(800)}`;
    const target = (current: string): TargetSettings[] => [
      { kind: "codex_user", current },
    ];
    const alone = compile(personal(facts(200, 80), target(""))).files[0];
    const there = compile(personal(facts(200, 80), target(mine))).files[0];
    expect(there.refs).toEqual(alone.refs);
    expect(there.drift_sha256).toBe(alone.drift_sha256);
  });
});
