import { describe, expect, it, vi } from "vitest";
import { Memax } from "../index.js";
import type { V2 } from "../index.js";

function client(data: unknown, status = 200) {
  const fetchMock = vi.fn(
    async () =>
      new Response(JSON.stringify({ data }), {
        status,
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

const space: V2.Space = {
  id: "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5c",
  tenant_id: "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5d",
  slug: "acme-web",
  name: "Acme web",
  kind: "project",
  role: "owner",
  repository: "acme/web",
  v2_enabled_at: "2026-10-06T09:30:00Z",
};

const imp: V2.Import = {
  id: "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a70",
  space_id: space.id,
  tenant_id: space.tenant_id,
  actor_kind: "person",
  files: [
    {
      path: "CLAUDE.md",
      kind: "claude_md",
      location: "repository",
      statements: 2,
      skipped: 0,
      hidden_characters: 0,
    },
  ],
  skipped: [],
  counts: {
    items: 2,
    proposed: 1,
    folded: 1,
    existing: 0,
    refused: 0,
    conflicts: 0,
  },
  check: { state: "pending" },
  origin: "init",
  created_at: "2026-10-06T09:30:00Z",
};

describe("memax.v2 spaces, imports and bulk review", () => {
  it("creates a space and switches one, as commands", async () => {
    const { memax, call } = client(space, 201);
    const got = await memax.v2.spaces.create(
      { name: "Acme web", repository: "acme/web" },
      { idempotencyKey: "k1", via: "cli" },
    );
    expect(got.slug).toBe("acme-web");
    expect(call()).toMatchObject({
      url: "https://api.memax.app/v2/spaces",
      method: "POST",
      body: { name: "Acme web", repository: "acme/web" },
    });
    expect(call().headers["Idempotency-Key"]).toBe("k1");
    expect(call().headers["X-Memax-Via"]).toBe("cli");

    const sw = client({ space, state: "switched" });
    await sw.memax.v2.spaces.switchToV2("personal", { idempotencyKey: "k2" });
    expect(sw.call()).toMatchObject({
      url: "https://api.memax.app/v2/spaces/personal:switch",
      method: "POST",
      body: { to: "v2" },
    });
  });

  it("uploads an import, reads it and settles a disagreement", async () => {
    const result: V2.ImportResult = {
      import: imp,
      items: [
        {
          key: "a",
          position: 0,
          ref: "CLAUDE.md:12",
          outcome: "proposed",
          memory: { id: imp.id, ref: "M-0003" },
        },
      ],
    };
    const up = client(result, 201);
    const input: V2.ImportInput = {
      client: "memax-cli 0.3.0",
      files: imp.files,
      skipped: [
        { ref: "CLAUDE.md:31", reason: "secret", detail: "GitHub token" },
      ],
      items: [
        {
          key: "a",
          location: "repository",
          statement: "Run tests with `pnpm test`.",
          section: "conventions",
          sources: [{ kind: "file", ref: "CLAUDE.md:12", trust: "repository" }],
        },
      ],
    };
    const out = await up.memax.v2.imports.create("acme-web", input, {
      idempotencyKey: "init-1",
    });
    expect(out.items[0].outcome).toBe("proposed");
    expect(up.call()).toMatchObject({
      url: "https://api.memax.app/v2/spaces/acme-web/imports",
      body: input,
    });

    const view: V2.ImportView = {
      import: imp,
      items: [],
      memories: [],
      conflicts: [],
      progress: { proposals: 1, working: 0, judged: 1, failed: 0, ready: true },
    };
    const get = client(view);
    expect(
      (await get.memax.v2.imports.get("acme-web", imp.id)).progress.ready,
    ).toBe(true);
    expect(get.call().url).toBe(
      `https://api.memax.app/v2/spaces/acme-web/imports/${imp.id}`,
    );

    const list = client({ items: [imp], has_more: false });
    await list.memax.v2.imports.list("acme-web", { limit: 5 });
    expect(list.call().url).toBe(
      "https://api.memax.app/v2/spaces/acme-web/imports?limit=5",
    );

    const settle = client({
      outcome: "applied",
      policy: { effect: "apply" },
      conflict: {
        id: imp.id,
        n: 1,
        members: [
          { id: imp.id, ref: "M-0003" },
          { id: imp.id, ref: "M-0004" },
        ],
        state: "settled",
        choice: "keep_one",
        created_receipt_id: imp.id,
        last_receipt_id: imp.id,
        created_at: imp.created_at,
      },
      memories: [],
      receipts: [],
    });
    await settle.memax.v2.imports.settle(
      "acme-web",
      imp.id,
      1,
      { choice: "keep_one", keep: "M-0003" },
      { idempotencyKey: "s1", via: "cli" },
    );
    expect(settle.call()).toMatchObject({
      url: `https://api.memax.app/v2/spaces/acme-web/imports/${imp.id}/conflicts/1:settle`,
      body: { choice: "keep_one", keep: "M-0003" },
    });
  });

  it("keeps and rejects in bulk", async () => {
    const res: V2.BulkReviewResult = {
      items: [
        {
          memory: "M-0003",
          ref: "M-0003",
          outcome: "applied",
          state: "kept",
          version: 1,
        },
      ],
      applied: 1,
      refused: 0,
      failed: 0,
    };
    const keep = client(res);
    const got = await keep.memax.v2.memories.keepMany(
      "acme-web",
      { items: [{ memory: "M-0003", version: 1 }] },
      { idempotencyKey: "b1", via: "cli" },
    );
    expect(got.applied).toBe(1);
    expect(keep.call()).toMatchObject({
      url: "https://api.memax.app/v2/spaces/acme-web/memories:keep",
      method: "POST",
    });
    const reject = client(res);
    await reject.memax.v2.memories.rejectMany(
      "acme-web",
      { items: [{ memory: "M-0003" }] },
      { idempotencyKey: "b2" },
    );
    expect(reject.call().url).toBe(
      "https://api.memax.app/v2/spaces/acme-web/memories:reject",
    );
  });
});
