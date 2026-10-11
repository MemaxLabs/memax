import { describe, expect, it, vi } from "vitest";
import { DEMO_SPACES } from "../data/demo-dataset";
import { createDemoSource } from "../data/demo-source";
import { demoImportView } from "../data/demo-imports-data";
import { DEMO_SWITCH_PREVIEW, DEMO_V1_SPACE } from "../data/demo-switch-data";
import type { ImportView } from "../data/imports";
import { holdsV1Work, type SwitchPreviewView } from "../data/switch";
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
  skipSpace,
  switchesToRead,
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

describe("skipping the first run before init has made a space", () => {
  it("opens a space still on V1 the person owns, where they can switch it", () => {
    const shared = space("acme", { kind: "team", role: "member", onV2: false });
    const personal = space("personal", { kind: "personal", onV2: false });
    const team = space("acme-web", { kind: "team", onV2: false });
    expect(skipSpace([shared, personal])?.slug).toBe("personal");
    expect(skipSpace([shared])?.slug).toBe("acme");
    // A team space of theirs before Personal, as the landing has it.
    expect(skipSpace([personal, shared, team])?.slug).toBe("acme-web");
  });

  it("has nowhere to go for someone with no space at all", () => {
    expect(skipSpace([])).toBeNull();
  });
});

/** A switch preview of a V1 space: nothing in it, then `more`. */
function preview(more: Partial<SwitchPreviewView> = {}): SwitchPreviewView {
  return {
    ...DEMO_SWITCH_PREVIEW,
    kind: "personal",
    kinds: ["personal"],
    members: DEMO_SWITCH_PREVIEW.members.slice(0, 1),
    notes: {
      total: 0,
      candidates: 0,
      fold: 0,
      kept: 0,
      archived: 0,
      secret: 0,
    },
    personas: 0,
    configs: 0,
    targets: [],
    agents: [],
    gates: 0,
    dreamRuns: 0,
    ...more,
  };
}

describe("where someone with nothing on V2 lands", () => {
  // Every account starts with an empty personal space on V1, so what the
  // person owns on V1, and what it holds, tells a V1 person from a new
  // one. Only an owner switches a space.
  const personal = space("personal", { kind: "personal", onV2: false });
  const acmeWeb = space("acme-web", { kind: "team", onV2: false });
  const sharedTeam = space("acme", {
    kind: "team",
    role: "member",
    onV2: false,
  });
  const viewed = space("ops", { kind: "team", role: "viewer", onV2: false });
  const wrote = preview({
    notes: { ...preview().notes, total: 12, candidates: 7, fold: 5 },
  });
  const cases: Array<{
    who: string;
    spaces: SpaceSummary[];
    switches?: Array<{
      space: SpaceSummary;
      preview: SwitchPreviewView | null;
    }>;
    lands: ReturnType<typeof decideLanding>;
  }> = [
    { who: "no space at all", spaces: [], lands: { kind: "first-run" } },
    {
      who: "only the personal space every account starts with",
      spaces: [personal],
      switches: [{ space: personal, preview: preview() }],
      lands: { kind: "first-run" },
    },
    {
      who: "a personal space whose preview didn't load",
      spaces: [personal],
      switches: [{ space: personal, preview: null }],
      lands: { kind: "first-run" },
    },
    {
      who: "V1 memories in their personal space",
      spaces: [personal],
      switches: [{ space: personal, preview: wrote }],
      lands: { kind: "switch", space: "personal" },
    },
    {
      who: "a persona synced from an agent's files",
      spaces: [personal],
      switches: [{ space: personal, preview: preview({ personas: 1 }) }],
      lands: { kind: "switch", space: "personal" },
    },
    {
      who: "agents connected on V1",
      spaces: [personal],
      switches: [
        {
          space: personal,
          preview: preview({ agents: DEMO_SWITCH_PREVIEW.agents }),
        },
      ],
      lands: { kind: "switch", space: "personal" },
    },
    {
      who: "nothing but V1's own Dream runs",
      spaces: [personal],
      switches: [{ space: personal, preview: preview({ dreamRuns: 3 }) }],
      lands: { kind: "first-run" },
    },
    {
      who: "a team space they own, before Personal",
      spaces: [personal, acmeWeb],
      lands: { kind: "switch", space: "acme-web" },
    },
    {
      who: "only a member or viewer of V1 spaces",
      spaces: [personal, sharedTeam, viewed],
      switches: [{ space: personal, preview: preview() }],
      lands: { kind: "first-run" },
    },
    {
      who: "a member of a V1 space, with their own V1 memories",
      spaces: [personal, sharedTeam],
      switches: [{ space: personal, preview: wrote }],
      lands: { kind: "switch", space: "personal" },
    },
  ];

  it.each(cases)("$who", ({ spaces, switches, lands }) => {
    expect(decideLanding({ spaces, imports: [], switches }, NOW)).toEqual(
      lands,
    );
  });

  it("reads a switch preview only when the personal space is what decides", () => {
    expect(switchesToRead([personal])).toEqual([personal]);
    expect(switchesToRead([personal, sharedTeam])).toEqual([personal]);
    // A team space of theirs decides without reading anything.
    expect(switchesToRead([personal, acmeWeb])).toEqual([]);
    // So does anything on V2.
    expect(switchesToRead([personal, space("memax-v2")])).toEqual([]);
    expect(switchesToRead([sharedTeam])).toEqual([]);
  });

  it("opens the switch on the space's Today, and FirstRun at /setup/import", () => {
    expect(landingHref({ kind: "switch", space: "acme-web" })).toBe(
      "/acme-web/today",
    );
    expect(landingHref({ kind: "first-run" })).toBe("/setup/import");
  });

  it("tells V1 work from an empty space by what switching would move", () => {
    expect(holdsV1Work(preview())).toBe(false);
    expect(holdsV1Work(preview({ dreamRuns: 4 }))).toBe(false);
    expect(holdsV1Work(wrote)).toBe(true);
    expect(holdsV1Work(preview({ configs: 1 }))).toBe(true);
    expect(holdsV1Work(preview({ gates: 1 }))).toBe(true);
    expect(holdsV1Work(DEMO_SWITCH_PREVIEW)).toBe(true);
  });

  it("reads the personal space's preview from the source", async () => {
    const demo = createDemoSource({ streamDelayMs: 0, commandDelayMs: 0 });
    const status = vi.fn(async () => ({
      ...(await demo.switch.status({ space: DEMO_V1_SPACE })),
      preview: wrote,
    }));
    const v1Only = {
      ...demo,
      spaces: async () => [personal, sharedTeam],
      switch: { status },
    };
    expect(await resolveLanding(v1Only)).toEqual({
      kind: "switch",
      space: "personal",
    });
    expect(status).toHaveBeenCalledTimes(1);
    expect(status).toHaveBeenCalledWith(
      expect.objectContaining({ space: personal }),
    );
    // A preview that fails to load counts as nothing to move.
    const failing = {
      ...v1Only,
      switch: {
        status: async () => {
          throw new Error("down");
        },
      },
    };
    expect(await resolveLanding(failing)).toEqual({ kind: "first-run" });
  });
});

describe("where a person lands after signing in", () => {
  it("brings someone back to an import still in progress", () => {
    const v2 = space("memax-v2");
    const view = demoImportView();
    expect(
      decideLanding(
        {
          spaces: [space("personal", { kind: "personal" }), v2],
          imports: [{ space: v2, view }],
        },
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
      decideLanding(
        { spaces: [personal, v2], imports: [{ space: v2, view: done }] },
        NOW,
      ),
    ).toEqual({
      kind: "today",
      space: "memax-v2",
    });
    const old = demoImportView();
    old.summary.createdAt = "2026-09-01T09:00:00Z";
    expect(
      decideLanding(
        { spaces: [personal, v2], imports: [{ space: v2, view: old }] },
        NOW,
      ),
    ).toEqual({
      kind: "today",
      space: "memax-v2",
    });
    expect(decideLanding({ spaces: [personal], imports: [] }, NOW)).toEqual({
      kind: "today",
      space: "personal",
    });
  });

  it("opens a space on V2 before any switch, once there is one", () => {
    // V1 work of theirs waits on its own Today, in the switcher.
    const v2 = space("memax-v2");
    const v1 = space("acme-web", { kind: "team", onV2: false });
    expect(
      decideLanding(
        { spaces: [v1, v2], imports: [{ space: v2, view: null }] },
        NOW,
      ),
    ).toEqual({ kind: "today", space: "memax-v2" });
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
        {
          spaces: [a, b],
          imports: [
            { space: a, view: older },
            { space: b, view: newer },
          ],
        },
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
