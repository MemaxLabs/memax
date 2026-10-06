import { describe, expect, it } from "vitest";
import { V2_SPACE_PLACES } from "@/lib/ui-gate";
import {
  PLACE_ROUTES,
  placeHref,
  railOfRoute,
  railPlaces,
  routeOfRail,
  spaceHasPlace,
  switchHref,
} from "./places";

describe("places", () => {
  it("rails the six places in HANDOFF §6's order", () => {
    expect(railPlaces("project")).toEqual([
      "today",
      "review",
      "briefs",
      "memories",
      "handoffs",
      "agents",
    ]);
    expect(railPlaces("personal")).toEqual(railPlaces("project"));
  });

  it("adds Decisions after Memories in a team space", () => {
    expect(railPlaces("team")).toEqual([
      "today",
      "review",
      "briefs",
      "memories",
      "decisions",
      "handoffs",
      "agents",
    ]);
  });

  it("routes every place under /[space]/ that the UI gate knows", () => {
    for (const route of PLACE_ROUTES) {
      expect(V2_SPACE_PLACES as readonly string[]).toContain(route);
    }
    expect(routeOfRail("briefs")).toBe("brief");
    expect(railOfRoute("brief")).toBe("briefs");
    expect(railOfRoute("activity")).toBeUndefined();
    expect(placeHref("memax-v2", "review")).toBe("/memax-v2/review");
  });

  it("switches to the same place, or Today when the space hasn't got it", () => {
    expect(switchHref({ slug: "personal", kind: "personal" }, "review")).toBe(
      "/personal/review",
    );
    expect(switchHref({ slug: "memax-v2", kind: "project" }, "decisions")).toBe(
      "/memax-v2/today",
    );
    expect(switchHref({ slug: "memax-team", kind: "team" }, "decisions")).toBe(
      "/memax-team/decisions",
    );
    expect(switchHref({ slug: "memax-v2", kind: "project" }, undefined)).toBe(
      "/memax-v2/today",
    );
    expect(spaceHasPlace("project", "decisions")).toBe(false);
  });
});
