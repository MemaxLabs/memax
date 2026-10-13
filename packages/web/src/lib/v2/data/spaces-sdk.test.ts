import { describe, expect, it, vi } from "vitest";
import { createDemoSource } from "./demo-source";
import { createSdkSource } from "./sdk-source";

// The spaces list's counts and making a space, through the SDK source
// (memax.v2.spaces) and the demo's.

function setup(viewerId = "u1") {
  const client = {
    v2: {
      spaces: {
        list: vi.fn().mockResolvedValue({
          items: [
            {
              id: "s1",
              tenant_id: "t1",
              slug: "memax-v2",
              name: "memax-v2",
              kind: "project",
              role: "owner",
              v2_enabled_at: "2026-10-01T00:00:00Z",
              created_at: "2026-09-30T00:00:00Z",
              counts: { kept: 214, waiting: 5, agents: 4 },
            },
            {
              id: "s2",
              tenant_id: "t1",
              slug: "acme-web",
              name: "acme-web",
              kind: "team",
              role: "owner",
            },
          ],
        }),
        create: vi.fn().mockResolvedValue({
          id: "s3",
          tenant_id: "t1",
          slug: "acme-api",
          name: "acme-api",
          kind: "project",
          role: "owner",
          repository: "MemaxLabs/acme-api",
          v2_enabled_at: "2026-10-05T21:40:00Z",
          created_at: "2026-10-05T21:40:00Z",
        }),
      },
    },
  };
  const source = createSdkSource({
    client: client as never,
    viewer: { id: viewerId, initials: "ZZ", name: "Ziyang", timeZone: "UTC" },
  });
  return { client, source };
}

describe("the SDK source's spaces", () => {
  it("carries each V2 space's counts for the switcher, none for one on V1", async () => {
    const { source } = setup();
    const [v2, v1] = await source.spaces();
    expect(v2).toMatchObject({
      onV2: true,
      kept: 214,
      waiting: 5,
      agents: 4,
      people: null,
      createdAt: "2026-09-30T00:00:00Z",
    });
    expect(v1).toMatchObject({
      onV2: false,
      kept: null,
      waiting: null,
      agents: null,
    });
  });

  it("makes a space through memax.v2.spaces.create, empty", async () => {
    const { client, source } = setup();
    const made = await source.createSpace({
      name: "acme-api",
      kind: "project",
      repository: "MemaxLabs/acme-api",
      idempotencyKey: "k1",
    });
    expect(client.v2.spaces.create).toHaveBeenCalledWith(
      { name: "acme-api", kind: "project", repository: "MemaxLabs/acme-api" },
      { idempotencyKey: "k1" },
    );
    expect(made).toMatchObject({
      slug: "acme-api",
      onV2: true,
      kept: 0,
      waiting: 0,
      agents: 0,
    });
    // No repository: none is sent.
    await source.createSpace({
      name: "notes",
      kind: "project",
      repository: "",
      idempotencyKey: "k2",
    });
    expect(client.v2.spaces.create).toHaveBeenLastCalledWith(
      { name: "notes", kind: "project" },
      { idempotencyKey: "k2" },
    );
  });
});

describe("the demo's spaces", () => {
  it("makes project spaces with the API's slugs, and refuses the rest", async () => {
    const demo = createDemoSource({ commandDelayMs: 0 });
    const made = await demo.createSpace({
      name: "memax-web",
      kind: "project",
      repository: "",
      idempotencyKey: "k1",
    });
    // memax-web is the dataset's: the next free one.
    expect(made.slug).toBe("memax-web-2");
    expect((await demo.spaces()).at(-1)?.slug).toBe("memax-web-2");
    // The same key is the same space.
    expect(
      (
        await demo.createSpace({
          name: "memax-web",
          kind: "project",
          repository: "",
          idempotencyKey: "k1",
        })
      ).id,
    ).toBe(made.id);
    expect(demo.peek?.overview("memax-web-2")?.memories.any).toBe(false);
    for (const [kind, code] of [
      ["personal", "personal_exists"],
      ["team", "space_kind"],
    ] as const) {
      await expect(
        demo.createSpace({
          name: "x",
          kind,
          repository: "",
          idempotencyKey: kind,
        }),
      ).rejects.toMatchObject({ failure: { kind: "refused", code } });
    }
  });
});
