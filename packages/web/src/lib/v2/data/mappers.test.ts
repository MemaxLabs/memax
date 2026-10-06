import { MemaxError, type V2 } from "memax-sdk";
import { describe, expect, it, vi } from "vitest";
import { createSdkActivity, receiptToEntry } from "./activity-sdk";
import { AgentCommandError, autonomyIn } from "./agents";
import {
  agentsOverview,
  createSdkAgents,
  registryKey,
  toAgentCommandError,
  toAgentWrite,
  toApiKey,
  toConnection,
} from "./agents-sdk";
import { createDemoAgents } from "./agents-demo";
import { DEMO_SPACES } from "./demo-dataset";
import type { Viewer } from "./types";

// The /v2 → view mappers behind the SDK source, and the demo's rules.

const ME = "0192a7c0-0000-7000-8000-0000000000a1";
const viewer: Viewer = {
  id: ME,
  initials: "ZZ",
  name: "Ziyang",
  timeZone: "America/Vancouver",
};

function receipt(over: Partial<V2.Receipt> = {}): V2.Receipt {
  return {
    id: "r1",
    seq: 1,
    tenant_id: "t1",
    space_id: "s1",
    object_kind: "memory",
    object_id: "m1",
    object_ref: "M-0219",
    action: "kept",
    actor_kind: "person",
    actor_id: ME,
    via: "web",
    occurred_at: "2026-10-05T14:31:00-07:00",
    recorded_at: "2026-10-05T14:31:01-07:00",
    stream_id: "m1",
    stream_version: 2,
    ...over,
  };
}

function connection(
  over: Partial<V2.AgentConnection> = {},
): V2.AgentConnection {
  return {
    id: "c1",
    person_id: ME,
    agent: "gemini-cli",
    display_name: "Gemini CLI",
    surface: "cli",
    credential: { kind: "api_key", id: "k1", active: true },
    max_autonomy: "propose",
    state: "active",
    spaces: [
      {
        space_id: "s1",
        slug: "memax-v2",
        name: "memax-v2",
        kind: "project",
        autonomy: "propose",
        reads_7d: 0,
        writes_7d: 4,
        updated_at: "2026-10-01T00:00:00Z",
      },
    ],
    reads_7d: 0,
    writes_7d: 4,
    connected_by_kind: "memax",
    created_receipt_id: "r0",
    last_receipt_id: "r9",
    created_at: "2026-09-02T09:30:00-07:00",
    updated_at: "2026-10-01T00:00:00Z",
    ...over,
  };
}

describe("receiptToEntry", () => {
  it("names the viewer as you, and carries the receipt's fields", () => {
    const entry = receiptToEntry(
      receipt({ session_ref: "7c2f", via: "mcp", reason: "dup" }),
      viewer,
    );
    expect(entry).toMatchObject({
      id: "r1",
      at: "2026-10-05T14:31:00-07:00",
      actor: { kind: "you", initials: "ZZ" },
      action: "kept",
      object: { kind: "memory", ref: "M-0219", id: "m1" },
      via: [
        { kind: "via", via: "mcp" },
        { kind: "session", ref: "7c2f" },
      ],
      rawVia: "mcp",
      session: "7c2f",
      reason: "dup",
    });
    // Receipts never hold words, so there's nothing to quote.
    expect(entry.detail).toBeUndefined();
  });

  it("tells other people, agents and the system apart", () => {
    expect(
      receiptToEntry(receipt({ actor_id: "someone" }), viewer).actor,
    ).toEqual({ kind: "person" });
    expect(
      receiptToEntry(
        receipt({ actor_kind: "agent", actor_id: "c6", agent: "gemini-cli" }),
        viewer,
      ).actor,
    ).toEqual({ kind: "agent", agent: "gemini", connectionId: "c6" });
    const repo = receiptToEntry(receipt({ actor_kind: "repository" }), viewer);
    expect(repo.actor).toEqual({ kind: "repository" });
    expect(repo.via).toEqual([{ kind: "repository" }]);
  });

  it("reads an agent receipt's level from its autonomy source", () => {
    const entry = receiptToEntry(
      receipt({
        object_kind: "agent",
        object_ref: "gemini-cli",
        object_id: "c6",
        action: "autonomy_changed",
        source: { kind: "autonomy", ref: "read" },
      }),
      viewer,
    );
    expect(entry.detail).toEqual({ kind: "autonomy", level: "read" });
    expect(entry.object.ref).toBe("gemini");
    // The level is in the sentence, not the via column.
    expect(entry.via).toEqual([{ kind: "via", via: "web" }]);
  });

  it("pages with the cursor, and serves no totals", async () => {
    const list = vi.fn().mockResolvedValue({
      items: [receipt()],
      has_more: true,
      next_cursor: "abc",
    });
    const activity = createSdkActivity({
      client: { v2: { receipts: { list } } } as never,
      viewer,
    });
    const page = await activity.activity({
      space: DEMO_SPACES[1]!,
      cursor: "prev",
    });
    expect(list).toHaveBeenCalledWith("memax-v2", {
      cursor: "prev",
      limit: 50,
      signal: undefined,
    });
    expect(page.nextCursor).toBe("abc");
    expect(page.totals).toBeNull();
  });
});

describe("toConnection", () => {
  it("maps a connection, with its reads and targets not served", () => {
    const view = toConnection(
      connection({
        reads_7d: 1204,
        spaces: [
          {
            ...connection().spaces[0]!,
            reads_7d: 1180,
          },
        ],
      }),
      viewer,
    );
    expect(view).toMatchObject({
      id: "c1",
      agent: "gemini",
      name: "Gemini CLI",
      credential: { kind: "api_key", active: true },
      maxAutonomy: "propose",
      mine: true,
      connectedBy: "memax",
      reads7d: 1204,
      writes7d: 4,
    });
    expect(view.spaces[0]).toMatchObject({
      autonomy: "propose",
      reads7d: 1180,
    });
    // A 0 is a count now that reads are recorded, not "unknown".
    expect(toConnection(connection(), viewer).reads7d).toBe(0);
    expect(view.target).toBeUndefined();
    expect(toConnection(connection({ person_id: "other" }), viewer).mine).toBe(
      false,
    );
    expect(toConnection(connection(), null).mine).toBe(false);
    expect(registryKey("other", "Aider")).toBe("Aider");
  });

  it("reads one agent's week and sessions, reads included", async () => {
    const get = vi.fn().mockResolvedValue({
      agent: connection({ reads_7d: 512 }),
      this_week: {
        reads: 512,
        writes: 21,
        proposals: 21,
        kept: 14,
        rejected: 3,
        waiting: 4,
      },
      recent_writes: [],
      sessions: [
        {
          session_ref: "9f1c",
          reads: 38,
          writes: 2,
          last_at: "2026-10-05T14:21:00-07:00",
        },
        {
          session_ref: "5b0e",
          reads: 0,
          writes: 4,
          last_at: "2026-09-11T10:00:00-07:00",
        },
      ],
    } satisfies V2.AgentDetail);
    const agents = createSdkAgents({
      client: { v2: { agents: { get } } } as never,
      viewer,
    });
    const detail = await agents.agent("c1");
    expect(detail?.connection.reads7d).toBe(512);
    expect(detail?.week).toMatchObject({ reads: 512, writes: 21, waiting: 4 });
    expect(detail?.sessions).toEqual([
      {
        ref: "9f1c",
        reads: 38,
        writes: 2,
        lastAt: "2026-10-05T14:21:00-07:00",
      },
      { ref: "5b0e", reads: 0, writes: 4, lastAt: "2026-09-11T10:00:00-07:00" },
    ]);
  });

  it("keeps a proposal a person kept, with the session it came from", () => {
    const write = toAgentWrite(
      receipt({ action: "proposed", session_ref: "2d4a", actor_kind: "agent" }),
      { state: "kept", statement: "Workers must be idempotent." } as V2.Memory,
      "codex",
    );
    expect(write).toMatchObject({
      ref: "M-0219",
      statement: "Workers must be idempotent.",
      state: "kept",
      note: { kind: "proposed-in", agent: "codex", session: "2d4a" },
    });
    const gone = toAgentWrite(
      receipt(),
      { state: "forgotten", statement: "" } as V2.Memory,
      "codex",
    );
    expect(gone.statement).toBeNull();
    expect(toAgentWrite(receipt(), null, "codex").statement).toBeNull();
  });
});

describe("toAgentCommandError", () => {
  const refused = (code: string) =>
    new MemaxError("no", "refused", 403, {
      policy: { effect: "refuse", code, message: "English." },
    });
  it.each([
    ["autonomy_needs_web", "needs_web"],
    ["key_max_propose", "key_max_propose"],
    ["not_your_agent", "not_your_agent"],
    ["autonomy_not_allowed", "not_allowed"],
    ["person_must_manage", "person_must_manage"],
    ["something_new", "failed"],
  ])("refused %s → %s", (code, refusal) => {
    expect(toAgentCommandError(refused(code)).refusal).toBe(refusal);
  });

  it("maps the other errors, and anything else to failed", () => {
    expect(
      toAgentCommandError(new MemaxError("x", "surface_unverified", 403))
        .refusal,
    ).toBe("surface_unverified");
    expect(
      toAgentCommandError(new MemaxError("x", "invalid_transition", 409))
        .refusal,
    ).toBe("invalid_transition");
    expect(toAgentCommandError(new Error("network")).refusal).toBe("failed");
    const kept = new AgentCommandError("needs_web");
    expect(toAgentCommandError(kept)).toBe(kept);
  });

  it("is what setAutonomy throws", async () => {
    const setAutonomy = vi
      .fn()
      .mockRejectedValue(refused("autonomy_needs_web"));
    const agents = createSdkAgents({
      client: { v2: { agents: { setAutonomy } } } as never,
      viewer,
    });
    await expect(
      agents.setAutonomy({
        agent: "c1",
        space: DEMO_SPACES[1]!,
        autonomy: "write",
        idempotencyKey: "k",
      }),
    ).rejects.toMatchObject({ refusal: "needs_web" });
    expect(setAutonomy).toHaveBeenCalledWith(
      "c1",
      "memax-v2",
      { autonomy: "write" },
      { idempotencyKey: "k" },
    );
  });
});

describe("toApiKey", () => {
  it("shows read or propose, the key's prefix, and its spaces", () => {
    const base = {
      id: "k1",
      name: "CI",
      prefix: "mxk_7f2c",
      scope: "1 hub",
      hub_id: "s1",
      expires_at: null,
      last_used: null,
      created_at: "2026-09-20T09:00:00Z",
    };
    expect(toApiKey({ ...base, scopes: ["read"] })).toEqual({
      id: "k1",
      name: "CI",
      masked: "mxk_7f2c…",
      spaceIds: ["s1"],
      may: "read",
      createdAt: "2026-09-20T09:00:00Z",
      lastUsedAt: null,
    });
    expect(
      toApiKey({
        ...base,
        hub_id: null,
        scopes: [],
        default_permissions: ["memory:read", "memory:write"],
      }),
    ).toMatchObject({ may: "propose", spaceIds: null });
  });
});

describe("agentsOverview", () => {
  it("counts the space's agents, and says when there are none", async () => {
    const listInSpace = vi
      .fn()
      .mockResolvedValueOnce({
        items: [connection(), connection({ id: "c2", state: "paused" })],
      })
      .mockResolvedValueOnce({ items: [] })
      .mockRejectedValueOnce(new Error("down"));
    const client = { v2: { agents: { listInSpace } } } as never;
    expect(await agentsOverview(client, "memax-v2")).toEqual({
      counts: { connected: 2, active: 1 },
      status: null,
    });
    expect(await agentsOverview(client, "memax-web")).toEqual({
      counts: { connected: 0, active: 0 },
      status: { kind: "no-agents" },
    });
    expect(await agentsOverview(client, "x")).toBeNull();
  });
});

describe("the demo agents", () => {
  const v2 = DEMO_SPACES.find((s) => s.slug === "memax-v2")!;

  it("are Agents.png's six, and Codex has AgentDetail.png's week", async () => {
    const demo = createDemoAgents();
    const list = await demo.spaceAgents(v2);
    expect(
      list.map((a) => [a.name, autonomyIn(a, "memax-v2")?.autonomy]),
    ).toEqual([
      ["Claude Code", "write"],
      ["Codex", "propose"],
      ["Cursor", "read"],
      ["ChatGPT", "propose"],
      ["Claude", "propose"],
      ["Gemini CLI", "read"],
    ]);
    expect(list[0]!.spaces[0]!.reads7d).toBe(1204);
    expect(list[5]!.state).toBe("paused");
    const codex = await demo.agent(list[1]!.id);
    expect(codex?.week).toMatchObject({ reads: 512, writes: 21, kept: 14 });
    expect(codex?.recentWrites.map((w) => w.ref)).toEqual([
      "M-0431",
      "M-0432",
      "M-0410",
    ]);
  });

  it("change in memory, and refuse what the server would", async () => {
    const demo = createDemoAgents();
    const [claudeCode] = await demo.spaceAgents(v2);
    const lowered = await demo.setAutonomy({
      agent: claudeCode!.id,
      space: v2,
      autonomy: "read",
      idempotencyKey: "k",
    });
    expect(lowered.spaces[0]!.autonomy).toBe("read");
    expect((await demo.spaceAgents(v2))[0]!.spaces[0]!.autonomy).toBe("read");
    const gone = await demo.disconnectAgent({
      agent: claudeCode!.id,
      idempotencyKey: "k2",
    });
    expect(gone).toMatchObject({
      state: "disconnected",
      credential: { active: false },
    });
    expect(await demo.spaceAgents(v2)).toHaveLength(5);
    await expect(
      demo.pauseAgent({ agent: claudeCode!.id, idempotencyKey: "k3" }),
    ).rejects.toMatchObject({ refusal: "invalid_transition" });
    // A fresh source starts from the board again.
    expect(await createDemoAgents().spaceAgents(v2)).toHaveLength(6);
  });
});
