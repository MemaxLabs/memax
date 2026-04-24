import { join } from "node:path";
import { describe, expect, it } from "vitest";
import {
  classifyAgentConfigPlacement,
  resolveAgentConfigWritePath,
} from "./agent-configs.js";

describe("resolveAgentConfigWritePath", () => {
  const cwd = "/workspaces/memax";
  const home = "/home/tester";
  const scope = "project:github.com/memaxlabs/memax";

  it("resolves Claude project config into .claude", () => {
    expect(
      resolveAgentConfigWritePath("claude-code", "CLAUDE.md", scope, {
        cwd,
        home,
        currentProjectScope: scope,
      }),
    ).toBe(join(cwd, ".claude", "CLAUDE.md"));
  });

  it("resolves Codex project config into .codex", () => {
    expect(
      resolveAgentConfigWritePath("codex", "instructions.md", scope, {
        cwd,
        home,
        currentProjectScope: scope,
      }),
    ).toBe(join(cwd, ".codex", "instructions.md"));
  });

  it("resolves Copilot project config into .github", () => {
    expect(
      resolveAgentConfigWritePath("copilot", "copilot-instructions.md", scope, {
        cwd,
        home,
        currentProjectScope: scope,
      }),
    ).toBe(join(cwd, ".github", "copilot-instructions.md"));
  });

  it("resolves OpenCode project files into .opencode", () => {
    expect(
      resolveAgentConfigWritePath("opencode", "identity.md", scope, {
        cwd,
        home,
        currentProjectScope: scope,
      }),
    ).toBe(join(cwd, ".opencode", "identity.md"));
  });

  it("resolves Claude per-project memory files via project dir locator", () => {
    expect(
      resolveAgentConfigWritePath("claude-code", "memory/feedback.md", scope, {
        cwd,
        home,
        currentProjectScope: scope,
        findClaudeProjectDir: () =>
          "/home/tester/.claude/projects/-workspaces-memax",
      }),
    ).toBe(
      "/home/tester/.claude/projects/-workspaces-memax/memory/feedback.md",
    );
  });

  it("refuses to resolve project-scoped files for a different project", () => {
    expect(
      resolveAgentConfigWritePath("codex", "instructions.md", scope, {
        cwd,
        home,
        currentProjectScope: "project:github.com/other/repo",
      }),
    ).toBeNull();
  });

  it("resolves global Claude config under ~/.claude", () => {
    expect(
      resolveAgentConfigWritePath("claude-code", "CLAUDE.md", "global", {
        cwd,
        home,
        currentProjectScope: scope,
      }),
    ).toBe(join(home, ".claude", "CLAUDE.md"));
  });
});

describe("classifyAgentConfigPlacement", () => {
  const cwd = "/workspaces/memax";
  const home = "/home/tester";
  const scope = "project:github.com/memaxlabs/memax";

  it("reports present when a local config already exists", () => {
    const result = classifyAgentConfigPlacement(
      "codex",
      "instructions.md",
      scope,
      {
        cwd,
        home,
        currentProjectScope: scope,
        localByKey: new Map([
          [
            `codex|instructions.md|${scope}`,
            {
              agent: "codex",
              label: "./.codex/instructions.md",
              path: join(cwd, ".codex", "instructions.md"),
              filePath: "instructions.md",
              scope,
            },
          ],
        ]),
      },
    );

    expect(result).toEqual({
      kind: "present",
      path: join(cwd, ".codex", "instructions.md"),
      reason: "present locally",
    });
  });

  it("reports restorable when there is a safe write path on this machine", () => {
    const result = classifyAgentConfigPlacement(
      "codex",
      "instructions.md",
      scope,
      {
        cwd,
        home,
        currentProjectScope: scope,
      },
    );

    expect(result).toEqual({
      kind: "restorable",
      path: join(cwd, ".codex", "instructions.md"),
      reason: "safe restore path available",
    });
  });

  it("reports different project for mismatched project scopes", () => {
    const result = classifyAgentConfigPlacement(
      "codex",
      "instructions.md",
      scope,
      {
        cwd,
        home,
        currentProjectScope: "project:github.com/other/repo",
      },
    );

    expect(result).toEqual({
      kind: "different_project",
      reason: "belongs to github.com/memaxlabs/memax",
    });
  });

  it("reports unresolved when no safe path can be derived", () => {
    const result = classifyAgentConfigPlacement(
      "unknown-agent",
      "instructions.md",
      "global",
      {
        cwd,
        home,
        currentProjectScope: scope,
      },
    );

    expect(result).toEqual({
      kind: "unresolved",
      reason: "no safe restore path on this machine",
    });
  });
});
