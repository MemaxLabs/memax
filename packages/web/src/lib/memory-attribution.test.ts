import { describe, expect, it } from "vitest";
import { resolveMemoryAttribution } from "@/lib/memory-attribution";

describe("resolveMemoryAttribution", () => {
  it("treats provenance-only agent rows as agent-attributed", () => {
    const attribution = resolveMemoryAttribution({
      owner_id: "user_1",
      source_agent: "",
      author_name: "",
      author_avatar_url: "",
      agent_display_name: "",
      agent_icon: "",
      provenance: {
        created_by_type: "agent",
        created_by_slug: "codex",
        created_by_display_name: "Codex",
        initiation_type: "agent_proactive",
      },
    } as never);

    expect(attribution.hasAgent).toBe(true);
    expect(attribution.agentDisplayName).toBe("Codex");
    expect(attribution.isAgentCapture).toBe(true);
  });

  it("keeps legacy unknown agent attribution neutral", () => {
    const attribution = resolveMemoryAttribution({
      owner_id: "user_1",
      source_agent: "claude-code",
      author_name: "",
      author_avatar_url: "",
      agent_display_name: "Claude Code",
      agent_icon: "",
      provenance: {
        created_by_type: "agent",
        created_by_slug: "claude-code",
        created_by_display_name: "Claude Code",
        initiation_type: "unknown",
        attribution_source: "legacy_source_agent",
      },
    } as never);

    expect(attribution.isLegacyUnknownAgentAttribution).toBe(true);
    expect(attribution.isAgentCapture).toBe(false);
  });

  it("treats assisted_by_agent plus human_requested_agent as collaboration, not actor identity", () => {
    const attribution = resolveMemoryAttribution({
      owner_id: "user_1",
      source_agent: "",
      author_name: "",
      author_avatar_url: "",
      agent_display_name: "",
      agent_icon: "",
      provenance: {
        created_by_type: "human",
        initiation_type: "human_requested_agent",
        assisted_by_agent: "claude-code",
      },
    } as never);

    expect(attribution.hasAgent).toBe(true);
    expect(attribution.createdByType).toBe("human");
    expect(attribution.isHumanRequestedAgent).toBe(true);
    expect(attribution.isAgentCapture).toBe(false);
    expect(attribution.agentDisplayName).toBe("Claude Code");
  });
});

describe("resolveMemoryAttribution — unknown author", () => {
  const base = {
    id: "m1",
    hub_id: "h1",
    owner_id: "u1",
    title: "t",
    content: "",
    content_type: "markdown",
    content_hash: "",
    summary: "",
    kind: "note",
    stability: "stable",
    retrieval_weight: 1,
    tags: [],
    boundary: "private",
    state: "active",
    pinned: false,
    source: "cli",
    version: 1,
    access_count: 0,
    created_at: "2026-09-18T00:00:00Z",
    updated_at: "2026-09-18T00:00:00Z",
    accessed_at: "2026-09-18T00:00:00Z",
  } as const;

  it("marks a machine write with no agent and no human evidence as unknown, not the user", () => {
    const attribution = resolveMemoryAttribution(
      {
        ...base,
        provenance: {
          created_by_type: "unknown",
          created_via: "cli",
          initiation_type: "unknown",
          attribution_source: "unknown",
        },
      } as never,
      "u1",
    );
    expect(attribution.hasAgent).toBe(false);
    expect(attribution.isOwnMemory).toBe(true);
    expect(attribution.isUnknownAuthor).toBe(true);
    expect(attribution.createdByType).toBe("unknown");
  });

  it("keeps a web human_direct write as the user", () => {
    const attribution = resolveMemoryAttribution(
      {
        ...base,
        source: "web",
        provenance: {
          created_by_type: "human",
          created_via: "web",
          initiation_type: "human_direct",
          attribution_source: "human",
        },
      } as never,
      "u1",
    );
    expect(attribution.isUnknownAuthor).toBe(false);
    expect(attribution.createdByType).toBe("human");
  });

  it("never flags an agent-attributed row as unknown author", () => {
    const attribution = resolveMemoryAttribution(
      {
        ...base,
        provenance: {
          created_by_type: "agent",
          created_by_slug: "hatch",
          created_via: "cli",
          initiation_type: "unknown",
          attribution_source: "auth",
        },
      } as never,
      "u1",
    );
    expect(attribution.hasAgent).toBe(true);
    expect(attribution.isUnknownAuthor).toBe(false);
  });
});
