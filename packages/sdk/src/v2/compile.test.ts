import { describe, expect, it, vi } from "vitest";
import { Memax } from "../index.js";
import type { V2 } from "../index.js";

function client(body: unknown) {
  const fetchMock = vi.fn(
    async () =>
      new Response(JSON.stringify({ data: body }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
  );
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

const targetID = "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a60";

describe("memax.v2.briefs", () => {
  it("reads, revises with If-Match and lists versions", async () => {
    const brief: V2.Brief = {
      id: "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a61",
      version_id: "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a62",
      ref: "B-0043",
      version: 3,
      space_id: "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5c",
      tenant_id: "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5d",
      title: "Memax V2 engineering brief",
      sections: [
        { key: "decisions", heading: "Decisions", items: [{ ref: "M-0219" }] },
      ],
      facts: 1,
      current: true,
      receipt_id: "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a63",
      created_at: "2026-10-06T09:30:00Z",
    };
    const { memax, call } = client(brief);

    expect((await memax.v2.briefs.get("memax-v2")).ref).toBe("B-0043");
    const input: V2.ReviseBriefInput = {
      title: brief.title,
      sections: [
        {
          key: "open",
          heading: "Open",
          items: [{ text: "Fly.io or Railway?", cites: ["M-0431", "M-0174"] }],
        },
      ],
    };
    await memax.v2.briefs.revise("memax-v2", input, {
      idempotencyKey: "b-1",
      ifMatch: 3,
    });
    await memax.v2.briefs.versions("memax-v2", { limit: 5 });

    expect(call(0).url).toBe("https://api.memax.app/v2/spaces/memax-v2/brief");
    const revise = call(1);
    expect(revise.method).toBe("POST");
    expect(revise.headers["If-Match"]).toBe('"3"');
    expect(revise.headers["Idempotency-Key"]).toBe("b-1");
    expect(revise.body).toEqual(input);
    expect(call(2).url).toBe(
      "https://api.memax.app/v2/spaces/memax-v2/brief/versions?limit=5",
    );
  });
});

describe("memax.v2.targets", () => {
  it("adds, changes and compiles targets", async () => {
    const { memax, call } = client({});

    await memax.v2.targets.list("memax-v2");
    await memax.v2.targets.create(
      "memax-v2",
      { kind: "cursor_mdc" },
      { idempotencyKey: "t-1" },
    );
    await memax.v2.targets.update(
      targetID,
      { settings: { include: "kept_only" } },
      { idempotencyKey: "t-2", ifMatch: 2 },
    );
    await memax.v2.targets.compile(targetID, {}, { idempotencyKey: "t-3" });

    expect(call(0).url).toBe(
      "https://api.memax.app/v2/spaces/memax-v2/targets",
    );
    expect(call(1).method).toBe("POST");
    expect(call(1).body).toEqual({ kind: "cursor_mdc" });
    const update = call(2);
    expect(update.method).toBe("PATCH");
    expect(update.url).toBe(`https://api.memax.app/v2/targets/${targetID}`);
    expect(update.headers["If-Match"]).toBe('"2"');
    expect(update.headers["Idempotency-Key"]).toBe("t-2");
    expect(call(3).url).toBe(
      `https://api.memax.app/v2/targets/${targetID}:compile`,
    );
  });

  it("previews, reports, acknowledges and resolves hand edits", async () => {
    const { memax, call } = client({});

    await memax.v2.targets.preview(targetID);
    await memax.v2.targets.runs(targetID, { limit: 3 });
    await memax.v2.targets.observe(
      targetID,
      { path: "AGENTS.md", content: "# Brief\n", device_id: "zz-laptop" },
      { idempotencyKey: "o-1", via: "cli" },
    );
    await memax.v2.targets.deliver(
      targetID,
      { compile: "C-0881", sha256: "a".repeat(64) },
      { idempotencyKey: "d-1", via: "cli" },
    );
    await memax.v2.targets.drift(targetID);
    await memax.v2.targets.pull(targetID, {}, { idempotencyKey: "r-1" });
    await memax.v2.targets.overwrite(targetID, {}, { idempotencyKey: "r-2" });
    await memax.v2.targets.stop(targetID, {}, { idempotencyKey: "r-3" });

    const base = `https://api.memax.app/v2/targets/${targetID}`;
    expect(call(0).url).toBe(`${base}/preview`);
    expect(call(1).url).toBe(`${base}/runs?limit=3`);
    expect(call(2).url).toBe(`${base}/observations`);
    expect(call(2).headers["X-Memax-Via"]).toBe("cli");
    expect(call(3).url).toBe(`${base}/deliveries`);
    expect(call(3).body).toEqual({
      compile: "C-0881",
      sha256: "a".repeat(64),
    });
    expect(call(4).url).toBe(`${base}/drift`);
    expect(call(5).url).toBe(`${base}/drift:pull`);
    expect(call(6).url).toBe(`${base}/drift:overwrite`);
    expect(call(7).url).toBe(`${base}/drift:stop`);
    expect(call(7).headers["Idempotency-Key"]).toBe("r-3");
  });
});
