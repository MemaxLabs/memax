// @vitest-environment jsdom

import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, cleanup, fireEvent } from "@testing-library/react";
import type { ReactNode } from "react";
import { useEffect } from "react";

// ─── Module mocks — applied before the SUT is imported. ───

let mockPathname = "/";
let mockMemoryTopicId: string | null = null;
const mockUseMemory =
  vi.fn<(id: string) => { data: { topic_id: string } | undefined }>();
let mockTopics: Array<{
  id: string;
  children: Array<{ id: string; children?: unknown[] }>;
}> = [];

// i18n: stub the one branch we touch (t.topics.*). Missing keys throw,
// so leaving a path unmocked fails loudly.
vi.mock("@/i18n", () => ({
  useLocale: () => ({
    t: {
      topics: {
        treeTitle: "Topic Explorer",
        treeOpen: "Open tree",
        treeClose: "Close tree",
        // BrandMark (rendered inside PinnedTreeSidebar header) reads this
        // via t.topics.brandHome — without the stub the rendered aria-
        // label is `undefined`, which is silent but semantically wrong.
        brandHome: "memax — home",
      },
    },
  }),
}));

// Always-desktop by default. Individual tests re-mock for mobile paths.
vi.mock("@/hooks/use-is-mobile", () => ({
  useIsMobile: () => false,
}));

// Pathname + router — the panel reads usePathname to derive the active
// topic. Keep it trivial.
vi.mock("next/navigation", () => ({
  usePathname: () => mockPathname,
  useRouter: () => ({ push: vi.fn(), replace: vi.fn() }),
  useSearchParams: () => new URLSearchParams(),
}));

// Stub the tree content so we can assert "subtree is mounted" without
// pulling in the full data-fetching stack. The stub renders a button
// so inert-tab-order behaviour is verifiable against a real focusable
// descendant.
vi.mock("./topic-tree-content", () => ({
  TopicTreeContent: ({
    activeTopic,
    expandedIds,
    onToggleExpand,
  }: {
    activeTopic?: string;
    expandedIds: Set<string>;
    onToggleExpand: (id: string) => void;
  }) => (
    <div
      data-testid="tree-subtree"
      data-active-topic={activeTopic ?? ""}
      data-expanded-ids={[...expandedIds].join(",")}
    >
      <button data-testid="tree-subtree-focusable">Home</button>
      <button onClick={() => onToggleExpand("topic-root")}>toggle-root</button>
    </div>
  ),
}));

vi.mock("@/hooks/use-memories", () => ({
  useMemory: (id: string) =>
    mockUseMemory(id) ?? {
      data: mockMemoryTopicId ? { topic_id: mockMemoryTopicId } : undefined,
    },
}));

vi.mock("@/hooks/use-topics", () => ({
  useTopics: () => ({
    data: { topics: mockTopics },
  }),
}));

// DestinationPicker used by mobile modal — stub out to a plain div so
// tests don't need to bootstrap the picker's own data deps.
vi.mock("../destination-picker", () => ({
  DestinationPicker: () => <div data-testid="mobile-destination-picker" />,
}));

// framer-motion in jsdom: mock motion.* tags to render as their raw
// HTML counterparts with `animate` merged into inline style. Real
// framer-motion in jsdom doesn't reliably snap `animate` onto
// element.style — this mock gives deterministic DOM state so
// "animate target = style target" holds.
vi.mock("framer-motion", async () => {
  const React = await import("react");
  type MotionProps = {
    initial?: unknown;
    animate?: Record<string, unknown>;
    exit?: unknown;
    transition?: unknown;
    style?: React.CSSProperties;
    children?: React.ReactNode;
    [key: string]: unknown;
  };
  function createMotionTag(tag: string) {
    return React.forwardRef<HTMLElement, MotionProps>(function MotionTag(
      { initial, animate, exit, transition, style, children, ...rest },
      ref,
    ) {
      // Silence unused-var warnings without dropping intent.
      void initial;
      void exit;
      void transition;
      const mergedStyle: React.CSSProperties = {
        ...(style ?? {}),
        ...((animate as React.CSSProperties) ?? {}),
      };
      return React.createElement(
        tag,
        { ...rest, ref, style: mergedStyle },
        children as React.ReactNode,
      );
    });
  }
  const motion = new Proxy(
    {},
    {
      get: (_, key: string) => createMotionTag(key),
    },
  );
  const AnimatePresence = ({ children }: { children: React.ReactNode }) =>
    React.createElement(React.Fragment, null, children);
  const useReducedMotion = () => false;
  return { motion, AnimatePresence, useReducedMotion };
});

// ─── Provider + SUT imports — deferred to apply mocks. ───
// eslint-disable-next-line import/first
import { TopicTreePanelProvider, useTreePanel } from "./topic-tree-panel";
// eslint-disable-next-line import/first

interface TreeStateOverrides {
  isPinned?: boolean;
  isDragSessionOpen?: boolean;
  isOpen?: boolean;
}

/**
 * Force the TopicTreePanelProvider into the state we want before the
 * SUT renders. Uses a child component to call the context setters
 * (pin / beginDragSession / toggle) during the initial render.
 */
function SeedTreeState({
  children,
  state,
}: {
  children: ReactNode;
  state: TreeStateOverrides;
}) {
  return (
    <TopicTreePanelProvider>
      <Seeder state={state} />
      {children}
    </TopicTreePanelProvider>
  );
}

function Seeder({ state }: { state: TreeStateOverrides }) {
  const { pin, beginDragSession, toggle, isPinned, isDragSessionOpen, isOpen } =
    useTreePanel();
  // Effect-based seed — state changes land AFTER the first render, then
  // @testing-library's act() wrapper flushes the subsequent re-render
  // before returning from render(). Avoids React's "setState while
  // rendering" warning that a direct call path would trigger.
  useEffect(() => {
    if (state.isPinned && !isPinned) pin();
    if (state.isDragSessionOpen && !isDragSessionOpen) beginDragSession();
    if (state.isOpen && !isOpen) toggle();
  }, [
    state.isPinned,
    state.isDragSessionOpen,
    state.isOpen,
    isPinned,
    isDragSessionOpen,
    isOpen,
    pin,
    beginDragSession,
    toggle,
  ]);
  return null;
}

beforeEach(() => {
  // `localStorage` persistence carries the pinned pref between tests.
  // Clearing prevents order-dependence when isPinned is the thing under
  // test.
  window.localStorage.clear();
  mockPathname = "/";
  mockMemoryTopicId = null;
  mockTopics = [];
  mockUseMemory.mockReset();
});

afterEach(() => {
  cleanup();
});

describe("Mobile tree modal — non-darkening blurred backdrop", () => {
  beforeEach(() => {
    vi.resetModules();
    vi.doMock("@/hooks/use-is-mobile", () => ({
      useIsMobile: () => true,
    }));
  });

  afterEach(() => {
    vi.doUnmock("@/hooks/use-is-mobile");
  });

  it("renders the backdrop with .glass-backdrop (blurred, non-darkening)", async () => {
    // Re-import with the mobile mock active.
    const { TopicTreePanelOverlayHost, TopicTreePanelProvider, useTreePanel } =
      await import("./topic-tree-panel");

    function OpenOnMount() {
      const { toggle, isOpen } = useTreePanel();
      useEffect(() => {
        if (!isOpen) toggle();
      }, [isOpen, toggle]);
      return null;
    }

    const { container } = render(
      <TopicTreePanelProvider>
        <OpenOnMount />
        <TopicTreePanelOverlayHost />
      </TopicTreePanelProvider>,
    );

    // Backdrop is a motion.div with glass-backdrop class, mounted when
    // isOpen flips true. Not bg-black or bg-black/35 — must be
    // transparent + blurred.
    const backdrop = container.querySelector(".glass-backdrop");
    expect(backdrop).toBeTruthy();
    // Sanity: not the old dim overlay.
    expect(backdrop?.classList.contains("bg-black/35")).toBe(false);
  });
});
