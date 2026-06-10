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
