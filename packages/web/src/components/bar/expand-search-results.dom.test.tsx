// @vitest-environment jsdom

import { describe, it, expect, vi, afterEach } from "vitest";
import { render, cleanup, screen } from "@testing-library/react";

// --- Mocks: keep bar context shape minimal, control surface visibility ---

let mockBarState: Record<string, unknown> = {};

vi.mock("@/contexts/bar-context", () => ({
  useBar: () => mockBarState,
}));

vi.mock("@/i18n", () => ({
  useLocale: () => ({
    t: {
      bar: {
        ai: {
          placeholderBefore: "AI answer will appear here after",
          placeholderAfter: "",
          thinking: "Thinking…",
          freeStub: "AI answers are a Pro feature.",
          error: "AI answer failed.",
        },
        section: {
          memaxSays: "memax says",
          memaxSaysTopic: "memax says · {topic}",
          related: "Related",
          relatedInTopic: "Related in {topic}",
          matches: "Matches",
        },
        remember: {
          action: "Dump to memax",
          actionFiles: "Dump {count} files",
          sending: "Sending…",
          error: "Couldn't send.",
          errorNetwork: "Network error",
        },
        hint: {
          recall: "recall",
          remember: "remember",
          navigate: "navigate",
          close: "close",
          run: "run",
        },
        noResults: "No results",
        noResultsHint: "Save something first.",
        recallError: "Recall failed.",
        recallErrorHint: "Network error.",
        command: {
          topic: { label: "topic", desc: "Jump to a topic" },
          hub: { label: "hub", desc: "Switch workspace" },
        },
      },
      recall: { copy: "Copy", copyForAI: "Copy for AI" },
      billing: { upgrade: "Upgrade" },
    },
  }),
  useInterpolate: () => (s: string) => s,
}));

vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: vi.fn() }),
  usePathname: () => "/home",
}));

vi.mock("@/hooks/use-memories", () => ({
  useMemories: () => ({ data: undefined }),
  flattenMemories: () => [],
}));

vi.mock("@/hooks/use-bar-search", () => ({
  useBarSearch: () => ({ data: undefined, resolvedQuery: "" }),
}));

vi.mock("@/hooks/use-topics", () => ({
  useTopics: () => ({ data: undefined }),
}));

vi.mock("@/lib/auth", () => ({
  useAuth: () => ({ activeHubId: "h1", hubs: [], user: null }),
  useActiveHub: () => ({
    hubFilter: undefined,
    activeHub: null,
    isTeamHub: false,
  }),
}));

vi.mock("@/hooks/use-copy", () => ({
  useCopy: () => ({ copied: false, copy: vi.fn() }),
}));

vi.mock("@/lib/recent-navigation", () => ({
  requestSurfaceTransitionForNavigation: vi.fn(),
}));

vi.mock("@/lib/strip-markdown", () => ({
  stripMarkdown: (s: string) => s,
}));

vi.mock("@/lib/bar-interaction", () => ({
  parseSlashCommand: () => null,
  parseMention: () => null,
}));

vi.mock("@/lib/format-context", () => ({
  formatContextBlock: () => "",
}));

vi.mock("@/lib/hub-display", () => ({
  getHubDisplayName: () => "Hub",
}));

// Defer import so mocks apply
// eslint-disable-next-line import/first
import { ExpandSearchResults } from "./expand-search-results";

function baseBarState(overrides: Record<string, unknown> = {}) {
  return {
    value: "",
    phase: "input",
    recallResult: undefined,
    recallQuery: "",
    recallFetching: false,
    recallIsError: false,
    retryRecall: vi.fn(),
    askAnswer: "",
    aiLoading: false,
    aiStreaming: false,
    canUseAI: true,
    selectedMemory: null,
    stagedFiles: [],
    sendKind: "recall",
    rememberState: "idle",
    rememberedTitle: "",
    undoRemember: vi.fn(),
    retryRemember: vi.fn(),
    activeNavigationIndex: null,
    setNavigationItems: vi.fn(),
    commitCommandScope: vi.fn(),
    peelForNavigation: vi.fn(),
    routeTopicDismissed: false,
    setValue: vi.fn(),
    isMobile: false,
    inputRef: { current: null },
    handleRememberSubmit: vi.fn(),
    handleSubmit: vi.fn(),
    scope: { kind: "recall" },
    interaction: {
      hasText: false,
      hasFiles: false,
      filesUploading: false,
      isCommandScope: false,
      isSlashDiscovery: false,
      isCommandMode: false,
      isMentionMode: false,
      isRememberMode: false,
      isRecallMode: true,
      showRecallSurfaces: true,
      hasLiftSignal: false,
      hasExpandContent: true,
      canRemember: false,
      sendState: "disabled",
      mobileComposeState: "docked",
    },
    surface: {
      visibilityState: "engaged",
      expandSurfaceKind: "recall-input",
      showExpandSurface: true,
      showRememberRow: true,
      showSynthesis: true,
      showRecallResults: true,
      showHintBar: true,
    },
    ...overrides,
  };
}

afterEach(() => {
  cleanup();
  mockBarState = {};
});

describe("ExpandSearchResults", () => {
  it("does not crash when showExpandSurface toggles from false to true", () => {
    // First render: surface hidden (hooks still run but component returns null)
    mockBarState = baseBarState({
      surface: {
        ...baseBarState().surface,
        showExpandSurface: false,
      },
    });
    const { rerender } = render(<ExpandSearchResults />);
    expect(screen.queryByText("Dump to memax")).toBeNull();

    // Second render: surface visible — previously crashed with hook-order error
    mockBarState = baseBarState({
      value: "test",
      interaction: {
        ...baseBarState().interaction,
        hasText: true,
        canRemember: true,
      },
    });
    rerender(<ExpandSearchResults />);
    expect(screen.getByText("Dump to memax")).toBeTruthy();
  });

  it("does not crash on repeated hidden→visible→hidden toggles", () => {
    // Simulates mobile keyboard open/close cycle
    const hidden = baseBarState({
      surface: { ...baseBarState().surface, showExpandSurface: false },
    });
    const visible = baseBarState({
      value: "hello",
      interaction: {
        ...baseBarState().interaction,
        hasText: true,
        canRemember: true,
      },
    });

    mockBarState = hidden;
    const { rerender } = render(<ExpandSearchResults />);

    for (let i = 0; i < 3; i++) {
      mockBarState = visible;
      rerender(<ExpandSearchResults />);
      expect(screen.getByText("Dump to memax")).toBeTruthy();

      mockBarState = hidden;
      rerender(<ExpandSearchResults />);
      expect(screen.queryByText("Dump to memax")).toBeNull();
    }
  });

  it("renders remember row inside the single scroll lane after synthesis", () => {
    mockBarState = baseBarState({
      value: "test",
      interaction: {
        ...baseBarState().interaction,
        hasText: true,
        canRemember: true,
        showRecallSurfaces: true,
      },
    });
    render(<ExpandSearchResults />);

    // Per the bar north-star (zones ② → ③ → ④), RememberRow lives inside the
    // scroll container between synthesis and sources — not a pinned row.
    const rememberRow = screen.getByText("Dump to memax");
    const synthesis = screen.getByText(/AI answer will appear here/);
    const scrollParent = rememberRow.closest("[class*='overflow-y-auto']");
    expect(scrollParent).toBeTruthy();
    // Synthesis must render before RememberRow in DOM order so the zone
    // cascade ② → ③ holds even when future refactors move rows around.
    expect(
      synthesis.compareDocumentPosition(rememberRow) &
        Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy();
  });

  it("renders synthesis placeholder inside the scroll container", () => {
    mockBarState = baseBarState({
      value: "test",
      interaction: {
        ...baseBarState().interaction,
        hasText: true,
        canRemember: true,
        showRecallSurfaces: true,
      },
    });
    render(<ExpandSearchResults />);

    const placeholder = screen.getByText(/AI answer will appear here/);
    const scrollParent = placeholder.closest("[class*='overflow-y-auto']");
    expect(scrollParent).toBeTruthy();
  });

  it("uses overscroll containment on the expanded result scroller", () => {
    mockBarState = baseBarState({
      value: "test",
      interaction: {
        ...baseBarState().interaction,
        hasText: true,
        canRemember: true,
        showRecallSurfaces: true,
      },
    });
    const { container } = render(<ExpandSearchResults />);

    expect(container.querySelector(".overscroll-contain")).toBeTruthy();
  });
});
