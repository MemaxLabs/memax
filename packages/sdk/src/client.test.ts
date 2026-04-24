import { describe, expect, it } from "vitest";
import { Memax } from "./client.js";

describe("Memax client resources", () => {
  it("exposes shared API resources", () => {
    const client = new Memax({
      apiUrl: "https://api.memax.test",
      fetch: async () =>
        new Response(JSON.stringify({ data: {} }), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        }),
    });

    expect(client.memories).toBeDefined();
    expect(client.configs).toBeDefined();
    expect(client.agentSessions).toBeDefined();
    expect(client.uploads).toBeDefined();
    expect(client.account).toBeDefined();
    expect(client.dreams).toBeDefined();
    expect(client.notifications).toBeDefined();
    expect(client.agents).toBeDefined();
    expect(client.invites).toBeDefined();
  });

  it("builds provider auth URLs", () => {
    const client = new Memax({
      apiUrl: "https://api.memax.test/",
      fetch: async () => new Response(JSON.stringify({ data: {} })),
    });

    expect(client.auth.providerLoginURL("github", "http://localhost/cb")).toBe(
      "https://api.memax.test/v1/auth/github?redirect_uri=http%3A%2F%2Flocalhost%2Fcb",
    );
    expect(client.auth.providerLoginURL("google", "http://localhost/cb")).toBe(
      "https://api.memax.test/v1/auth/google?redirect_uri=http%3A%2F%2Flocalhost%2Fcb",
    );
    expect(
      client.auth.linkProviderURL("google", "https://memax.app/settings"),
    ).toBe(
      "https://api.memax.test/v1/auth/link/google?redirect_uri=https%3A%2F%2Fmemax.app%2Fsettings",
    );
  });

  it("lists and unlinks auth identities", async () => {
    const calls: Array<[string, RequestInit | undefined]> = [];
    const client = new Memax({
      apiUrl: "https://api.memax.test",
      fetch: async (input, init) => {
        calls.push([String(input), init]);
        if (String(input).endsWith("/v1/auth/identities")) {
          return new Response(
            JSON.stringify({
              data: [
                {
                  id: "identity-1",
                  user_id: "user-1",
                  provider: "google",
                  provider_id: "google-1",
                  provider_email: "user@example.com",
                  provider_name: "User",
                  created_at: "2026-04-13T00:00:00Z",
                },
              ],
            }),
          );
        }
        return new Response(JSON.stringify({ data: { status: "unlinked" } }));
      },
    });

    await expect(client.auth.listIdentities()).resolves.toHaveLength(1);
    await expect(client.auth.unlinkProvider("google")).resolves.toEqual({
      status: "unlinked",
    });
    expect(calls.map(([url, init]) => [url, init?.method])).toEqual([
      ["https://api.memax.test/v1/auth/identities", "GET"],
      ["https://api.memax.test/v1/auth/link/google", "DELETE"],
    ]);
  });

  it("sends notifications hub scope as a query param on list", async () => {
    const calls: Array<[string, RequestInit | undefined]> = [];
    const client = new Memax({
      apiUrl: "https://api.memax.test",
      fetch: async (input, init) => {
        calls.push([String(input), init]);
        return new Response(
          JSON.stringify({
            data: {
              notifications: [],
              has_more: false,
            },
          }),
          { headers: { "Content-Type": "application/json" } },
        );
      },
    });

    await client.notifications.list({ hub: "hub-alpha", status: "pending" });

    expect(calls).toHaveLength(1);
    expect(calls[0][0]).toBe(
      "https://api.memax.test/v1/notifications?hub=hub-alpha&status=pending",
    );
    const headers = new Headers(calls[0][1]?.headers);
    expect(headers.get("X-Hub-ID")).toBeNull();
  });

  it("sends notifications hub scope as a query param on summary", async () => {
    const calls: Array<[string, RequestInit | undefined]> = [];
    const client = new Memax({
      apiUrl: "https://api.memax.test",
      fetch: async (input, init) => {
        calls.push([String(input), init]);
        return new Response(
          JSON.stringify({
            data: {
              needs_action_pending: 0,
              updates_pending: 0,
              updates_unseen: 0,
              by_kind: {},
            },
          }),
          { headers: { "Content-Type": "application/json" } },
        );
      },
    });

    await client.notifications.summary("hub-beta");

    expect(calls).toHaveLength(1);
    expect(calls[0][0]).toBe(
      "https://api.memax.test/v1/notifications/summary?hub=hub-beta",
    );
    const headers = new Headers(calls[0][1]?.headers);
    expect(headers.get("X-Hub-ID")).toBeNull();
  });

  // ── Batch result normalization (Bug 2 regression) ────────────────────────
  //
  // The SDK guarantees `skipped` is always a real array on BatchMoveResult /
  // BatchDeleteResult, even if the server response omits the field, encodes
  // an empty slice as JSON null, or sends only the count field. This matches
  // the compile-time type contract (`skipped: BatchMoveSkippedMemory[]`) and
  // lets callers read `.skipped.length` / `.skipped.map(...)` without a
  // defensive guard at every callsite.
  //
  // Caught live: Derek's drag-to-topic move crashed with
  // "Cannot read properties of undefined (reading 'length')" at
  // use-memory-move.ts:759 when the response arrived without a `skipped`
  // field. The fix normalizes at the SDK boundary so every downstream
  // consumer benefits.

  it("normalizes batchMove response when skipped is missing", async () => {
    const client = new Memax({
      apiUrl: "https://api.memax.test",
      fetch: async () =>
        new Response(JSON.stringify({ data: { moved: 3 } }), {
          headers: { "Content-Type": "application/json" },
        }),
    });

    const result = await client.memories.batchMove(["a", "b", "c"], {
      topicId: "topic-1",
    });
    expect(result.moved).toBe(3);
    expect(result.skipped).toEqual([]);
    expect(Array.isArray(result.skipped)).toBe(true);
  });

  it("normalizes batchMove response when skipped is JSON null", async () => {
    const client = new Memax({
      apiUrl: "https://api.memax.test",
      fetch: async () =>
        new Response(JSON.stringify({ data: { moved: 1, skipped: null } }), {
          headers: { "Content-Type": "application/json" },
        }),
    });

    const result = await client.memories.batchMove(["a"], {
      topicId: "topic-1",
    });
    expect(result.moved).toBe(1);
    expect(result.skipped).toEqual([]);
  });

  it("preserves batchMove skipped entries when the server populates them", async () => {
    const client = new Memax({
      apiUrl: "https://api.memax.test",
      fetch: async () =>
        new Response(
          JSON.stringify({
            data: {
              moved: 2,
              skipped: [
                { id: "a", reason: "not_owned" },
                { id: "b", reason: "already_at_target" },
              ],
            },
          }),
          { headers: { "Content-Type": "application/json" } },
        ),
    });

    const result = await client.memories.batchMove(["a", "b", "c", "d"], {
      topicId: "topic-1",
    });
    expect(result.moved).toBe(2);
    expect(result.skipped).toHaveLength(2);
    expect(result.skipped[0]).toEqual({ id: "a", reason: "not_owned" });
    expect(result.skipped[1]).toEqual({
      id: "b",
      reason: "already_at_target",
    });
  });

  it("normalizes batchMove response when moved is missing", async () => {
    const client = new Memax({
      apiUrl: "https://api.memax.test",
      fetch: async () =>
        new Response(JSON.stringify({ data: {} }), {
          headers: { "Content-Type": "application/json" },
        }),
    });

    const result = await client.memories.batchMove(["a"], {
      topicId: "topic-1",
    });
    expect(result.moved).toBe(0);
    expect(result.skipped).toEqual([]);
  });

  it("normalizes batchDelete response when skipped is missing", async () => {
    const client = new Memax({
      apiUrl: "https://api.memax.test",
      fetch: async () =>
        new Response(JSON.stringify({ data: { deleted: 5 } }), {
          headers: { "Content-Type": "application/json" },
        }),
    });

    const result = await client.memories.batchDelete(["a", "b", "c", "d", "e"]);
    expect(result.deleted).toBe(5);
    expect(result.skipped).toEqual([]);
    expect(Array.isArray(result.skipped)).toBe(true);
  });

  it("preserves batchDelete skipped entries when the server populates them", async () => {
    const client = new Memax({
      apiUrl: "https://api.memax.test",
      fetch: async () =>
        new Response(
          JSON.stringify({
            data: {
              deleted: 1,
              skipped: [
                { id: "b", reason: "not_owned" },
                { id: "c", reason: "not_found" },
              ],
            },
          }),
          { headers: { "Content-Type": "application/json" } },
        ),
    });

    const result = await client.memories.batchDelete(["a", "b", "c"]);
    expect(result.deleted).toBe(1);
    expect(result.skipped).toHaveLength(2);
    expect(result.skipped.map((s) => s.reason)).toEqual([
      "not_owned",
      "not_found",
    ]);
  });

  // ── Agent config batch-delete normalization ─────────────────────────────
  //
  // `configs.batchDelete` is a greenfield endpoint introduced in the agent
  // delete hardening work. It shares the normalization contract with
  // `memories.batchDelete`: `skipped` is always a real array, `deleted` is
  // always a number, even when the server response is partial. The web
  // hook's rollback rule (throw on full-skip with real failures) depends
  // on this shape — if the SDK leaked `undefined` through, the hook's
  // `result.skipped.filter(...)` would crash before reaching the throw.

  it("normalizes configs.batchDelete response when skipped is missing", async () => {
    const client = new Memax({
      apiUrl: "https://api.memax.test",
      fetch: async () =>
        new Response(JSON.stringify({ data: { deleted: 3 } }), {
          headers: { "Content-Type": "application/json" },
        }),
    });

    const result = await client.configs.batchDelete([
      "cfg-1",
      "cfg-2",
      "cfg-3",
    ]);
    expect(result.deleted).toBe(3);
    expect(result.skipped).toEqual([]);
    expect(Array.isArray(result.skipped)).toBe(true);
  });

  it("normalizes configs.batchDelete response when skipped is JSON null", async () => {
    const client = new Memax({
      apiUrl: "https://api.memax.test",
      fetch: async () =>
        new Response(JSON.stringify({ data: { deleted: 1, skipped: null } }), {
          headers: { "Content-Type": "application/json" },
        }),
    });

    const result = await client.configs.batchDelete(["cfg-1"]);
    expect(result.deleted).toBe(1);
    expect(result.skipped).toEqual([]);
  });

  it("preserves configs.batchDelete skipped entries when the server populates them", async () => {
    const client = new Memax({
      apiUrl: "https://api.memax.test",
      fetch: async () =>
        new Response(
          JSON.stringify({
            data: {
              deleted: 1,
              skipped: [
                { id: "cfg-2", reason: "not_found" },
                { id: "cfg-3", reason: "delete_failed" },
              ],
            },
          }),
          { headers: { "Content-Type": "application/json" } },
        ),
    });

    const result = await client.configs.batchDelete([
      "cfg-1",
      "cfg-2",
      "cfg-3",
    ]);
    expect(result.deleted).toBe(1);
    expect(result.skipped).toHaveLength(2);
    expect(result.skipped.map((s) => s.reason)).toEqual([
      "not_found",
      "delete_failed",
    ]);
  });

  it("normalizes configs.batchDelete response when deleted is missing", async () => {
    const client = new Memax({
      apiUrl: "https://api.memax.test",
      fetch: async () =>
        new Response(JSON.stringify({ data: {} }), {
          headers: { "Content-Type": "application/json" },
        }),
    });

    const result = await client.configs.batchDelete(["cfg-1"]);
    expect(result.deleted).toBe(0);
    expect(result.skipped).toEqual([]);
  });
});
