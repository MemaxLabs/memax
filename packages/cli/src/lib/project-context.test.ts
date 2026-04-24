import { beforeEach, describe, expect, it, vi } from "vitest";

const { execSync, existsSync, readFileSync } = vi.hoisted(() => ({
  execSync: vi.fn(),
  existsSync: vi.fn(),
  readFileSync: vi.fn(),
}));

vi.mock("node:child_process", () => ({
  execSync,
}));

vi.mock("node:fs", async () => {
  const actual = await vi.importActual<typeof import("node:fs")>("node:fs");
  return {
    ...actual,
    existsSync,
    readFileSync,
  };
});

import {
  getProjectScope,
  normalizeRepoUrl,
  readMemaxYmlConfig,
  readMemaxYmlHub,
  resolveProjectScope,
} from "./project-context.js";

describe("normalizeRepoUrl", () => {
  it("normalizes common git remote forms", () => {
    expect(normalizeRepoUrl("https://github.com/MemaxLabs/memax.git")).toBe(
      "github.com/memaxlabs/memax",
    );
    expect(normalizeRepoUrl("git@github.com:MemaxLabs/memax.git")).toBe(
      "github.com/memaxlabs/memax",
    );
    expect(normalizeRepoUrl("ssh://git@github.com/MemaxLabs/memax")).toBe(
      "github.com/memaxlabs/memax",
    );
  });
});

describe("getProjectScope", () => {
  beforeEach(() => {
    execSync.mockReset();
    existsSync.mockReset();
    readFileSync.mockReset();
    existsSync.mockReturnValue(false);
  });

  it("uses the normalized origin remote when present", () => {
    execSync.mockReturnValueOnce("git@github.com:MemaxLabs/memax.git\n");
    expect(getProjectScope("/workspaces/memax")).toBe(
      "project:github.com/memaxlabs/memax",
    );
  });

  it("returns generic project scope when there is no canonical remote", () => {
    execSync.mockImplementation(() => {
      throw new Error("no remote");
    });
    expect(getProjectScope("/workspaces/memax")).toBe("project");
  });

  it("uses .memax.yml project_id as an explicit override", () => {
    existsSync.mockImplementation((path: string) =>
      path.endsWith(".memax.yml"),
    );
    readFileSync.mockReturnValue("project_id: Acme.Internal/Payments-API\n");

    expect(getProjectScope("/workspaces/memax")).toBe(
      "project:acme.internal/payments-api",
    );
  });

  it("reports when .memax.yml project_id overrides git origin", () => {
    existsSync.mockImplementation((path: string) =>
      path.endsWith(".memax.yml"),
    );
    readFileSync.mockReturnValue("project_id: github.com/acme/override\n");
    execSync.mockReturnValueOnce("git@github.com:MemaxLabs/memax.git\n");

    expect(resolveProjectScope("/workspaces/memax")).toEqual({
      scope: "project:github.com/acme/override",
      source: "memax_yml",
      warning:
        ".memax.yml project_id (github.com/acme/override) overrides git origin (github.com/memaxlabs/memax)",
    });
  });
});

describe("readMemaxYmlConfig", () => {
  beforeEach(() => {
    existsSync.mockReset();
    readFileSync.mockReset();
    existsSync.mockReturnValue(false);
  });

  it("parses hub and project_id from .memax.yml", () => {
    existsSync.mockImplementation((path: string) =>
      path.endsWith(".memax.yml"),
    );
    readFileSync.mockReturnValue(
      "hub: team-backend\nproject_id: github.com/MemaxLabs/memax\n",
    );

    expect(readMemaxYmlConfig("/workspaces/memax")).toEqual({
      hub: "team-backend",
      project_id: "github.com/memaxlabs/memax",
    });
    expect(readMemaxYmlHub()).toBe("team-backend");
  });
});
