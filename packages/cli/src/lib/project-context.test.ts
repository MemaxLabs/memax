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
    expect(normalizeRepoUrl("https://github.com/acme/project.git")).toBe(
      "github.com/acme/project",
    );
    expect(normalizeRepoUrl("git@github.com:acme/project.git")).toBe(
      "github.com/acme/project",
    );
    expect(normalizeRepoUrl("ssh://git@github.com/acme/project")).toBe(
      "github.com/acme/project",
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
    execSync.mockReturnValueOnce("git@github.com:acme/project.git\n");
    expect(getProjectScope("/workspaces/project")).toBe(
      "project:github.com/acme/project",
    );
  });

  it("returns generic project scope when there is no canonical remote", () => {
    execSync.mockImplementation(() => {
      throw new Error("no remote");
    });
    expect(getProjectScope("/workspaces/project")).toBe("project");
  });

  it("uses .memax.yml project_id as an explicit override", () => {
    existsSync.mockImplementation((path: string) =>
      path.endsWith(".memax.yml"),
    );
    readFileSync.mockReturnValue("project_id: Acme.Internal/Payments-API\n");

    expect(getProjectScope("/workspaces/project")).toBe(
      "project:acme.internal/payments-api",
    );
  });

  it("reports when .memax.yml project_id overrides git origin", () => {
    existsSync.mockImplementation((path: string) =>
      path.endsWith(".memax.yml"),
    );
    readFileSync.mockReturnValue("project_id: github.com/acme/override\n");
    execSync.mockReturnValueOnce("git@github.com:acme/project.git\n");

    expect(resolveProjectScope("/workspaces/project")).toEqual({
      scope: "project:github.com/acme/override",
      source: "memax_yml",
      warning:
        ".memax.yml project_id (github.com/acme/override) overrides git origin (github.com/acme/project)",
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
      "hub: team-backend\nproject_id: github.com/acme/project\n",
    );

    expect(readMemaxYmlConfig("/workspaces/project")).toEqual({
      hub: "team-backend",
      project_id: "github.com/acme/project",
    });
    expect(readMemaxYmlHub()).toBe("team-backend");
  });
});
