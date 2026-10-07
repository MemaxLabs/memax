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
  kind: "team",
  role: "owner",
};

const preview: V2.SwitchPreview = {
  kind: "team",
  kinds: ["team", "project"],
  members: [
    {
      person_id: space.tenant_id,
      name: "Ziyang",
      v1_role: "admin",
      role: "member",
      can_forget: true,
    },
  ],
  notes: {
    total: 112,
    person: 100,
    agent: 12,
    candidates: 90,
    fold: 20,
    kept: 2,
    long: 8,
    secret: 1,
    archived: 1,
    format: 0,
    external: 2,
    seeds: 5,
  },
  personas: 0,
  configs: [],
  targets: [],
  agents: [],
  gates: 1,
  dream_runs: 4,
  plan: "pro",
  empty: false,
};

const running: V2.SpaceSwitch = {
  space,
  state: "running",
  step: "candidates",
  preview,
  progress: {
    notes: 112,
    personas: 0,
    configs: 0,
    targets: [],
    proposed: 0,
    folded: 0,
    existing: 0,
    refused: 0,
    imports: [],
    connected: 0,
    already_connected: 0,
    notified: 0,
    gates_moved: 0,
    gates_left: 0,
  },
  attempts: 1,
  background: true,
};

describe("memax.v2 Switch to V2", () => {
  it("reads the preview, switches as a project and switches back", async () => {
    const st = client(running);
    const got = await st.memax.v2.spaces.switchStatus("acme-web");
    expect(got.preview.notes.candidates).toBe(90);
    expect(st.call()).toMatchObject({
      url: "https://api.memax.app/v2/spaces/acme-web/switch",
      method: "GET",
    });

    const sw = client(running, 202);
    const res = await sw.memax.v2.spaces.switchToV2("acme-web", {
      idempotencyKey: "k1",
      via: "cli",
      kind: "project",
      repository: "acme/web",
    });
    expect(res.background).toBe(true);
    expect(sw.call()).toMatchObject({
      url: "https://api.memax.app/v2/spaces/acme-web:switch",
      method: "POST",
      body: { to: "v2", kind: "project", repository: "acme/web" },
    });
    expect(sw.call().headers["Idempotency-Key"]).toBe("k1");

    const back = client({ ...running, state: "off", background: false });
    await back.memax.v2.spaces.switchToV1("acme-web", {
      idempotencyKey: "k2",
    });
    expect(back.call()).toMatchObject({
      url: "https://api.memax.app/v2/spaces/acme-web:switch",
      body: { to: "v1" },
    });

    const runs = client({ items: [] });
    await runs.memax.v2.spaces.v1DreamRuns("acme-web");
    expect(runs.call().url).toBe(
      "https://api.memax.app/v2/spaces/acme-web/v1-dream-runs",
    );
  });

  it("searches, reads and forgets notes", async () => {
    const note: V2.Note = {
      id: "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a90",
      ref: "N-0042",
      space_id: space.id,
      owner_id: space.tenant_id,
      origin: "memory",
      title: "Deploys",
      excerpt: "We deploy on Fridays.",
      author_kind: "person",
      state: "active",
      disposition: "candidate",
      length: 21,
      created_at: "2026-10-06T09:30:00Z",
      updated_at: "2026-10-06T09:30:00Z",
    };
    const s = client({ items: [note] });
    const page = await s.memax.v2.notes.search("acme-web", {
      q: "deploy",
      limit: 5,
    });
    expect(page.items[0].ref).toBe("N-0042");
    expect(s.call().url).toBe(
      "https://api.memax.app/v2/spaces/acme-web/notes?q=deploy&limit=5",
    );

    const g = client(note);
    await g.memax.v2.notes.get("acme-web", "N-0042");
    expect(g.call().url).toBe(
      "https://api.memax.app/v2/spaces/acme-web/notes/N-0042",
    );

    const pv = client({ ref: "N-0042", carries: [], allowed: true });
    await pv.memax.v2.notes.previewForget("acme-web", "N-0042");
    expect(pv.call().url).toBe(
      "https://api.memax.app/v2/spaces/acme-web/notes/N-0042/forget-preview",
    );

    const f = client({});
    await f.memax.v2.notes.forget(
      "acme-web",
      "N-0042",
      { carries: ["M-0007"] },
      { idempotencyKey: "k3" },
    );
    expect(f.call()).toMatchObject({
      url: "https://api.memax.app/v2/spaces/acme-web/notes/N-0042:forget",
      method: "POST",
      body: { carries: ["M-0007"] },
    });
  });
});
