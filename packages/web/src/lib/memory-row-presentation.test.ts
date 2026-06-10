import { describe, expect, it } from "vitest";
import {
  resolveMemoryRowLifecycleState,
  resolveMemoryRowHubType,
  resolveMemoryRowPresentation,
  type MemoryRowPresentationContext,
} from "@/lib/memory-row-presentation";

function makeMemory(
  overrides: Partial<{
    summary: string;
    state: string;
    content_type: string;
    author_name: string;
    source_agent: string;
    provenance: {
      created_by_type: "human" | "agent";
      created_by_slug?: string;
      created_by_display_name?: string;
      initiation_type:
        | "human_direct"
        | "human_requested_agent"
        | "agent_proactive"
        | "agent_automatic"
        | "import"
        | "unknown";
      attribution_source?: string;
    };
    agent_display_name: string;
    agent_icon: string;
    owner_id: string;
  }> = {},
) {
  return {
    summary: "",
    state: "ready",
    content_type: "text",
    owner_id: "user_1",
    ...overrides,
  };
}

function makeContext(
  overrides: Partial<MemoryRowPresentationContext>,
): MemoryRowPresentationContext {
  return {
    surface: "topic",
    isMobile: false,
    currentUserId: "user_1",
    isTeamHub: false,
    ...overrides,
  };
}

describe("memory row presentation", () => {
  it("keeps personal self topic rows content-led with no leading actor and inbox rows compact", () => {
    const topic = resolveMemoryRowPresentation(
      makeMemory(),
      makeContext({ surface: "topic" }),
    );
    const inbox = resolveMemoryRowPresentation(
      makeMemory(),
      makeContext({ surface: "inbox" }),
    );

    expect(topic.trailingActor).toBe("none");
    expect(topic.leadingIdentity).toBe("none");
    expect(topic.showSummary).toBe(false);
    expect(inbox.trailingActor).toBe("none");
    expect(inbox.leadingIdentity).toBe("content");
  });

  it("shows a trailing agent identity for personal agent topic and inbox rows", () => {
    const memory = makeMemory({
      source_agent: "claude-code",
      agent_display_name: "Claude Code",
    });

    const topic = resolveMemoryRowPresentation(
      memory,
      makeContext({ surface: "topic" }),
    );
    const inbox = resolveMemoryRowPresentation(
      memory,
      makeContext({ surface: "inbox" }),
    );

    expect(topic.trailingActor).toBe("agent");
    expect(topic.leadingIdentity).toBe("none");
    expect(topic.showTrailingContentMeta).toBe(false);
    expect(inbox.trailingActor).toBe("agent");
    expect(inbox.leadingIdentity).toBe("content");
  });

  it("supports provenance-only agent rows without legacy source_agent", () => {
    const memory = makeMemory({
      source_agent: "",
      provenance: {
        created_by_type: "agent",
        created_by_slug: "codex",
        created_by_display_name: "Codex",
        initiation_type: "agent_proactive",
      },
    });

    const topic = resolveMemoryRowPresentation(
      memory,
      makeContext({ surface: "topic" }),
    );

    expect(topic.trailingActor).toBe("agent");
    expect(topic.leadingIdentity).toBe("none");
  });

  it("shows a trailing author avatar for team human topic and inbox rows", () => {
    const memory = makeMemory({ author_name: "Derek" });

    const topic = resolveMemoryRowPresentation(
      memory,
      makeContext({ surface: "topic", isTeamHub: true }),
    );
    const inbox = resolveMemoryRowPresentation(
      memory,
      makeContext({ surface: "inbox", isTeamHub: true }),
    );

    expect(topic.trailingActor).toBe("author");
    expect(topic.leadingIdentity).toBe("none");
    expect(inbox.trailingActor).toBe("author");
    expect(inbox.leadingIdentity).toBe("content");
  });

  it("prefers the human actor over the agent for team rows captured via an agent", () => {
    const result = resolveMemoryRowPresentation(
      makeMemory({
        author_name: "Derek",
        source_agent: "claude-code",
        agent_display_name: "Claude Code",
      }),
      makeContext({ surface: "topic", isTeamHub: true }),
    );

    expect(result.trailingActor).toBe("author");
    expect(result.leadingIdentity).toBe("none");
  });

  it("keeps rich content badges trailing in topic rows even with a trailing actor", () => {
    const topic = resolveMemoryRowPresentation(
      makeMemory({
        content_type: "image",
        author_name: "Derek",
      }),
      makeContext({ surface: "topic", isTeamHub: true }),
    );

    expect(topic.leadingIdentity).toBe("none");
    expect(topic.trailingActor).toBe("author");
    expect(topic.showTrailingContentMeta).toBe(true);
  });

  it("keeps desktop recent behavior attribution-rich", () => {
    const personalSelf = resolveMemoryRowPresentation(
      makeMemory(),
      makeContext({ surface: "recent" }),
    );
    const personalAgent = resolveMemoryRowPresentation(
      makeMemory({
        source_agent: "claude-code",
        agent_display_name: "Claude Code",
      }),
      makeContext({ surface: "recent" }),
    );
    const teamHuman = resolveMemoryRowPresentation(
      makeMemory({ author_name: "Derek" }),
      makeContext({ surface: "recent", isTeamHub: true }),
    );

    expect(personalSelf.useStackedRecent).toBe(true);
    expect(personalSelf.leadingIdentity).toBe("none");
    expect(personalAgent.useStackedRecent).toBe(true);
    expect(personalAgent.leadingIdentity).toBe("agent");
    expect(teamHuman.useStackedRecent).toBe(true);
    expect(teamHuman.leadingIdentity).toBe("author");
  });

  it("collapses recent rows to compact single-line on mobile (no summary, no stacking, trailing actor)", () => {
    const personalSelfMobile = resolveMemoryRowPresentation(
      makeMemory({ summary: "something" }),
      makeContext({ surface: "recent", isMobile: true }),
    );
    const personalAgentMobile = resolveMemoryRowPresentation(
      makeMemory({
        summary: "something",
        source_agent: "claude-code",
        agent_display_name: "Claude Code",
      }),
      makeContext({ surface: "recent", isMobile: true }),
    );
    const teamHumanMobile = resolveMemoryRowPresentation(
      makeMemory({ summary: "something", author_name: "Derek" }),
      makeContext({ surface: "recent", isMobile: true, isTeamHub: true }),
    );
    const personalPdfMobile = resolveMemoryRowPresentation(
      makeMemory({ summary: "something", content_type: "pdf" }),
      makeContext({ surface: "recent", isMobile: true }),
    );

    // Personal own: no summary, no stacking, no copy, no actor — just title + time.
    expect(personalSelfMobile.showSummary).toBe(false);
    expect(personalSelfMobile.useStackedRecent).toBe(false);
    expect(personalSelfMobile.showCopy).toBe(false);
    expect(personalSelfMobile.leadingIdentity).toBe("none");
    expect(personalSelfMobile.trailingActor).toBe("none");

    // Personal with agent: trailing agent icon carries the attribution.
    expect(personalAgentMobile.useStackedRecent).toBe(false);
    expect(personalAgentMobile.trailingActor).toBe("agent");
    expect(personalAgentMobile.leadingIdentity).toBe("none");
    expect(personalAgentMobile.showSummary).toBe(false);

    // Team with human author: trailing author avatar.
    expect(teamHumanMobile.useStackedRecent).toBe(false);
    expect(teamHumanMobile.trailingActor).toBe("author");
    expect(teamHumanMobile.leadingIdentity).toBe("none");
    expect(teamHumanMobile.showSummary).toBe(false);

    // Rich content type (pdf): trailing type badge shows on mobile compact.
    expect(personalPdfMobile.showTrailingContentMeta).toBe(true);
  });

  it("keeps recall inline attribution behavior unchanged", () => {
    const teamRecall = resolveMemoryRowPresentation(
      makeMemory({ author_name: "Derek" }),
      makeContext({ surface: "recall", isTeamHub: true }),
    );
    const agentRecall = resolveMemoryRowPresentation(
      makeMemory({
        source_agent: "claude-code",
        agent_display_name: "Claude Code",
      }),
      makeContext({ surface: "recall" }),
    );

    expect(teamRecall.showInlineContext).toBe(true);
    expect(teamRecall.showHubLabel).toBe(true);
    expect(teamRecall.trailingActor).toBe("none");
    expect(agentRecall.showInlineContext).toBe(true);
    expect(agentRecall.trailingActor).toBe("none");
  });

  it("uses processing as the leading identity and suppresses trailing actors", () => {
    const processing = resolveMemoryRowPresentation(
      makeMemory({
        state: "processing",
        author_name: "Derek",
        source_agent: "claude-code",
      }),
      makeContext({ surface: "topic", isTeamHub: true }),
    );

    expect(processing.leadingIdentity).toBe("processing");
    expect(processing.trailingActor).toBe("none");
    expect(processing.lifecycleState).toBe("remembering");
  });

  it("treats non-processing recent rows without topic labels as chunked", () => {
    const chunked = resolveMemoryRowPresentation(
      makeMemory({ state: "ready" }),
      makeContext({ surface: "recent" }),
      { hasTopicLabel: false },
    );

    expect(chunked.lifecycleState).toBe("chunked");
    expect(chunked.isProcessing).toBe(false);
  });

  it("treats non-processing rows with topic labels as filed", () => {
    const filed = resolveMemoryRowPresentation(
      makeMemory({ state: "ready" }),
      makeContext({ surface: "recent" }),
      { hasTopicLabel: true },
    );

    expect(filed.lifecycleState).toBe("filed");
    expect(filed.isProcessing).toBe(false);
  });
});

describe("memory row lifecycle state", () => {
  it("maps legacy processing rows to remembering", () => {
    expect(resolveMemoryRowLifecycleState("processing", false)).toBe(
      "remembering",
    );
  });

  it("keeps topic-less ready rows chunked", () => {
    expect(resolveMemoryRowLifecycleState("ready", false)).toBe("chunked");
  });

  it("treats topic-tagged ready rows as filed", () => {
    expect(resolveMemoryRowLifecycleState("ready", true)).toBe("filed");
  });
});

describe("memory row card preset", () => {
  it("emits column layout, glass material, halo-on-hover, and hides actions", () => {
    const card = resolveMemoryRowPresentation(
      makeMemory(),
      makeContext({ surface: "card" }),
    );
    expect(card.layout).toBe("column");
    expect(card.glassMaterial).toBe(true);
    expect(card.haloOnHover).toBe(true);
    expect(card.showActions).toBe(false);
    expect(card.showTopic).toBe(true);
  });

  it("row surfaces keep the existing layout / no glass / no halo", () => {
    const recent = resolveMemoryRowPresentation(
      makeMemory(),
      makeContext({ surface: "recent" }),
    );
    const topic = resolveMemoryRowPresentation(
      makeMemory(),
      makeContext({ surface: "topic" }),
    );
    expect(recent.layout).toBe("row");
    expect(recent.glassMaterial).toBe(false);
    expect(recent.haloOnHover).toBe(false);
    expect(topic.layout).toBe("row");
    expect(topic.showTopic).toBe(false);
  });

  it("surfaces a cross-hub label on cards when memory.hub_id differs from currentHubId", () => {
    const cardSameHub = resolveMemoryRowPresentation(
      { ...makeMemory(), hub_id: "hub-personal" } as Parameters<
        typeof resolveMemoryRowPresentation
      >[0],
      makeContext({ surface: "card", currentHubId: "hub-personal" }),
    );
    const cardCrossHub = resolveMemoryRowPresentation(
      { ...makeMemory(), hub_id: "hub-team-x" } as Parameters<
        typeof resolveMemoryRowPresentation
      >[0],
      makeContext({ surface: "card", currentHubId: "hub-personal" }),
    );
    expect(cardSameHub.showHubLabel).toBe(false);
    expect(cardCrossHub.showHubLabel).toBe(true);
  });

  it("does NOT surface cross-hub label on row surfaces (preserves prior recall-only policy)", () => {
    const recent = resolveMemoryRowPresentation(
      { ...makeMemory(), hub_id: "hub-team-x" } as Parameters<
        typeof resolveMemoryRowPresentation
      >[0],
      makeContext({
        surface: "recent",
        currentHubId: "hub-personal",
        isTeamHub: false,
      }),
    );
    expect(recent.showHubLabel).toBe(false);
  });
});

describe("memory row hub type", () => {
  it("prefers the explicit hub type when available", () => {
    expect(
      resolveMemoryRowHubType(makeMemory({ author_name: "Derek" }), "personal"),
    ).toBe("personal");
    expect(resolveMemoryRowHubType(makeMemory(), "team")).toBe("team");
  });

  it("falls back to author presence when hub type is unavailable", () => {
    expect(resolveMemoryRowHubType(makeMemory({ author_name: "Derek" }))).toBe(
      "team",
    );
    expect(resolveMemoryRowHubType(makeMemory())).toBe("personal");
  });
});
