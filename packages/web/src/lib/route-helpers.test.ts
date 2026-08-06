import { describe, it, expect } from "vitest";
import {
  isMemoriesRoute,
  isPulseRoute,
  isTopicRoute,
  isMemoryDetailRoute,
  isBarMemorySurfaceRoute,
  isChatSurfaceRoute,
  isChatSessionRoute,
  getMemoryIdForPath,
  getTopicIdForPath,
  getActiveTopicIdForPath,
  getHubSlugForPath,
  buildMemoriesPath,
  buildTopicPath,
  buildMemoryDetailPath,
  getShellTabForPath,
  isBrainViewRoute,
} from "./route-helpers";

describe("isBrainViewRoute", () => {
  it("matches /brain and descendants", () => {
    expect(isBrainViewRoute("/brain")).toBe(true);
    expect(isBrainViewRoute("/brain/sessions/x")).toBe(true);
  });
  it("does NOT match /home — the neutral entry resolver", () => {
    // /home renders no bar and no brain chrome while it resolves the
    // landing surface; classifying it as brain-view reintroduces the
    // wrong-surface flash on cold entry.
    expect(isBrainViewRoute("/home")).toBe(false);
    expect(isBrainViewRoute("/home/anything")).toBe(false);
  });
});

describe("isMemoriesRoute", () => {
  it("matches v1 shapes", () => {
    expect(isMemoriesRoute("/memories")).toBe(true);
    expect(isMemoriesRoute("/memories/")).toBe(true);
    expect(isMemoriesRoute("/memories/abc")).toBe(true);
    expect(isMemoriesRoute("/memories/topics/xyz")).toBe(true);
  });
  it("matches v2 shapes", () => {
    expect(isMemoriesRoute("/h/personal/memories")).toBe(true);
    expect(isMemoriesRoute("/h/engineering/memories/")).toBe(true);
    expect(isMemoriesRoute("/h/team/memories/abc")).toBe(true);
    expect(isMemoriesRoute("/h/foo/memories/topics/x")).toBe(true);
  });
  it("rejects look-alikes", () => {
    expect(isMemoriesRoute("/memoriesfoo")).toBe(false); // no boundary
    expect(isMemoriesRoute("/inbox")).toBe(false);
    expect(isMemoriesRoute("/h/foo")).toBe(false);
    expect(isMemoriesRoute("/h/foo/topics/x")).toBe(false);
    expect(isMemoriesRoute("/")).toBe(false);
  });
});

describe("isPulseRoute", () => {
  it("matches /pulse and descendants", () => {
    expect(isPulseRoute("/pulse")).toBe(true);
    expect(isPulseRoute("/pulse/")).toBe(true);
    expect(isPulseRoute("/pulse/anything")).toBe(true);
  });
  it("rejects look-alikes", () => {
    expect(isPulseRoute("/pulsefoo")).toBe(false);
    expect(isPulseRoute("/memories")).toBe(false);
  });
  it("does NOT match the retired /inbox route", () => {
    // /inbox is a server-side redirect to /pulse — no chrome should
    // ever resolve against it.
    expect(isPulseRoute("/inbox")).toBe(false);
  });
});

describe("isTopicRoute", () => {
  it("matches v1 + v2", () => {
    expect(isTopicRoute("/memories/topics/abc")).toBe(true);
    expect(isTopicRoute("/h/personal/topics/abc")).toBe(true);
  });
  it("rejects nested or sibling paths", () => {
    expect(isTopicRoute("/memories/topics/abc/x")).toBe(false);
    expect(isTopicRoute("/memories")).toBe(false);
    expect(isTopicRoute("/h/foo/memories/topics/x")).toBe(false); // wrong shape — topic is sibling of memories
  });
});

describe("isMemoryDetailRoute", () => {
  it("matches /memories/<id> and /h/<slug>/memories/<id>", () => {
    expect(isMemoryDetailRoute("/memories/abc-123")).toBe(true);
    expect(isMemoryDetailRoute("/h/personal/memories/abc-123")).toBe(true);
  });
  it("excludes reserved sibling segments (topics, new)", () => {
    expect(isMemoryDetailRoute("/memories/topics")).toBe(false);
    expect(isMemoryDetailRoute("/memories/new")).toBe(false);
    expect(isMemoryDetailRoute("/h/personal/memories/new")).toBe(false);
  });
  it("rejects the overview itself", () => {
    expect(isMemoryDetailRoute("/memories")).toBe(false);
    expect(isMemoryDetailRoute("/h/personal/memories")).toBe(false);
  });
});

describe("isBarMemorySurfaceRoute", () => {
  it("returns true on memories + pulse routes (matches existing bar contract)", () => {
    expect(isBarMemorySurfaceRoute("/memories")).toBe(true);
    expect(isBarMemorySurfaceRoute("/memories/topics/x")).toBe(true);
    expect(isBarMemorySurfaceRoute("/pulse")).toBe(true);
    expect(isBarMemorySurfaceRoute("/h/personal/memories")).toBe(true);
    expect(isBarMemorySurfaceRoute("/h/team/memories/abc")).toBe(true);
  });
  it("returns true on v2 topic routes (sibling of /h/<slug>/memories)", () => {
    // /h/<slug>/topics/:id is NOT under /memories, so isMemoriesRoute
    // alone misses it. The predicate explicitly OR's isTopicRoute so
    // bar memory-surface chrome (drop targets, focus-to-engage docking,
    // topic-route-aware chip) keeps working on v2 topic pages.
    expect(isBarMemorySurfaceRoute("/h/personal/topics/welcome")).toBe(true);
    expect(isBarMemorySurfaceRoute("/h/engineering/topics/abc")).toBe(true);
  });
  it("returns false elsewhere", () => {
    expect(isBarMemorySurfaceRoute("/")).toBe(false);
    expect(isBarMemorySurfaceRoute("/brain")).toBe(false);
    expect(isBarMemorySurfaceRoute("/agents")).toBe(false);
    expect(isBarMemorySurfaceRoute("/h/personal")).toBe(false);
  });
});

describe("getMemoryIdForPath", () => {
  it("extracts the id from v1 + v2 detail routes", () => {
    expect(getMemoryIdForPath("/memories/abc")).toBe("abc");
    expect(getMemoryIdForPath("/h/personal/memories/abc")).toBe("abc");
  });
  it("returns undefined for reserved siblings", () => {
    expect(getMemoryIdForPath("/memories/topics")).toBeUndefined();
    expect(getMemoryIdForPath("/memories/new")).toBeUndefined();
  });
  it("returns undefined for the overview", () => {
    expect(getMemoryIdForPath("/memories")).toBeUndefined();
    expect(getMemoryIdForPath("/h/personal/memories")).toBeUndefined();
  });
  it("returns undefined for non-memory paths", () => {
    expect(getMemoryIdForPath("/")).toBeUndefined();
    expect(getMemoryIdForPath("/brain")).toBeUndefined();
  });
});

describe("getTopicIdForPath", () => {
  it("extracts from v1 + v2 topic routes", () => {
    expect(getTopicIdForPath("/memories/topics/welcome")).toBe("welcome");
    expect(getTopicIdForPath("/h/personal/topics/tutorial")).toBe("tutorial");
  });
  it("returns undefined for non-topic paths", () => {
    expect(getTopicIdForPath("/memories")).toBeUndefined();
    expect(getTopicIdForPath("/memories/abc")).toBeUndefined();
  });
});

describe("getActiveTopicIdForPath", () => {
  it("returns the topic id on a topic route", () => {
    expect(getActiveTopicIdForPath({ pathname: "/memories/topics/x" })).toBe(
      "x",
    );
    expect(getActiveTopicIdForPath({ pathname: "/h/personal/topics/y" })).toBe(
      "y",
    );
  });
  it("falls back to the memory's topic_id on a memory detail route", () => {
    expect(
      getActiveTopicIdForPath({
        pathname: "/memories/m1",
        memoryTopicId: "topic-a",
      }),
    ).toBe("topic-a");
    expect(
      getActiveTopicIdForPath({
        pathname: "/h/personal/memories/m1",
        memoryTopicId: "topic-a",
      }),
    ).toBe("topic-a");
  });
  it("returns undefined when memoryTopicId is null/undefined", () => {
    expect(
      getActiveTopicIdForPath({
        pathname: "/memories/m1",
        memoryTopicId: null,
      }),
    ).toBeUndefined();
    expect(
      getActiveTopicIdForPath({ pathname: "/memories/m1" }),
    ).toBeUndefined();
  });
  it("returns undefined on overview / unrelated routes", () => {
    expect(getActiveTopicIdForPath({ pathname: "/memories" })).toBeUndefined();
    expect(getActiveTopicIdForPath({ pathname: "/" })).toBeUndefined();
  });
});

describe("getHubSlugForPath", () => {
  it("returns the slug on v2 routes", () => {
    expect(getHubSlugForPath("/h/personal/memories")).toBe("personal");
    expect(getHubSlugForPath("/h/engineering/topics/x")).toBe("engineering");
    expect(getHubSlugForPath("/h/some-slug")).toBe("some-slug");
  });
  it("returns null on v1 routes", () => {
    expect(getHubSlugForPath("/memories")).toBeNull();
    expect(getHubSlugForPath("/pulse")).toBeNull();
    expect(getHubSlugForPath("/")).toBeNull();
  });
});

describe("buildMemoriesPath", () => {
  it("emits v1 path when slug is null", () => {
    expect(buildMemoriesPath(null)).toBe("/memories");
  });
  it("emits v2 path when slug is provided", () => {
    expect(buildMemoriesPath("personal")).toBe("/h/personal/memories");
    expect(buildMemoriesPath("engineering")).toBe("/h/engineering/memories");
  });
});

describe("buildTopicPath", () => {
  it("emits v1 path when slug is null", () => {
    expect(buildTopicPath(null, "topic-1")).toBe("/memories/topics/topic-1");
  });
  it("emits v2 path when slug is provided", () => {
    expect(buildTopicPath("personal", "topic-1")).toBe(
      "/h/personal/topics/topic-1",
    );
  });
});

describe("buildMemoryDetailPath", () => {
  it("emits v1 path when slug is null", () => {
    expect(buildMemoryDetailPath(null, "m1")).toBe("/memories/m1");
  });
  it("emits v2 path when slug is provided", () => {
    expect(buildMemoryDetailPath("personal", "m1")).toBe(
      "/h/personal/memories/m1",
    );
  });
});

describe("getShellTabForPath", () => {
  it("returns the right tab for v1 routes", () => {
    expect(getShellTabForPath("/brain")).toBe("brain");
    expect(getShellTabForPath("/brain/sessions/x")).toBe("brain");
    expect(getShellTabForPath("/agents")).toBe("agents");
    expect(getShellTabForPath("/agents/abc")).toBe("agents");
    expect(getShellTabForPath("/pulse")).toBe("pulse");
    expect(getShellTabForPath("/memories")).toBe("memories");
    expect(getShellTabForPath("/memories/abc")).toBe("memories");
    expect(getShellTabForPath("/memories/topics/x")).toBe("memories");
  });
  it("returns the right tab for v2 routes", () => {
    expect(getShellTabForPath("/h/personal/memories")).toBe("memories");
    expect(getShellTabForPath("/h/team/memories/abc")).toBe("memories");
    expect(getShellTabForPath("/h/personal/topics/x")).toBe("memories");
  });
  it("returns null for routes not under a tab", () => {
    expect(getShellTabForPath("/")).toBeNull();
    expect(getShellTabForPath("/login")).toBeNull();
    // /home is the neutral entry resolver — no rail tab may light up
    // while it decides between /brain and the memories overview.
    expect(getShellTabForPath("/home")).toBeNull();
    expect(getShellTabForPath("/h/personal")).toBeNull(); // hub root, not a memories route
    expect(getShellTabForPath("/settings")).toBeNull();
  });
});

describe("round-trip: parse → build → parse", () => {
  it("v1 memory detail", () => {
    const id = getMemoryIdForPath("/memories/abc");
    expect(id).toBe("abc");
    const path = buildMemoryDetailPath(null, id!);
    expect(path).toBe("/memories/abc");
    expect(getMemoryIdForPath(path)).toBe("abc");
  });
  it("v2 memory detail", () => {
    const id = getMemoryIdForPath("/h/personal/memories/abc");
    expect(id).toBe("abc");
    const slug = getHubSlugForPath("/h/personal/memories/abc");
    expect(slug).toBe("personal");
    const path = buildMemoryDetailPath(slug, id!);
    expect(path).toBe("/h/personal/memories/abc");
  });
  it("v2 topic detail", () => {
    const id = getTopicIdForPath("/h/team/topics/welcome");
    expect(id).toBe("welcome");
    const slug = getHubSlugForPath("/h/team/topics/welcome");
    expect(slug).toBe("team");
    const path = buildTopicPath(slug, id!);
    expect(path).toBe("/h/team/topics/welcome");
  });
});
