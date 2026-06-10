// @vitest-environment jsdom

import { describe, it, expect, vi, afterEach } from "vitest";
import {
  render,
  cleanup,
  fireEvent,
  screen,
  waitFor,
} from "@testing-library/react";

vi.mock("@/i18n", () => ({
  useLocale: () => ({
    t: {
      topics: {
        title: "Topics",
        topicLabel: "Topic",
        movePickerBrowseSubtopics: "Browse subtopics in {name}",
        movePickerTopicWithChildren:
          "Move here · {memories} in this topic · {subtopics} subtopics",
        movePickerTopicOnly: "Move here · {memories} in this topic",
        movePickerSubtopicsOnly: "Move here · {subtopics} subtopics",
        movePickerEmpty: "Move here",
        loadingHubTopics: "Loading topics...",
        noTopics: "No topics",
      },
      hubs: {
        hubsListLabel: "Hubs",
      },
    },
  }),
  useInterpolate: () => (template: string, vars: Record<string, unknown>) =>
    template.replace(/\{(\w+)\}/g, (_, key) => String(vars[key] ?? "")),
}));

vi.mock("@/hooks/use-is-mobile", () => ({
  useIsMobile: () => false,
}));

vi.mock("@/hooks/use-topics", () => ({
  useTopics: () => ({
    data: {
      topics: [
        {
          id: "alpha",
          name: "Alpha",
          icon: "folder",
          children: [],
          memory_count: 3,
          total_memory_count: 3,
        },
        {
          id: "beta",
          name: "Beta",
          icon: "folder",
          children: [],
          memory_count: 1,
          total_memory_count: 1,
        },
      ],
    },
  }),
}));

vi.mock("@/lib/auth", () => ({
  useAuth: () => ({
    user: { name: "Ada", display_name: "Ada" },
    hubs: [{ hub: { id: "hub-1", name: "Primary" } }],
    switchHub: vi.fn(),
  }),
  useActiveHub: () => ({
    activeHub: { hub: { id: "hub-1", name: "Primary" } },
  }),
}));

vi.mock("@/lib/memax-client", () => ({
  getMemaxClient: () => ({
    topics: {
      list: vi.fn(),
    },
  }),
}));

vi.mock("@/lib/query-client", () => ({
  queryClient: {
    fetchQuery: vi.fn(),
  },
}));

vi.mock("@/lib/hub-display", () => ({
  getHubDisplayName: (hub: { name: string }) => hub.name,
}));

vi.mock("@/components/features/topic/topic-icon", () => ({
  TopicIcon: ({ className }: { className?: string }) => (
    <span data-testid="topic-icon" className={className} />
  ),
}));

// eslint-disable-next-line import/first
import { DrillDownTree } from "./drill-down-tree";

afterEach(() => {
  cleanup();
});

describe("DrillDownTree mobile/touch focus behavior", () => {
  it("does not preselect a destination on open, but highlights once keyboard navigation starts", async () => {
    render(<DrillDownTree mode="select" onClose={() => {}} />);

    await screen.findByText("Alpha");
    expect(
      document.querySelector('[role="option"][aria-selected="true"]'),
    ).toBeNull();

    fireEvent.keyDown(screen.getByRole("listbox"), { key: "Home" });

    await waitFor(() => {
      const alphaRow = screen.getByText("Alpha").closest('[role="option"]');
      expect(alphaRow?.getAttribute("aria-selected")).toBe("true");
    });
  });

  it("ignores touch pointer hover so rows do not look selected on phone", async () => {
    render(<DrillDownTree mode="select" onClose={() => {}} />);

    await screen.findByText("Alpha");
    const alphaRow = screen.getByText("Alpha").closest('[role="option"]');
    expect(alphaRow).toBeTruthy();

    fireEvent.pointerEnter(alphaRow!, { pointerType: "touch" });

    expect(
      document.querySelector('[role="option"][aria-selected="true"]'),
    ).toBeNull();
  });
});
