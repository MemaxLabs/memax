// @vitest-environment jsdom

import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import {
  render,
  cleanup,
  act,
  fireEvent,
  waitFor,
  within,
} from "@testing-library/react";

// ── Mocks ─────────────────────────────────────────────────────────────────
//
// MemoryModal pulls in many hooks + heavy children. For the destructive
// dissolve sequence we care only about: menu mounts, Forget sub-panel
// morph, onConfirm fires, DISSOLVE_WAIT elapses, onClose is called.
// Everything else is stubbed to the minimum required.

vi.mock("@/i18n", () => ({
  useLocale: () => ({
    t: {
      batch: {},
      note: {
        classifiedAs: "classified as",
        classification: {
          recentActivity: "recent",
          pastContext: "past",
          howTo: "how-to",
          decisionContext: "decision",
          durableReference: "durable",
          reference: "reference",
        },
        stability: {
          volatile: "fades",
          evolving: "updates",
          stable: "kept",
        },
        actions: {
          menu: "More actions",
          copyMarkdown: "Copy markdown",
          rename: "Rename",
          download: "Download .md",
          showDetails: "Show details",
          hideDetails: "Hide details",
        },
        summary: "summary",
        attachments: "Attachments",
        originalFile: "Original file",
        download: "Download",
        downloading: "Downloading...",
        openPreview: "Open preview",
        closePreview: "Close preview",
        previousImage: "Previous",
        nextImage: "Next",
        addTag: "add tag",
        removeTag: "remove tag",
        source: {
          you: "you",
          claudeCode: "claude",
          cursor: "cursor",
          chatgpt: "chatgpt",
        },
        remembering: "remembering",
        forgetting: "Forgetting…",
        recalledN: "recalled {n}",
        capturedAt: "captured {time}",
        updatedAt: "updated {time}",
        origin: { web: "web", cli: "cli", mcp: "mcp", api: "api" },
        related: "Related",
      },
      forget: {
        button: "Forget",
        keep: "Keep",
        thisMemory: "Forget this memory?",
        forgetting: "Forgetting…",
      },
      attribution: {
        you: "you",
        via: "via",
        captured: "captured",
        capturedByAgent: "captured by {agent}",
        capturedByAgentPrefix: "via",
        capturedByAgentSuffix: "",
        fromAgent: "via {agent}",
        withAgent: "with {agent}",
        youViaPushed: "via {agent}",
        savedWithAgentPrefix: "with",
        savedWithAgentSuffix: "",
        pushed: "pushed",
      },
      topics: {},
      hubs: {},
      time: {
        justNow: "now",
        mAgo: "{n}m",
        hAgo: "{n}h",
        dAgo: "{n}d",
        wAgo: "{n}w",
        moAgo: "{n}mo",
      },
    },
  }),
  useInterpolate: () => (template: string, vars: Record<string, unknown>) =>
    template.replace(/\{(\w+)\}/g, (_, key) => String(vars[key] ?? "")),
  pluralize: (one: string, other: string, n: number) =>
    n === 1 ? one : other.replace("{n}", String(n)),
}));

vi.mock("@/hooks/use-is-mobile", () => ({
  useIsMobile: () => false,
}));

vi.mock("@/hooks/use-memories", async () => {
  const actual = await vi.importActual<typeof import("@/hooks/use-memories")>(
    "@/hooks/use-memories",
  );
  return {
    ...actual,
    useMemory: () => ({ data: null }),
    useUpdateMemory: () => ({
      mutate: vi.fn(),
      mutateAsync: vi.fn(async () => {}),
    }),
  };
});

// forgetWithConfirm returns a configurable value per test. We set it
// via the closed-over `forgetResult` and `forgetCall` handles.
const forgetCall = vi.fn<(ids: string[]) => Promise<boolean>>();
vi.mock("@/hooks/use-memory-forget", () => ({
  useMemoryForget: () => ({
    forgetWithConfirm: forgetCall,
  }),
}));

// Stub heavy children. ActionMenu is the real one — we're testing the
// ⋯ → destructive morph wiring end-to-end, so don't mock it.
vi.mock("@/components/features/memory-detail/memory-attachments", () => ({
  MemoryAttachments: () => null,
}));
vi.mock("@/components/features/stability-icon", () => ({
  StabilityIcon: () => null,
}));
vi.mock("@/lib/memory-attribution", async () => {
  const actual = await vi.importActual<
    typeof import("@/lib/memory-attribution")
  >("@/lib/memory-attribution");
  return {
    ...actual,
    resolveMemoryAttribution: () => ({
      authorName: null,
      authorAvatar: null,
      authorInitials: null,
      hasAgent: false,
      agentDisplayName: "",
      agentIcon: null,
      agentIconEmoji: null,
      agentIconColor: null,
      agentIdentity: null,
      isOwnMemory: true,
      isLegacyUnknownAgentAttribution: false,
      isHumanRequestedAgent: false,
    }),
  };
});
vi.mock("@/lib/memory-attribution-copy", () => ({
  linkedAgentAttributionCopy: () => ({ prefix: "", suffix: "" }),
  standaloneAgentAttributionText: () => "",
}));
vi.mock("@/lib/auth", () => ({
  useAuth: () => ({ user: { id: "u1", name: "Derek" } }),
}));

// framer-motion pass-through stub — render children, ignore motion props.
vi.mock("framer-motion", () => {
  type AnyProps = Record<string, unknown> & { children?: React.ReactNode };
  const passThrough =
    (tag: keyof React.JSX.IntrinsicElements) =>
    ({ children, ...rest }: AnyProps) => {
      const filtered = Object.fromEntries(
        Object.entries(rest).filter(
          ([k]) =>
            !k.startsWith("while") &&
            !k.startsWith("initial") &&
            !k.startsWith("animate") &&
            !k.startsWith("exit") &&
            !k.startsWith("transition") &&
            !k.startsWith("layout") &&
            !k.startsWith("drag") &&
            !k.startsWith("onDrag") &&
            !k.startsWith("variants"),
        ),
      );
      return React.createElement(tag, filtered, children);
    };
  const motion = new Proxy(
    {},
    {
      get: (_t, prop: string) =>
        passThrough(prop as keyof React.JSX.IntrinsicElements),
    },
  );
  return {
    motion,
    AnimatePresence: ({ children }: AnyProps) =>
      React.createElement(React.Fragment, null, children),
    useDragControls: () => ({ start: () => {} }),
    useReducedMotion: () => false,
  };
});

// eslint-disable-next-line import/first
import React from "react";
// eslint-disable-next-line import/first
import { MemoryModal } from "./memory-modal";
// eslint-disable-next-line import/first
import type { Memory } from "@/hooks/use-memories";

// ── Fixture ────────────────────────────────────────────────────────────────

const fixtureMemory: Memory = {
  id: "m-1",
  owner_id: "u1",
  hub_id: "h-1",
  title: "Test memory",
  content: "hello world",
  summary: "hello",
  tags: [],
  access_count: 0,
  created_at: "2026-04-20T00:00:00Z",
  updated_at: "2026-04-20T00:00:00Z",
  state: "ready",
  kind: "semantic",
  stability: "evolving",
  source: "web",
  source_agent: null,
  author_name: null,
  author_avatar_url: null,
  agent_display_name: null,
  agent_icon: null,
  attachments: [],
  content_type: "text",
  version: 1,
  provenance: undefined,
  project_context: undefined,
  source_path: null,
  lifecycle: undefined,
} as unknown as Memory;

// ── Tests ──────────────────────────────────────────────────────────────────

beforeEach(() => {
  forgetCall.mockReset();
});

afterEach(() => {
  cleanup();
  // Real timers are the default; no-op unless a future test opts into
  // fake timers for the dissolve wait. Codex re-review on ab05a1d1
  // flagged the 400ms real-time wait in the resolve(false) test as a
  // wall-clock cost if the suite grows — worth switching to
  // vi.useFakeTimers() + advanceTimersByTime(DISSOLVE_WAIT) at that
  // point. Currently acceptable at 2 tests.
  vi.useRealTimers();
});

describe("MemoryModal destructive sequence (slice 3+4 parity)", () => {
  it("⋯ → Forget → Confirm triggers onConfirm, dissolves, then calls onClose after DISSOLVE_WAIT", async () => {
    forgetCall.mockResolvedValue(true);
    const onClose = vi.fn();

    render(<MemoryModal memory={fixtureMemory} onClose={onClose} />);

    // ⋯ trigger is the ActionMenu. Open it.
    const moreTrigger = await waitFor(() =>
      within(document.body).getByRole("button", { name: "More actions" }),
    );
    await act(async () => {
      fireEvent.click(moreTrigger);
    });

    // Select Forget item — destructive items DO NOT close the popover;
    // they swap to the confirm sub-panel.
    const forgetItem = await waitFor(() =>
      within(document.body).getByRole("menuitem", { name: "Forget" }),
    );
    await act(async () => {
      fireEvent.click(forgetItem);
    });

    // Sub-panel mounted (alertdialog role with the confirm prompt).
    const confirmPanel = await waitFor(() =>
      within(document.body).getByRole("alertdialog", {
        name: "Forget this memory?",
      }),
    );

    // Click the destructive button inside the sub-panel.
    const confirmBtn = within(confirmPanel).getByRole("button", {
      name: "Forget",
    });
    await act(async () => {
      fireEvent.click(confirmBtn);
    });

    // forgetWithConfirm fired exactly once with our memory id.
    await waitFor(() => {
      expect(forgetCall).toHaveBeenCalledWith(["m-1"]);
    });

    // After the onConfirm promise resolves true, the handler calls
    // setDissolving(true) then setTimeout(onClose, DISSOLVE_WAIT).
    // DISSOLVE_WAIT is 250ms — just wait on the real timeline. Keep
    // the assertion loose so the test isn't brittle to small timing
    // drift on CI.
    await waitFor(
      () => {
        expect(onClose).toHaveBeenCalledTimes(1);
      },
      { timeout: 1500 },
    );
  });

  it("resolve(false) keeps the confirm panel mounted and does NOT call onClose", async () => {
    forgetCall.mockResolvedValue(false);
    const onClose = vi.fn();

    render(<MemoryModal memory={fixtureMemory} onClose={onClose} />);

    const moreTrigger = await waitFor(() =>
      within(document.body).getByRole("button", { name: "More actions" }),
    );
    await act(async () => {
      fireEvent.click(moreTrigger);
    });
    const forgetItem = await waitFor(() =>
      within(document.body).getByRole("menuitem", { name: "Forget" }),
    );
    await act(async () => {
      fireEvent.click(forgetItem);
    });
    const confirmPanel = await waitFor(() =>
      within(document.body).getByRole("alertdialog"),
    );
    await act(async () => {
      fireEvent.click(
        within(confirmPanel).getByRole("button", { name: "Forget" }),
      );
    });

    await waitFor(() => {
      expect(forgetCall).toHaveBeenCalledTimes(1);
    });

    // Sub-panel still mounted; modal NOT dissolving. Wait a bit
    // longer than DISSOLVE_WAIT (250ms) to prove onClose is truly
    // blocked, not just slow.
    expect(within(document.body).queryByRole("alertdialog")).not.toBeNull();
    await new Promise((resolve) => setTimeout(resolve, 400));
    expect(onClose).not.toHaveBeenCalled();
  });
});
