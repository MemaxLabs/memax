import { MemaxError, type V2 } from "memax-sdk";
import { describe, expect, it, vi } from "vitest";
import { webSessionOf } from "../web-session";
import { toFailure } from "./command-error";
import { DEMO_SPACES } from "./demo-dataset";
import { createDemoSource } from "./demo-source";
import {
  answerNeedsWebHere,
  askingAgents,
  gateStatusAt,
  isGateRef,
  waitingGates,
  type GateView,
} from "./gates";
import { answerStatement } from "./gates-demo";
import { createSdkGates, gateOf, GATES_PAGE } from "./gates-sdk";
import { createSdkSource } from "./sdk-source";
import type { V2Client } from "./sdk-records";
import { waitingOnYou } from "./types";

const team = DEMO_SPACES.find((s) => s.slug === "memax-team")!;
const ME = "user-zz";
const NOW = new Date("2026-10-05T21:40:00Z");

function gate(fields: Partial<V2.Gate> = {}): V2.Gate {
  return {
    id: "0192a7c0-0000-7000-8000-0000000c0012",
    ref: "G-0012",
    space_id: "s1",
    tenant_id: "t1",
    question: "Which deploy target should the v2 API use?",
    context: "M-0174 and M-0431 disagree.",
    options: [
      { label: "Fly.io, iad and ams", detail: "Where it runs today." },
      { label: "Railway" },
      { label: "Decide later", detail: "  " },
    ],
    status: "waiting",
    expires_at: "2026-10-12T21:38:00Z",
    asked_by: "conn-codex",
    agent: "codex",
    session_ref: "session 9f1c",
    needs_web: true,
    version: 1,
    created_receipt_id: "r1",
    last_receipt_id: "r1",
    created_at: "2026-10-05T21:38:00Z",
    updated_at: "2026-10-05T21:38:00Z",
    ...fields,
  };
}

function receipt(fields: Partial<V2.Receipt> = {}): V2.Receipt {
  return {
    id: "r2",
    seq: 2,
    tenant_id: "t1",
    space_id: "s1",
    object_kind: "gate",
    object_id: "g1",
    object_ref: "G-0012",
    action: "answered",
    actor_kind: "person",
    actor_id: ME,
    via: "web",
    occurred_at: "2026-10-05T21:41:00Z",
    recorded_at: "2026-10-05T21:41:00Z",
    stream_id: "g1",
    stream_version: 2,
    ...fields,
  };
}

/** A client with only memax.v2.gates, each method a mock. */
function client(gates: Partial<V2Client["v2"]["gates"]>) {
  return { v2: { gates } } as unknown as V2Client;
}

describe("mapping /v2 gates", () => {
  it("reads the question, the options from 0, the agent and the session", () => {
    const view = gateOf(gate({ agent: "gemini-cli" }), ME);
    expect(view).toMatchObject({
      ref: "G-0012",
      version: 1,
      question: "Which deploy target should the v2 API use?",
      context: "M-0174 and M-0431 disagree.",
      status: "waiting",
      agent: "gemini",
      session: "9f1c",
      askedAt: "2026-10-05T21:38:00Z",
      expiresAt: "2026-10-12T21:38:00Z",
      needsWeb: true,
      answer: null,
      withdrawn: null,
    });
    // An option without a detail, or a blank one, has none.
    expect(view.options).toEqual([
      { label: "Fly.io, iad and ams", detail: "Where it runs today." },
      { label: "Railway", detail: null },
      { label: "Decide later", detail: null },
    ]);
    expect(
      gateOf(gate({ context: " ", session_ref: undefined }), ME),
    ).toMatchObject({ context: null, session: null });
  });

  it("reads an answer as the viewer's or a teammate's, counting the option from 0", () => {
    const answer = (by: string) =>
      gateOf(
        gate({
          status: "answered",
          version: 2,
          answer: {
            option: 2,
            label: "Railway",
            memory: { id: "m9", ref: "M-0447" },
            answered_by: by,
            answered_at: "2026-10-05T21:41:00Z",
            assurance: "human_web",
          },
        }),
        ME,
      ).answer;
    expect(answer(ME)).toEqual({
      option: 1,
      label: "Railway",
      memory: "M-0447",
      by: { kind: "person", self: true },
      at: "2026-10-05T21:41:00Z",
    });
    expect(answer("jy")?.by).toEqual({ kind: "person", self: false });
  });

  it("reads who took a question back: the agent that asked, or a person", () => {
    const withdrawn = (byKind: "agent" | "person", by: string) =>
      gateOf(
        gate({
          status: "withdrawn",
          withdrawn: { by_kind: byKind, by, at: "2026-10-05T21:42:00Z" },
        }),
        ME,
      ).withdrawn;
    expect(withdrawn("agent", "conn-codex")?.by).toEqual({
      kind: "agent",
      agent: "codex",
    });
    expect(withdrawn("person", ME)?.by).toEqual({ kind: "person", self: true });
  });
});

describe("gates by the clock", () => {
  const view = gateOf(gate(), ME);
  it("reads a waiting gate past its expiry as expired", () => {
    expect(gateStatusAt(view, NOW)).toBe("waiting");
    expect(gateStatusAt(view, new Date("2026-10-12T21:38:00Z"))).toBe(
      "expired",
    );
    expect(gateStatusAt({ ...view, status: "withdrawn" }, NOW)).toBe(
      "withdrawn",
    );
  });

  it("lists the waiting ones soonest to expire first, and names their agents once", () => {
    const later = view;
    const sooner: GateView = {
      ...view,
      ref: "G-0011",
      agent: "claude-code",
      expiresAt: "2026-10-06T20:40:00Z",
    };
    const gone: GateView = {
      ...view,
      ref: "G-0010",
      expiresAt: "2026-10-05T20:00:00Z",
    };
    const list = waitingGates([later, gone, sooner], NOW);
    expect(list.map((g) => g.ref)).toEqual(["G-0011", "G-0012"]);
    expect(askingAgents([...list, later])).toEqual(["claude-code", "codex"]);
  });

  it("knows a G- ref, and when an answer here needs the web", () => {
    expect(isGateRef("G-0012")).toBe(true);
    expect(isGateRef("H-0093")).toBe(false);
    expect(answerNeedsWebHere(view, false)).toBe(true);
    expect(answerNeedsWebHere(view, true)).toBe(false);
    expect(answerNeedsWebHere(view, null)).toBe(false);
    expect(answerNeedsWebHere({ ...view, needsWeb: false }, false)).toBe(false);
  });

  it("words an answer the way the ledger keeps it", () => {
    expect(answerStatement("Which deploy target?", "Railway")).toBe(
      "Which deploy target? Railway",
    );
    expect(answerStatement("Deploy target", "Railway")).toBe(
      "Deploy target: Railway",
    );
    expect(answerStatement("用哪个？", "Railway")).toBe("用哪个？Railway");
  });
});

describe("memax.v2.gates behind the source", () => {
  it("lists the space's waiting gates in one page, soonest to expire first", async () => {
    const list = vi.fn(async () => ({
      items: [
        gate(),
        gate({ ref: "G-0011", expires_at: "2026-10-06T20:40:00Z" }),
      ],
      has_more: false,
    }));
    const gates = createSdkGates({
      client: client({ list }),
      viewerId: () => ME,
      webSession: () => true,
    });
    const waiting = await gates.waiting({ space: team });
    expect(list).toHaveBeenCalledWith("memax-team", {
      status: "waiting",
      limit: GATES_PAGE,
      signal: undefined,
    });
    expect(waiting.map((g) => g.ref)).toEqual(["G-0011", "G-0012"]);
    expect(gates.webSession()).toBe(true);
  });

  it("reads one gate by ref within the space, and a missing one as null", async () => {
    const get = vi
      .fn()
      .mockResolvedValueOnce(gate({ status: "withdrawn" }))
      .mockRejectedValueOnce(new MemaxError("nope", "not_found", 404));
    const gates = createSdkGates({
      client: client({ get }),
      viewerId: () => ME,
      webSession: () => null,
    });
    expect((await gates.get({ space: team, ref: "G-0012" }))?.status).toBe(
      "withdrawn",
    );
    expect(get).toHaveBeenCalledWith("G-0012", {
      space: "memax-team",
      signal: undefined,
    });
    expect(await gates.get({ space: team, ref: "G-0099" })).toBeNull();
  });

  it("answers with the option from 1, If-Match on the version and the caller's key", async () => {
    const answered = gate({
      status: "answered",
      version: 2,
      answer: {
        option: 1,
        label: "Fly.io, iad and ams",
        memory: { id: "m9", ref: "M-0447" },
        answered_by: ME,
        answered_at: "2026-10-05T21:41:00Z",
        assurance: "human_web",
      },
    });
    const answer = vi.fn(async () => ({
      outcome: "applied" as const,
      policy: { effect: "apply" as const },
      gate: answered,
      receipts: [receipt()],
    }));
    const gates = createSdkGates({
      client: client({ answer }),
      viewerId: () => ME,
      webSession: () => true,
    });
    const result = await gates.answer({
      space: team,
      gate: gateOf(gate(), ME),
      option: 0,
      idempotencyKey: "k1",
    });
    expect(answer).toHaveBeenCalledWith(
      "G-0012",
      { option: 1 },
      { space: "memax-team", idempotencyKey: "k1", ifMatch: 1 },
    );
    // Without the memory in the result, the gate's answer names it.
    expect(result.memory).toEqual({ ref: "M-0447", version: 1 });
    expect(result.gate.status).toBe("answered");
    expect(result.recompiled).toBeNull();
  });

  it("withdraws with If-Match, and lets D15's refusal and an ended gate through as failures", async () => {
    const withdraw = vi.fn(async () => ({
      outcome: "applied" as const,
      policy: { effect: "apply" as const },
      gate: gate({ status: "withdrawn", version: 2 }),
      receipts: [receipt({ action: "withdrawn" })],
    }));
    const refusal = new MemaxError("needs the web", "refused", 403, {
      policy: {
        effect: "refuse",
        code: "decision_needs_web",
        message: "Decisions in Memax team need a person on the web.",
      },
    });
    const ended = new MemaxError(
      "already answered",
      "invalid_transition",
      409,
      {
        ref: "G-0012",
        status: "answered",
      },
    );
    const answer = vi
      .fn()
      .mockRejectedValueOnce(refusal)
      .mockRejectedValueOnce(ended);
    const gates = createSdkGates({
      client: client({ withdraw, answer }),
      viewerId: () => ME,
      webSession: () => false,
    });
    const view = gateOf(gate(), ME);
    const back = await gates.withdraw({
      space: team,
      gate: view,
      idempotencyKey: "w1",
    });
    expect(withdraw).toHaveBeenCalledWith(
      "G-0012",
      {},
      { space: "memax-team", idempotencyKey: "w1", ifMatch: 1 },
    );
    expect(back.status).toBe("withdrawn");
    const first = await gates
      .answer({ space: team, gate: view, option: 0, idempotencyKey: "a" })
      .catch((e: unknown) => e);
    expect(toFailure(first)).toMatchObject({
      kind: "refused",
      code: "decision_needs_web",
    });
    const second = await gates
      .answer({ space: team, gate: view, option: 0, idempotencyKey: "b" })
      .catch((e: unknown) => e);
    expect(toFailure(second)).toEqual({ kind: "decided", status: "answered" });
  });
});

describe("the overview and Today with gates", () => {
  function sdk(gates: Partial<V2Client["v2"]["gates"]>) {
    const empty = vi.fn().mockResolvedValue({ items: [], has_more: false });
    const fake = {
      v2: {
        review: {
          list: vi.fn().mockResolvedValue({
            items: [],
            total: 3,
            has_more: false,
          }),
        },
        memories: { list: empty },
        receipts: { list: empty },
        gates,
      },
    };
    return createSdkSource({
      client: fake as never,
      viewer: { id: ME, initials: "ZZ", name: "Ziyang", timeZone: "UTC" },
      webSession: () => false,
    });
  }

  it("counts the waiting gates beside Review's memories, for the rail", async () => {
    const source = sdk({
      list: vi.fn().mockResolvedValue({
        items: [
          gate(),
          gate({
            ref: "G-0011",
            agent: "claude-code",
            expires_at: "2026-10-06T20:40:00Z",
          }),
        ],
        has_more: false,
      }),
    });
    const overview = await source.overview(team);
    expect(overview.waiting).toBe(3);
    expect(overview.gatesWaiting).toBe(2);
    expect(waitingOnYou(overview)).toBe(5);
    expect(source.gates.webSession()).toBe(false);
    const today = await source.today.get({ space: team });
    expect(today.waiting.gates.map((g) => g.ref)).toEqual(["G-0011", "G-0012"]);
    expect(today.waiting.total).toBe(5);
    expect(today.waiting.questions).toEqual(["claude-code", "codex"]);
  });

  it("leaves the gates uncounted, not failed, when they don't load", async () => {
    const source = sdk({
      list: vi
        .fn()
        .mockRejectedValue(new MemaxError("down", "network_error", 0)),
    });
    const overview = await source.overview(team);
    expect(overview.gatesWaiting).toBeNull();
    expect(waitingOnYou(overview)).toBe(3);
  });
});

describe("the demo's gates", () => {
  it("answers like the server: a kept decision by you, once per key, then 409 with how it ended", async () => {
    const demo = createDemoSource({ commandDelayMs: 0 });
    const [first] = demo.gates.peekWaiting?.("memax-team") ?? [];
    expect(first?.ref).toBe("G-0011");
    expect(demo.gates.peekWaiting?.("memax-v2")).toEqual([]);
    const result = await demo.gates.answer({
      space: team,
      gate: first!,
      option: 1,
      idempotencyKey: "a",
    });
    expect(result.memory.ref).toBe("M-0439");
    // The same key is the same answer.
    const replay = await demo.gates.answer({
      space: team,
      gate: first!,
      option: 1,
      idempotencyKey: "a",
    });
    expect(replay.memory.ref).toBe("M-0439");
    const page = await demo.memories.list({ space: team, filter: "all" });
    expect(page.items[0]).toMatchObject({
      ref: "M-0439",
      state: "kept",
      section: "decisions",
      source: "G-0011",
      statement:
        "Keep the ChatGPT tool names, or align them with the core set? Align with the core set",
    });
    const overview = demo.peek?.overview("memax-team");
    expect(overview?.gatesWaiting).toBe(1);
    expect(overview?.memories.kept).toBe(39);
    expect(overview?.waitingBreakdown?.questions).toEqual(["codex"]);
    const again = await demo.gates
      .answer({ space: team, gate: first!, option: 0, idempotencyKey: "b" })
      .catch((e: unknown) => e);
    expect(toFailure(again)).toEqual({ kind: "decided", status: "answered" });
  });

  it("refuses an answer from a session that isn't the web app's (D15)", async () => {
    const demo = createDemoSource({ commandDelayMs: 0, webSession: false });
    const [first] = demo.gates.peekWaiting?.("memax-team") ?? [];
    const err = await demo.gates
      .answer({ space: team, gate: first!, option: 0, idempotencyKey: "a" })
      .catch((e: unknown) => e);
    expect(toFailure(err)).toMatchObject({
      kind: "refused",
      code: "decision_needs_web",
    });
    expect(demo.gates.peekWaiting?.("memax-team")).toHaveLength(2);
  });
});

describe("whether a session is the web app's", () => {
  const token = (claims: object) =>
    `h.${Buffer.from(JSON.stringify(claims)).toString("base64url")}.s`;
  it("reads the surface claim, and can't tell without a token", () => {
    expect(webSessionOf(token({ sub: "u", surface: "web" }))).toBe(true);
    expect(webSessionOf(token({ sub: "u", surface: "cli" }))).toBe(false);
    // A session from before migration 030 carries no surface.
    expect(webSessionOf(token({ sub: "u" }))).toBe(false);
    expect(webSessionOf("mxk_live_123")).toBe(false);
    expect(webSessionOf(null)).toBeNull();
    expect(webSessionOf("not-a-jwt")).toBeNull();
    expect(webSessionOf("a.%%%.c")).toBeNull();
  });
});
