import { describe, expect, it, vi } from "vitest";
import { Memax, MemaxError, refusalOf } from "../index.js";
import type { V2 } from "../index.js";

function client(body: unknown, status = 200) {
  const fetchMock = vi.fn(
    async () =>
      new Response(JSON.stringify(body), {
        status,
        headers: { "Content-Type": "application/json" },
      }),
  );
  const memax = new Memax({
    apiUrl: "https://api.memax.app",
    apiKey: "session-token",
    fetch: fetchMock,
    maxRetries: 0,
  });
  const call = () => {
    const [url, init] = fetchMock.mock.calls[0] as unknown as [
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

const agent = {
  id: "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a61",
  person_id: "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5d",
  agent: "copilot",
  display_name: "Copilot cloud agent · acme-web",
  surface: "cloud",
  credential: {
    kind: "api_key",
    id: "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a62",
    active: true,
  },
  max_autonomy: "read",
  state: "active",
  spaces: [],
  reads_7d: 0,
  writes_7d: 0,
  created_receipt_id: "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a63",
  last_receipt_id: "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a63",
  created_at: "2026-10-11T09:00:00Z",
  updated_at: "2026-10-11T09:00:00Z",
} as unknown as V2.AgentConnection;

const key: V2.StartKey = {
  secret: "mxk_" + "a".repeat(64),
  prefix: "mxk_aaaa",
  expires_at: "2027-04-09T09:00:00Z",
};

describe("memax.v2.agents.connectCloud and rekey", () => {
  it("connects a cloud agent to a space and answers its start key once", async () => {
    const res: V2.CloudAgentResult = {
      outcome: "applied",
      policy: { effect: "apply" },
      agent,
      key,
      receipts: [],
    };
    const { memax, call } = client({ data: res }, 201);
    const got = await memax.v2.agents.connectCloud(
      { agent: "copilot", space: "acme-web" },
      { idempotencyKey: "cloud-1", via: "cli" },
    );
    expect(got.key?.secret).toBe(key.secret);
    const c = call();
    expect(c.method).toBe("POST");
    expect(c.url).toBe("https://api.memax.app/v2/cloud-agents");
    expect(c.headers["Idempotency-Key"]).toBe("cloud-1");
    expect(c.body).toEqual({ agent: "copilot", space: "acme-web" });
  });

  it("rekeys under the same connection", async () => {
    const { memax, call } = client({
      data: {
        outcome: "applied",
        policy: { effect: "apply" },
        agent,
        key,
        receipts: [],
      },
    });
    const got = await memax.v2.agents.rekey(
      agent.id,
      {},
      { idempotencyKey: "rekey-1" },
    );
    expect(got.agent.id).toBe(agent.id);
    expect(call().url).toBe(
      `https://api.memax.app/v2/agents/${agent.id}:rekey`,
    );
  });

  it("surfaces Propose from the CLI as the web's refusal", async () => {
    const { memax } = client(
      {
        error: {
          code: "refused",
          message: "letting it propose needs you on the web",
          details: {
            policy: { effect: "refuse", code: "cloud_propose_needs_web" },
          },
        },
      },
      403,
    );
    const err = await memax.v2.agents
      .connectCloud(
        { agent: "copilot", space: "acme-web", autonomy: "propose" },
        { idempotencyKey: "cloud-2" },
      )
      .catch((e: unknown) => e);
    expect(err).toBeInstanceOf(MemaxError);
    expect(refusalOf(err)?.code).toBe("cloud_propose_needs_web");
  });
});
