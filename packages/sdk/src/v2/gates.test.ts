import { describe, expect, it, vi } from "vitest";
import { Memax, MemaxError, refusalOf } from "../index.js";
import type { V2 } from "../index.js";

function jsonResponse(body: unknown, init?: ResponseInit): Response {
  return new Response(JSON.stringify(body), {
    status: 200,
    headers: { "Content-Type": "application/json" },
    ...init,
  });
}

function client(response: Response) {
  const fetchMock = vi.fn(async () => response.clone());
  const memax = new Memax({
    apiUrl: "https://api.memax.app",
    apiKey: "mxk_test",
    fetch: fetchMock,
    maxRetries: 0,
  });
  const call = (i = 0) => {
    const [url, init] = fetchMock.mock.calls[i] as unknown as [
      string,
      RequestInit & { headers: Record<string, string> },
    ];
    return {
      url,
      method: init.method,
      headers: init.headers,
      body: init.body === undefined ? undefined : JSON.parse(String(init.body)),
    };
  };
  return { memax, call };
}

const gate: V2.Gate = {
  id: "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a60",
  ref: "G-0012",
  space_id: "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5c",
  tenant_id: "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5d",
  question: "Which deploy target should the v2 API use?",
  options: [
    { label: "Fly.io, iad and ams", detail: "Matches the current API." },
    { label: "Railway" },
  ],
  status: "waiting",
  expires_at: "2026-10-13T09:30:00Z",
  asked_by: "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a61",
  agent: "codex",
  needs_web: false,
  version: 1,
  created_receipt_id: "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a62",
  last_receipt_id: "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a62",
  created_at: "2026-10-06T09:30:00Z",
  updated_at: "2026-10-06T09:30:00Z",
};

const result: V2.GateResult = {
  outcome: "applied",
  policy: { effect: "apply" },
  gate,
  receipts: [],
};

describe("memax.v2.gates", () => {
  it("lists a space's gates by status, with a cursor", async () => {
    const page: V2.GatePage = { items: [gate], has_more: false };
    const { memax, call } = client(jsonResponse({ data: page }));

    await expect(
      memax.v2.gates.list("memax-v2", {
        status: ["waiting", "expired"],
        cursor: "ZzEy",
        limit: 5,
      }),
    ).resolves.toEqual(page);
    expect(call().url).toBe(
      "https://api.memax.app/v2/spaces/memax-v2/gates?cursor=ZzEy&limit=5&status=waiting&status=expired",
    );
    expect(call().method).toBe("GET");
  });

  it("asks with an Idempotency-Key and the surface", async () => {
    const { memax, call } = client(
      jsonResponse({ data: result }, { status: 201 }),
    );
    const input: V2.RequestDecisionInput = {
      question: gate.question,
      context: "M-0174 and M-0431 disagree.",
      options: gate.options,
    };

    await expect(
      memax.v2.gates.request("memax-v2", input, {
        idempotencyKey: "ask-1",
        via: "mcp",
      }),
    ).resolves.toEqual(result);
    const c = call();
    expect(c.url).toBe("https://api.memax.app/v2/spaces/memax-v2/gates");
    expect(c.method).toBe("POST");
    expect(c.headers).toMatchObject({
      "Idempotency-Key": "ask-1",
      "X-Memax-Via": "mcp",
    });
    expect(c.body).toEqual(input);
  });

  it("reads a display ID with its space, and an id without one", async () => {
    const { memax, call } = client(jsonResponse({ data: gate }));

    await expect(
      memax.v2.gates.get("G-0012", { space: "memax-v2" }),
    ).resolves.toEqual(gate);
    await memax.v2.gates.get(gate.id);
    expect(call(0).url).toBe(
      "https://api.memax.app/v2/gates/G-0012?space=memax-v2",
    );
    expect(call(1).url).toBe(`https://api.memax.app/v2/gates/${gate.id}`);
  });

  it("answers and withdraws with If-Match on the gate's version", async () => {
    const answered: V2.GateResult = {
      ...result,
      gate: { ...gate, status: "answered", version: 2 },
    };
    const { memax, call } = client(jsonResponse({ data: answered }));

    await expect(
      memax.v2.gates.answer(
        "G-0012",
        { option: 1, reason: "The workers are there." },
        { space: "memax-v2", idempotencyKey: "a", ifMatch: 1, via: "cli" },
      ),
    ).resolves.toEqual(answered);
    await memax.v2.gates.withdraw(gate.id, {}, { idempotencyKey: "w" });

    expect(call(0).url).toBe(
      "https://api.memax.app/v2/gates/G-0012:answer?space=memax-v2",
    );
    expect(call(0).headers).toMatchObject({
      "Idempotency-Key": "a",
      "If-Match": '"1"',
      "X-Memax-Via": "cli",
    });
    expect(call(0).body).toEqual({
      option: 1,
      reason: "The workers are there.",
    });
    expect(call(1).url).toBe(
      `https://api.memax.app/v2/gates/${gate.id}:withdraw`,
    );
    expect(call(1).headers["If-Match"]).toBeUndefined();
    expect(call(1).body).toEqual({});
  });

  it("surfaces D15's refusal and a gate that already ended", async () => {
    const refused = client(
      jsonResponse(
        {
          error: {
            code: "refused",
            message: "Decisions in acme need a person on the web.",
            details: {
              policy: {
                effect: "refuse",
                code: "decision_needs_web",
                message: "Decisions in acme need a person on the web.",
              },
            },
          },
        },
        { status: 403 },
      ),
    );
    const err = await refused.memax.v2.gates
      .answer("G-0012", { option: 2 }, { space: "acme", idempotencyKey: "a" })
      .catch((e: unknown) => e);
    expect(err).toBeInstanceOf(MemaxError);
    expect(refusalOf(err)?.code).toBe("decision_needs_web");

    const ended = client(
      jsonResponse(
        {
          error: {
            code: "invalid_transition",
            message: "G-0012 was already answered.",
            details: { ref: "G-0012", status: "answered" },
          },
        },
        { status: 409 },
      ),
    );
    const e2 = await ended.memax.v2.gates
      .answer(gate.id, { option: 1 }, { idempotencyKey: "b" })
      .catch((e: unknown) => e);
    expect(e2).toBeInstanceOf(MemaxError);
    expect((e2 as MemaxError).status).toBe(409);
    expect((e2 as MemaxError).details?.status).toBe("answered");
    expect(refusalOf(e2)).toBeUndefined();
  });
});
