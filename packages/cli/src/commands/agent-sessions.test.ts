import { existsSync, readFileSync, rmSync } from "node:fs";
import { join } from "node:path";
import { describe, expect, it } from "vitest";
import {
  classifyAgentSessionPlacement,
  computeSessionSyncHash,
  findShadowedGlobalSessions,
  hashPortableSessionContent,
  isLegacyGlobalSessionShadowed,
  materializeAgentSessionContent,
  resolveAgentSessionWritePath,
} from "./agent-sessions.js";

describe("resolveAgentSessionWritePath", () => {
  const cwd = "/workspaces/memax";
  const home = "/home/tester";
  const scope = "project:github.com/memaxlabs/memax";

  it("resolves global Codex history", () => {
    expect(
      resolveAgentSessionWritePath("codex", "history.jsonl", "global", {
        cwd,
        home,
        currentProjectScope: scope,
      }),
    ).toBe(join(home, ".codex", "history.jsonl"));
  });

  it("resolves global Codex sessions", () => {
    expect(
      resolveAgentSessionWritePath(
        "codex",
        "sessions/2026/04/07/example.jsonl",
        "global",
        {
          cwd,
          home,
          currentProjectScope: scope,
        },
      ),
    ).toBe(join(home, ".codex", "sessions", "2026/04/07/example.jsonl"));
  });

  it("refuses project-scoped writes for a different project", () => {
    expect(
      resolveAgentSessionWritePath("gemini", "chats/session.json", scope, {
        cwd,
        home,
        currentProjectScope: "project:github.com/other/repo",
      }),
    ).toBeNull();
  });

  it("resolves project-scoped Codex sessions into ~/.codex", () => {
    expect(
      resolveAgentSessionWritePath(
        "codex",
        "sessions/2026/04/07/example.jsonl",
        scope,
        {
          cwd,
          home,
          currentProjectScope: scope,
        },
      ),
    ).toBe(join(home, ".codex", "sessions", "2026/04/07/example.jsonl"));
  });
});

describe("classifyAgentSessionPlacement", () => {
  const cwd = "/workspaces/memax";
  const home = "/home/tester";
  const scope = "project:github.com/memaxlabs/memax";

  it("reports present when a local session already exists", () => {
    const result = classifyAgentSessionPlacement(
      "codex",
      "history.jsonl",
      "global",
      {
        cwd,
        home,
        currentProjectScope: scope,
        localByKey: new Map([
          [
            "codex|history.jsonl|global",
            {
              agent: "codex",
              path: join(home, ".codex", "history.jsonl"),
              filePath: "history.jsonl",
              scope: "global",
              sessionType: "history",
            },
          ],
        ]),
      },
    );

    expect(result).toEqual({
      kind: "present",
      path: join(home, ".codex", "history.jsonl"),
      reason: "present locally",
    });
  });

  it("reports restorable for a known safe path", () => {
    const result = classifyAgentSessionPlacement(
      "codex",
      "history.jsonl",
      "global",
      {
        cwd,
        home,
        currentProjectScope: scope,
      },
    );

    expect(result).toEqual({
      kind: "restorable",
      path: join(home, ".codex", "history.jsonl"),
      reason: "safe restore path available",
    });
  });

  it("reports different project for mismatched project scopes", () => {
    const result = classifyAgentSessionPlacement(
      "gemini",
      "chats/session.json",
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
});

describe("portable session hashing", () => {
  it("treats Codex cwd rewrites as the same logical session", () => {
    const first = Buffer.from(
      `${JSON.stringify({
        type: "session_meta",
        payload: { cwd: "/workspaces/memax" },
      })}\n`,
      "utf-8",
    );
    const second = Buffer.from(
      `${JSON.stringify({
        type: "session_meta",
        payload: { cwd: "/Users/alice/src/memax" },
      })}\n`,
      "utf-8",
    );

    expect(
      hashPortableSessionContent("codex", first, "/workspaces/memax"),
    ).toBe(
      hashPortableSessionContent("codex", second, "/Users/alice/src/memax"),
    );
  });
});

describe("materializeAgentSessionContent", () => {
  it("rewrites structured cwd fields for Codex restores", () => {
    const content = Buffer.from(
      `${JSON.stringify({
        type: "session_meta",
        payload: { cwd: "/workspaces/memax" },
      })}\n`,
      "utf-8",
    );

    const restored = materializeAgentSessionContent("codex", content, {
      scope: "project:github.com/memaxlabs/memax",
      currentProjectRootPath: "/Users/alice/src/memax",
      writePath: "/Users/alice/.codex/sessions/example.jsonl",
    }).toString("utf-8");

    expect(restored).toContain("/Users/alice/src/memax");
    expect(restored).not.toContain("/workspaces/memax");
  });

  it("writes Gemini project_root markers on restore", () => {
    const content = Buffer.from('{"sessionId":"1"}\n', "utf-8");
    const root = join("/tmp", "memax-agent-session-test");
    const writePath = join(root, "chats", "session.json");
    rmSync(root, { recursive: true, force: true });

    materializeAgentSessionContent("gemini", content, {
      scope: "project:github.com/memaxlabs/memax",
      currentProjectRootPath: "/workspaces/memax",
      writePath,
    });

    const markerPath = join(root, ".project_root");
    expect(existsSync(markerPath)).toBe(true);
    expect(readFileSync(markerPath, "utf-8")).toBe("/workspaces/memax\n");

    rmSync(root, { recursive: true, force: true });
  });
});

describe("session sync convergence", () => {
  it("acks pulled project sessions using the materialized portable hash", () => {
    const downloaded = Buffer.from(
      `${JSON.stringify({
        type: "session_meta",
        payload: { cwd: "/workspaces/memax" },
      })}\n`,
      "utf-8",
    );

    const materialized = materializeAgentSessionContent("codex", downloaded, {
      scope: "project:github.com/memaxlabs/memax",
      currentProjectRootPath: "/Users/alice/src/memax",
      writePath: "/Users/alice/.codex/sessions/example.jsonl",
    });

    expect(
      computeSessionSyncHash(
        "codex",
        "project:github.com/memaxlabs/memax",
        materialized,
        "/Users/alice/src/memax",
      ),
    ).toBe(
      hashPortableSessionContent(
        "codex",
        materialized,
        "/Users/alice/src/memax",
      ),
    );
  });

  it("marks legacy global codex session duplicates as shadowed", () => {
    expect(
      isLegacyGlobalSessionShadowed(
        {
          action: "pull",
          agent: "codex",
          file_path:
            "sessions/2026/04/07/rollout-2026-04-07T05-39-15-019d6673-c077-7371-a3b8-3d98c1c8e7a7.jsonl",
          scope: "global",
          reason: "cloud_only",
        },
        new Set([
          "codex|sessions/2026/04/07/rollout-2026-04-07T05-39-15-019d6673-c077-7371-a3b8-3d98c1c8e7a7.jsonl",
        ]),
      ),
    ).toBe(true);

    expect(
      isLegacyGlobalSessionShadowed(
        {
          action: "delete_local",
          agent: "codex",
          file_path:
            "sessions/2026/04/07/rollout-2026-04-07T05-39-15-019d6673-c077-7371-a3b8-3d98c1c8e7a7.jsonl",
          scope: "global",
          reason: "deleted_everywhere",
        },
        new Set([
          "codex|sessions/2026/04/07/rollout-2026-04-07T05-39-15-019d6673-c077-7371-a3b8-3d98c1c8e7a7.jsonl",
        ]),
      ),
    ).toBe(true);
  });

  it("does not shadow codex history or non-duplicate sessions", () => {
    expect(
      isLegacyGlobalSessionShadowed(
        {
          action: "pull",
          agent: "codex",
          file_path: "history.jsonl",
          scope: "global",
          reason: "cloud_only",
        },
        new Set(["codex|history.jsonl"]),
      ),
    ).toBe(false);

    expect(
      isLegacyGlobalSessionShadowed(
        {
          action: "pull",
          agent: "codex",
          file_path: "sessions/2026/04/07/example.jsonl",
          scope: "global",
          reason: "cloud_only",
        },
        new Set(),
      ),
    ).toBe(false);
  });

  it("finds safe legacy global duplicates by agent and file path", () => {
    const pairs = findShadowedGlobalSessions([
      {
        id: "g1",
        owner_id: "o1",
        agent: "codex",
        file_path: "sessions/2026/04/07/example.jsonl",
        scope: "global",
        session_type: "transcript",
        filename: "example.jsonl",
        content_type: "application/jsonl",
        size_bytes: 10,
        content_hash: "abc",
        version: 1,
        created_at: "2026-04-08T00:00:00Z",
        updated_at: "2026-04-08T00:00:00Z",
      },
      {
        id: "p1",
        owner_id: "o1",
        agent: "codex",
        file_path: "sessions/2026/04/07/example.jsonl",
        scope: "project:github.com/memaxlabs/memax",
        session_type: "transcript",
        filename: "example.jsonl",
        content_type: "application/jsonl",
        size_bytes: 10,
        content_hash: "abc",
        version: 2,
        created_at: "2026-04-08T00:00:00Z",
        updated_at: "2026-04-08T00:00:00Z",
      },
    ]);

    expect(pairs).toHaveLength(1);
    expect(pairs[0]?.global.id).toBe("g1");
    expect(pairs[0]?.project.id).toBe("p1");
  });

  it("ignores codex history and unrelated scopes when finding duplicates", () => {
    const pairs = findShadowedGlobalSessions([
      {
        id: "g1",
        owner_id: "o1",
        agent: "codex",
        file_path: "history.jsonl",
        scope: "global",
        session_type: "history",
        filename: "history.jsonl",
        content_type: "application/jsonl",
        size_bytes: 10,
        content_hash: "abc",
        version: 1,
        created_at: "2026-04-08T00:00:00Z",
        updated_at: "2026-04-08T00:00:00Z",
      },
      {
        id: "p1",
        owner_id: "o1",
        agent: "codex",
        file_path: "history.jsonl",
        scope: "project:github.com/memaxlabs/memax",
        session_type: "history",
        filename: "history.jsonl",
        content_type: "application/jsonl",
        size_bytes: 10,
        content_hash: "abc",
        version: 2,
        created_at: "2026-04-08T00:00:00Z",
        updated_at: "2026-04-08T00:00:00Z",
      },
    ]);

    expect(pairs).toHaveLength(0);
  });
});
