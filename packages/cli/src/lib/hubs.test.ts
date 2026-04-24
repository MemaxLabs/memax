import { describe, expect, it } from "vitest";
import { findHubMatch, getHubReference, PERSONAL_HUB_ALIAS } from "./hubs.js";

describe("getHubReference", () => {
  it("uses the reserved personal alias for personal hubs", () => {
    expect(
      getHubReference({
        id: "h1",
        name: "Personal",
        slug: "user-123",
        hub_type: "personal",
        owner_id: "u1",
      }),
    ).toBe(PERSONAL_HUB_ALIAS);
  });

  it("uses the real slug for team hubs", () => {
    expect(
      getHubReference({
        id: "h2",
        name: "Memax",
        slug: "memax",
        hub_type: "team",
        owner_id: "u1",
      }),
    ).toBe("memax");
  });
});

describe("findHubMatch", () => {
  const hubs = [
    {
      hub: {
        id: "personal-id",
        name: "Personal",
        slug: "user-123",
        hub_type: "personal",
        owner_id: "u1",
      },
      role: "owner",
      memory_count: 0,
    },
    {
      hub: {
        id: "team-id",
        name: "Memax",
        slug: "memax",
        hub_type: "team",
        owner_id: "u1",
      },
      role: "owner",
      memory_count: 0,
    },
  ] as const;

  it("resolves the reserved personal alias to the personal hub", () => {
    expect(findHubMatch([...hubs], "personal")?.hub.id).toBe("personal-id");
  });

  it("matches team hubs by slug", () => {
    expect(findHubMatch([...hubs], "memax")?.hub.id).toBe("team-id");
  });

  it("matches hubs by id", () => {
    expect(findHubMatch([...hubs], "team-id")?.hub.slug).toBe("memax");
  });
});
