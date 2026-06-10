import { describe, it, expect } from "vitest";
import type { Hub, HubWithRole } from "memax-sdk";
import { resolveHubFromSlug, hubRouteSlug, isUUIDLike } from "./hub-from-slug";

const HUB_PERSONAL_ID = "11111111-2222-3333-4444-555555555555";
const HUB_ENGINEERING_ID = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee";
const HUB_DESIGN_ID = "12345678-90ab-cdef-1234-567890abcdef";
const USER_A_ID = "ffffffff-eeee-dddd-cccc-bbbbbbbbbbbb";

function makeHub(overrides: Partial<Hub>): Hub {
  return {
    id: HUB_PERSONAL_ID,
    name: "Hub",
    slug: "hub",
    hub_type: "personal",
    owner_id: USER_A_ID,
    ...overrides,
  };
}

function makeWithRole(hub: Hub): HubWithRole {
  return { hub, role: "owner", memory_count: 0 };
}

describe("isUUIDLike", () => {
  it("accepts canonical RFC 4122 hex with hyphens", () => {
    expect(isUUIDLike(HUB_PERSONAL_ID)).toBe(true);
    expect(isUUIDLike(HUB_PERSONAL_ID.toUpperCase())).toBe(true);
  });
  it("rejects slugs that aren't UUID-shaped", () => {
    expect(isUUIDLike("personal")).toBe(false);
    expect(isUUIDLike("engineering")).toBe(false);
    expect(isUUIDLike("11111111-2222-3333-4444-55555555555")).toBe(false); // 11 chars in last group
    expect(isUUIDLike("")).toBe(false);
  });
});

describe("resolveHubFromSlug", () => {
  const hubs: HubWithRole[] = [
    makeWithRole(
      makeHub({
        id: HUB_PERSONAL_ID,
        slug: "personal-jiahao",
        hub_type: "personal",
        owner_id: USER_A_ID,
      }),
    ),
    makeWithRole(
      makeHub({
        id: HUB_ENGINEERING_ID,
        slug: "engineering",
        hub_type: "team",
      }),
    ),
    makeWithRole(
      makeHub({ id: HUB_DESIGN_ID, slug: "design-craft", hub_type: "team" }),
    ),
  ];

  it("resolves the literal `personal` alias to the user's own personal hub", () => {
    expect(resolveHubFromSlug({ slug: "personal", hubs })).toBe(
      HUB_PERSONAL_ID,
    );
  });

  it("resolves a team-hub slug to its UUID", () => {
    expect(resolveHubFromSlug({ slug: "engineering", hubs })).toBe(
      HUB_ENGINEERING_ID,
    );
    expect(resolveHubFromSlug({ slug: "design-craft", hubs })).toBe(
      HUB_DESIGN_ID,
    );
  });

  it("resolves a UUID-shaped slug by id (deep-link compat)", () => {
    expect(resolveHubFromSlug({ slug: HUB_ENGINEERING_ID, hubs })).toBe(
      HUB_ENGINEERING_ID,
    );
  });

  it("returns null for an unknown slug (route 404 path)", () => {
    expect(resolveHubFromSlug({ slug: "nonexistent-team", hubs })).toBeNull();
  });

  it("returns null for an empty slug", () => {
    expect(resolveHubFromSlug({ slug: "", hubs })).toBeNull();
  });

  it("returns null when the hub list is empty", () => {
    expect(resolveHubFromSlug({ slug: "personal", hubs: [] })).toBeNull();
  });

  it("does NOT resolve `personal` against the personal hub's literal slug field", () => {
    // The literal `personal` alias takes priority — we never want to leak
    // another personal hub's slug field into the alias path.
    const personalWithCustomSlug = makeWithRole(
      makeHub({
        id: HUB_PERSONAL_ID,
        slug: "personal-jiahao",
        hub_type: "personal",
      }),
    );
    expect(
      resolveHubFromSlug({ slug: "personal", hubs: [personalWithCustomSlug] }),
    ).toBe(HUB_PERSONAL_ID);
    // The custom slug also resolves directly — that's the by-slug path,
    // not the alias.
    expect(
      resolveHubFromSlug({
        slug: "personal-jiahao",
        hubs: [personalWithCustomSlug],
      }),
    ).toBe(HUB_PERSONAL_ID);
  });

  it("returns null for a UUID that the caller is not a member of (no info leak)", () => {
    const stranger = "99999999-9999-9999-9999-999999999999";
    expect(resolveHubFromSlug({ slug: stranger, hubs })).toBeNull();
  });

  it("falls back to slug match when a UUID-shaped segment matches a hub.slug instead of hub.id", () => {
    // Production assigns personal_hub.slug = user.id (auth.go:1735), so a
    // UUID-shaped URL segment is sometimes a slug, not a hub id. The
    // resolver must try ID first, then fall through to slug.
    const personalWithUserIdSlug = makeWithRole(
      makeHub({
        id: HUB_PERSONAL_ID,
        slug: USER_A_ID, // UUID-shaped slug
        hub_type: "personal",
        owner_id: USER_A_ID,
      }),
    );
    expect(
      resolveHubFromSlug({ slug: USER_A_ID, hubs: [personalWithUserIdSlug] }),
    ).toBe(HUB_PERSONAL_ID);
  });

  it("matches hub.id case-insensitively (UUID hex is case-insensitive per RFC 4122)", () => {
    const upperSlug = HUB_ENGINEERING_ID.toUpperCase();
    expect(resolveHubFromSlug({ slug: upperSlug, hubs })).toBe(
      HUB_ENGINEERING_ID,
    );
  });

  it("resolves a UUID-shaped team-hub slug via the slug fallback", () => {
    const teamWithUuidSlug = makeWithRole(
      makeHub({
        id: HUB_DESIGN_ID,
        slug: HUB_PERSONAL_ID, // arbitrary UUID-shaped slug; not THIS hub's id
        hub_type: "team",
      }),
    );
    expect(
      resolveHubFromSlug({
        slug: HUB_PERSONAL_ID,
        hubs: [teamWithUuidSlug],
      }),
    ).toBe(HUB_DESIGN_ID);
  });
});

describe("hubRouteSlug", () => {
  it("returns `personal` for the caller's own personal hub", () => {
    const hub = makeHub({ hub_type: "personal", owner_id: USER_A_ID });
    expect(hubRouteSlug(hub, USER_A_ID)).toBe("personal");
  });

  it("returns the literal slug for team hubs", () => {
    const hub = makeHub({ hub_type: "team", slug: "engineering" });
    expect(hubRouteSlug(hub, USER_A_ID)).toBe("engineering");
  });

  it("returns the literal slug for someone else's personal hub", () => {
    // Defensive: today personal hubs have one member so this case can't
    // happen, but the helper stays correct for forward-compat.
    const otherUser = "00000000-0000-0000-0000-000000000001";
    const hub = makeHub({
      hub_type: "personal",
      owner_id: otherUser,
      slug: "alice-personal",
    });
    expect(hubRouteSlug(hub, USER_A_ID)).toBe("alice-personal");
  });
});
