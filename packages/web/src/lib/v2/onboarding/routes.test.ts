import { describe, expect, it } from "vitest";
import { DEMO_SPACES } from "../data/demo-dataset";
import { createDemoSource } from "../data/demo-source";
import { demoImportView } from "../data/demo-imports-data";
import type { ImportView } from "../data/imports";
import type { SpaceSummary } from "../data/types";
import {
  afterSignIn,
  decideLanding,
  landingHref,
  resolveLanding,
  reviewImportHref,
  safeNext,
  setupHref,
  signInHref,
} from "./routes";

const NOW = new Date("2026-10-05T14:40:00-07:00");

function space(slug: string, more: Partial<SpaceSummary> = {}): SpaceSummary {
  return {
    id: slug,
    slug,
    name: slug,
    kind: "project",
    role: "owner",
    kept: null,
    agents: null,
    people: null,
    waiting: null,
    onV2: true,
    ...more,
  };
}

function settledEverything(view: ImportView): ImportView {
  return {
    ...view,
    memories: view.memories.map((m) => ({
      ...m,
      held: "decided",
      bulk: false,
      state: "kept",
    })),
    conflicts: view.conflicts.map((c) => ({ ...c, state: "settled" })),
  };
}

describe("where a sign-in goes on, by the person's web UI", () => {
  it("takes a person with the V2 UI flag to `next`, else their landing", () => {
    expect(afterSignIn("/memax-v2/review?filter=import", "v2")).toEqual({
      kind: "next",
      href: "/memax-v2/review?filter=import",
    });
    expect(afterSignIn("/device?code=WQRT-4821", "v2")).toEqual({
      kind: "next",
      href: "/device?code=WQRT-4821",
    });
    expect(afterSignIn(null, "v2")).toEqual({ kind: "landing" });
    expect(afterSignIn("https://evil.example/", "v2")).toEqual({
      kind: "landing",
    });
  });

  it("takes a person without it to V1's home, never into V2", () => {
    for (const ui of ["v1", null, undefined] as const) {
      expect(afterSignIn(null, ui)).toEqual({ kind: "v1", href: "/home" });
      for (const next of [
        "/memax-v2/today",
        "/setup/import?space=memax-v2",
        "/settings/account",
        "/join/abc",
      ]) {
        expect(afterSignIn(next, ui)).toEqual({ kind: "v1", href: "/home" });
      }
    }
  });

  it("still takes a person without it where every browser may go", () => {
    // The CLI's device code and an agent's OAuth request open for every
    // browser; a V1 page is theirs.
    for (const next of [
      "/device?code=WQRT-4821",
      "/oauth/authorize?request=r1",
      "/h/personal/memories",
      "/settings",
    ]) {
      expect(afterSignIn(next, "v1")).toEqual({ kind: "next", href: next });
    }
    expect(afterSignIn("//evil.example", "v1")).toEqual({
      kind: "v1",
      href: "/home",
    });
  });
});

describe("where a person lands after signing in", () => {
  it("starts someone with no space on the V2 record at FirstRun", () => {
    const personal = space("personal", { kind: "personal", onV2: false });
    expect(decideLanding([personal], [], NOW)).toEqual({ kind: "first-run" });
    expect(decideLanding([], [], NOW)).toEqual({ kind: "first-run" });
    expect(landingHref({ kind: "first-run" })).toBe("/setup/import");
  });

  it("brings someone back to an import still in progress", () => {
    const v2 = space("memax-v2");
    const view = demoImportView();
    expect(
      decideLanding(
        [space("personal", { kind: "personal" }), v2],
        [{ space: v2, view }],
        NOW,
      ),
    ).toEqual({
      kind: "review-import",
      space: "memax-v2",
      importId: view.summary.id,
    });
    expect(
      landingHref({ kind: "review-import", space: "memax-v2", importId: "i1" }),
    ).toBe("/memax-v2/review?filter=import&import=i1");
  });

  it("opens a project space's Today once the import is decided, or long past", () => {
    const personal = space("personal", { kind: "personal" });
    const v2 = space("memax-v2");
    const done = settledEverything(demoImportView());
    expect(
      decideLanding([personal, v2], [{ space: v2, view: done }], NOW),
    ).toEqual({
      kind: "today",
      space: "memax-v2",
    });
    const old = demoImportView();
    old.summary.createdAt = "2026-09-01T09:00:00Z";
    expect(
      decideLanding([personal, v2], [{ space: v2, view: old }], NOW),
    ).toEqual({
      kind: "today",
      space: "memax-v2",
    });
    expect(decideLanding([personal], [], NOW)).toEqual({
      kind: "today",
      space: "personal",
    });
  });

  it("prefers the newest import in progress", () => {
    const a = space("a");
    const b = space("b");
    const older = demoImportView();
    older.summary.id = "older";
    older.summary.createdAt = "2026-10-04T09:00:00Z";
    const newer = demoImportView();
    newer.summary.id = "newer";
    expect(
      decideLanding(
        [a, b],
        [
          { space: a, view: older },
          { space: b, view: newer },
        ],
        NOW,
      ),
    ).toMatchObject({
      space: "b",
      importId: "newer",
    });
  });

  it("reads the spaces and their newest imports from the source", async () => {
    const demo = createDemoSource({ streamDelayMs: 0, commandDelayMs: 0 });
    expect(await resolveLanding(demo)).toEqual({
      kind: "review-import",
      space: "memax-v2",
      importId: demoImportView().summary.id,
    });
    // A space whose imports fail to load counts as having none.
    const failing = {
      ...demo,
      spaces: async () => [...DEMO_SPACES],
      imports: {
        list: async () => {
          throw new Error("down");
        },
        get: async () => null,
      },
    };
    expect(await resolveLanding(failing)).toEqual({
      kind: "today",
      space: "memax-v2",
    });
  });
});

describe("the first session's links", () => {
  it.each([
    ["/device?code=WQRT-4821", "/device?code=WQRT-4821"],
    ["/memax-v2/today", "/memax-v2/today"],
    ["https://evil.example/", null],
    ["//evil.example/x", null],
    ["/\\evil.example", null],
    ["device", null],
    ["/signin?next=/x", null],
    ["/signin/callback", null],
    [null, null],
  ])("keeps %j as a next of %j", (next, want) => {
    expect(safeNext(next)).toBe(want);
  });

  it("builds the setup and sign-in paths", () => {
    expect(setupHref("cleanup", { space: "memax-v2", import: "i 1" })).toBe(
      "/setup/cleanup?space=memax-v2&import=i+1",
    );
    expect(setupHref("import")).toBe("/setup/import");
    expect(reviewImportHref("memax-v2")).toBe("/memax-v2/review?filter=import");
    expect(signInHref("/device?code=WQRT-4821")).toBe(
      "/signin?next=%2Fdevice%3Fcode%3DWQRT-4821",
    );
    expect(signInHref("/memax-v2/agents", { again: true })).toBe(
      "/signin?next=%2Fmemax-v2%2Fagents&again=1",
    );
    expect(signInHref("https://evil.example")).toBe("/signin");
  });
});
