import { describe, expect, it, vi } from "vitest";
import { Memax } from "../index.js";
import type { V2 } from "../index.js";

function jsonResponse(body: unknown, init?: ResponseInit): Response {
  return new Response(JSON.stringify(body), {
    status: 200,
    headers: { "Content-Type": "application/json" },
    ...init,
  });
}

function client(...responses: Response[]) {
  let i = 0;
  const fetchMock = vi.fn(async () =>
    responses[Math.min(i++, responses.length - 1)].clone(),
  );
  const memax = new Memax({
    apiUrl: "https://api.memax.app",
    fetch: fetchMock,
    maxRetries: 0,
  });
  const call = (n = 0) => {
    const [url, init] = fetchMock.mock.calls[n] as unknown as [
      string,
      RequestInit & { headers: Record<string, string> },
    ];
    return {
      url,
      method: init.method,
      headers: init.headers,
      body: init.body ? JSON.parse(init.body as string) : undefined,
    };
  };
  return { memax, call };
}

const machine: V2.Machine = {
  id: "0199b0f3-2c4e-7000-8000-0000000000a1",
  space_id: "0199b0f3-2c4e-7000-8000-0000000000b1",
  label: "machine-1",
  name: "work-mbp",
  platform: "darwin",
  device_id: "0199b0f3-2c4e-7000-8000-0000000000c1",
  linked: true,
  linked_at: "2026-10-11T09:00:00Z",
  version: 1,
  created_receipt_id: "0199b0f3-2c4e-7000-8000-0000000000d1",
  last_receipt_id: "0199b0f3-2c4e-7000-8000-0000000000d1",
  created_at: "2026-10-11T09:00:00Z",
  updated_at: "2026-10-11T09:00:00Z",
};
const result = (m: V2.Machine): { data: V2.MachineResult } => ({
  data: {
    outcome: "applied",
    policy: { effect: "apply" } as V2.PolicyDecision,
    machine: m,
    targets: [],
    receipts: [],
  },
});

describe("memax.v2.machines", () => {
  it("links a machine with the files you approved, as a command", async () => {
    const { memax, call } = client(
      jsonResponse(result(machine), { status: 201 }),
    );
    const got = await memax.v2.machines.link(
      "personal",
      {
        device_id: machine.device_id,
        name: "work-mbp",
        platform: "darwin",
        files: [{ kind: "claude_user" }, { kind: "codex_user" }],
      },
      { idempotencyKey: "k-1" },
    );
    expect(got.machine.label).toBe("machine-1");
    expect(call().url).toBe(
      "https://api.memax.app/v2/spaces/personal/machines",
    );
    expect(call().method).toBe("POST");
    expect(call().headers["Idempotency-Key"]).toBe("k-1");
    expect(call().body.files).toEqual([
      { kind: "claude_user" },
      { kind: "codex_user" },
    ]);
  });

  it("lists, renames with If-Match, and unlinks", async () => {
    const { memax, call } = client(
      jsonResponse({ data: { items: [machine] } }),
      jsonResponse(result({ ...machine, name: "Work laptop", version: 2 })),
      jsonResponse(result({ ...machine, linked: false, version: 3 })),
    );
    expect((await memax.v2.machines.list("personal")).items).toHaveLength(1);
    expect(call(0).url).toBe(
      "https://api.memax.app/v2/spaces/personal/machines",
    );

    const renamed = await memax.v2.machines.rename(
      machine.id,
      { name: "Work laptop" },
      { idempotencyKey: "k-2", ifMatch: 1 },
    );
    expect(renamed.machine.name).toBe("Work laptop");
    expect(call(1).method).toBe("PATCH");
    expect(call(1).url).toBe(`https://api.memax.app/v2/machines/${machine.id}`);
    expect(call(1).headers["If-Match"]).toBe('"1"');

    const off = await memax.v2.machines.unlink(
      machine.id,
      {},
      { idempotencyKey: "k-3" },
    );
    expect(off.machine.linked).toBe(false);
    expect(call(2).url).toBe(
      `https://api.memax.app/v2/machines/${machine.id}:unlink`,
    );
  });

  it("reads one machine's home files, as its daemon polls them", async () => {
    const { memax, call } = client(jsonResponse({ data: { items: [] } }));
    await memax.v2.targets.list("personal", { machine: machine.id });
    expect(call().url).toBe(
      `https://api.memax.app/v2/spaces/personal/targets?machine=${machine.id}`,
    );
    await memax.v2.targets.list("personal");
    expect(call(1).url).toBe(
      "https://api.memax.app/v2/spaces/personal/targets",
    );
  });
});
