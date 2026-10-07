import { describe, expect, it } from "vitest";
import type { V2 } from "memax-sdk";
import { forgetRequestsOf, previewOf, tombstoneOf } from "./sdk-forget";
import { withForgetCounts } from "./activity-sdk";
import type { ActivityEntry } from "./activity";

const ids = {
  memory: "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a70",
  space: "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5c",
  me: "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a72",
};

const tombstone = (over: Partial<V2.Tombstone> = {}): V2.Tombstone => ({
  id: ids.memory,
  op_id: ids.memory,
  ref: "M-0201",
  kind: "memory",
  object_id: ids.memory,
  space_id: ids.space,
  tenant_id: ids.space,
  with: ["M-0202"],
  by: { kind: "person", id: ids.me },
  via: "web",
  receipt_id: ids.memory,
  forgotten_at: "2026-10-03T10:12:04Z",
  reads_before: 23,
  gone: {
    versions: 1,
    sources: 2,
    embeddings: 1,
    verdicts: 0,
    model_verdicts: 0,
    gates: 0,
    files: 4,
  },
  agents: 5,
  status: "done",
  steps: [
    { kind: "asked", status: "done", at: "2026-10-03T10:12:04Z" },
    {
      kind: "target",
      status: "held",
      reason: "hand_edit",
      target: {
        id: ids.space,
        kind: "cursor_mdc",
        label: ".cursor/rules",
        delivery: "local",
      },
    },
    {
      kind: "agent",
      status: "waiting",
      reason: "paused",
      agent: { connection_id: ids.space, agent: "gemini-cli" },
    },
  ],
  unreachable: [
    { kind: "backups", days: 7 },
    {
      kind: "llm",
      processors: [
        { name: "OpenRouter", purpose: "judge", zero_retention: true },
      ],
    },
  ],
  ...over,
});

describe("Forget's /v2 shapes", () => {
  it("splits a preview's files from its copy-outs, and says why not", () => {
    const p = previewOf({
      ref: "M-0201",
      version: 3,
      carries: [
        {
          id: ids.memory,
          ref: "M-0202",
          reason: "cites",
          with: "M-0201",
          lifecycle: "kept",
          kind: "fact",
        },
      ],
      files: [
        { id: "a", kind: "agents_md", label: "AGENTS.md", delivery: "local" },
        {
          id: "b",
          kind: "chatgpt",
          label: "ChatGPT project",
          delivery: "copy",
        },
      ],
      agents: 5,
      readers: 4,
      allowed: false,
      policy: {
        effect: "refuse",
        code: "forget_not_allowed",
        message: "Only owners forget.",
      },
    });
    expect(p).toEqual({
      version: 3,
      carries: [{ ref: "M-0202", reason: "cites" }],
      files: 1,
      copies: 1,
      agents: 5,
      refusal: { code: "forget_not_allowed", message: "Only owners forget." },
    });
  });

  it("keeps only the waiting requests, newest first", () => {
    const req = (
      status: V2.ForgetRequestStatus,
      at: string,
      agent: string,
    ): V2.ForgetRequestRecord => ({
      id: at,
      memory_id: ids.memory,
      ref: "M-0201",
      space_id: ids.space,
      agent: { connection_id: ids.space, agent: agent as V2.AgentKind },
      reason: " it was a test value ",
      status,
      receipt_id: ids.memory,
      requested_at: at,
    });
    expect(
      forgetRequestsOf([
        req("waiting", "2026-10-05T10:00:00Z", "codex"),
        req("declined", "2026-10-05T12:00:00Z", "cursor"),
        req("waiting", "2026-10-05T11:00:00Z", "gemini-cli"),
      ]),
    ).toEqual([
      {
        agent: "gemini",
        reason: "it was a test value",
        at: "2026-10-05T11:00:00Z",
      },
      {
        agent: "codex",
        reason: "it was a test value",
        at: "2026-10-05T10:00:00Z",
      },
    ]);
    expect(forgetRequestsOf(undefined)).toEqual([]);
  });

  it("reads a tombstone: who, the steps, and what Memax can't reach", () => {
    const t = tombstoneOf(tombstone({ note: " personal " }), ids.me);
    expect(t.by).toEqual({ kind: "person", self: true });
    expect(t.note).toBe("personal");
    expect(t.via).toBe("web");
    expect(t.agents).toBe(5);
    expect(t.gone).toEqual({
      versions: 1,
      sources: 2,
      embeddings: 1,
      files: 4,
    });
    expect(t.steps.map((s) => [s.kind, s.status, s.reason])).toEqual([
      ["asked", "done", null],
      ["target", "held", "hand_edit"],
      ["agent", "waiting", "paused"],
    ]);
    expect(t.steps[1]!.target).toEqual({
      label: ".cursor/rules",
      kind: "cursor_mdc",
      delivery: "local",
    });
    expect(t.steps[2]!.agent).toBe("gemini");
    expect(t.unreachable[1]!.processors).toEqual([
      { name: "OpenRouter", purpose: "judge", zeroRetention: true },
    ]);
    // Another person, Memax re-applying, a carried memory.
    expect(tombstoneOf(tombstone(), "someone-else").by).toEqual({
      kind: "person",
      self: false,
    });
    expect(
      tombstoneOf(tombstone({ by: { kind: "memax" } }), ids.me).by,
    ).toEqual({ kind: "memax" });
    expect(
      tombstoneOf(tombstone({ carried: "folded", primary: "M-0200" }), ids.me)
        .carried,
    ).toEqual({ reason: "folded", primary: "M-0200" });
  });

  it("fills Activity's forgot rows from their tombstones", () => {
    const entry = (ref: string): ActivityEntry =>
      ({
        id: ref,
        at: "2026-10-03T10:12:04Z",
        actor: { kind: "you", initials: "ZZ" },
        action: "forgot",
        object: { kind: "memory", ref, id: ref },
        via: [],
        rawVia: "web",
        session: null,
        source: null,
        reason: null,
      }) as ActivityEntry;
    const entries = [entry("M-0201"), entry("M-0202"), entry("M-0300")];
    withForgetCounts(entries, [
      tombstone(),
      tombstone({ ref: "M-0202", carried: "cites", primary: "M-0201" }),
      tombstone({
        ref: "M-0300",
        gone: { ...tombstone().gone, files: 0 },
        agents: 0,
      }),
    ]);
    expect(entries[0]!.detail).toEqual({ kind: "forgot", files: 4, agents: 5 });
    // A memory carried with another's Forget, and one nothing held, say less.
    expect(entries[1]!.detail).toBeUndefined();
    expect(entries[2]!.detail).toBeUndefined();
  });
});
