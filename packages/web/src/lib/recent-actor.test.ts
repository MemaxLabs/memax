import { describe, expect, it } from "vitest";
import {
  getMemoryAgentSlug,
  memoryMatchesRecentActor,
  recentActorForMemory,
} from "@/lib/recent-actor";

describe("recent actor helpers", () => {
  it("prefers canonical provenance agent slug over legacy source_agent", () => {
    const memory = {
      source_agent: "claude-code",
      provenance: {
        created_by_slug: "codex",
      },
    };

    expect(getMemoryAgentSlug(memory)).toBe("codex");
    expect(recentActorForMemory(memory)).toBe("agent:codex");
  });

  it("supports provenance-only agent rows with empty legacy source_agent", () => {
    const memory = {
      source_agent: "",
      provenance: {
        created_by_slug: "codex",
      },
    };

    expect(getMemoryAgentSlug(memory)).toBe("codex");
    expect(recentActorForMemory(memory)).toBe("agent:codex");
  });

  it("treats team memories as author-led even when an agent captured them", () => {
    const memory = {
      author_name: "Alice",
      source_agent: "claude-code",
      provenance: {
        created_by_slug: "codex",
      },
    };

    expect(recentActorForMemory(memory, { hubType: "team" })).toBe(
      "author:Alice",
    );
    expect(
      memoryMatchesRecentActor(memory, "author:Alice", { hubType: "team" }),
    ).toBe(true);
    expect(
      memoryMatchesRecentActor(memory, "agent:codex", { hubType: "team" }),
    ).toBe(false);
  });

  it("falls back to self when there is no author or agent attribution", () => {
    const memory = {
      author_name: "",
      source_agent: "",
      provenance: undefined,
    };

    expect(recentActorForMemory(memory)).toBe("self");
    expect(memoryMatchesRecentActor(memory, "self")).toBe(true);
  });
});
