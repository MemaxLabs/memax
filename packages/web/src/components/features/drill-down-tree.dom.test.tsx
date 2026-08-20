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
        searchTopicsPlaceholder: "Search topics",
        movePickerBrowseSubtopics: "Browse subtopics in {name}",
        movePickerTopicWithChildren:
          "Move here · {memories} in this topic · {subtopics} subtopics",
        movePickerTopicOnly: "Move here · {memories} in this topic",
        movePickerSubtopicsOnly: "Move here · {subtopics} subtopics",
        movePickerEmpty: "Move here",
        loadingHubTopics: "Loading topics...",
        noTopics: "No topics",
        noSearchResults: 'No topics match "{query}"',
        moveToHub: "Move to {name}",
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
          children: [
            {
              id: "gamma",
              name: "Gamma-deep",
              icon: "folder",
              children: [],
              memory_count: 2,
              total_memory_count: 2,
            },
          ],
          memory_count: 1,
          total_memory_count: 3,
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

describe("DrillDownTree search", () => {
  // The bug this covers: a drill-down shows one level at a time, so a
  // topic nested under another is unreachable without knowing the path.
  // Search has to flatten the whole forest, not filter the level.
  it("finds a nested topic from the root level and shows its path", async () => {
    render(<DrillDownTree mode="select" onClose={() => {}} />);
    await screen.findByText("Alpha");

    // Nested topic is NOT visible at the root level.
    expect(screen.queryByText("Gamma-deep")).toBeNull();

    fireEvent.change(screen.getByPlaceholderText("Search topics"), {
      target: { value: "gamma" },
    });

    const row = await screen.findByText("Gamma-deep");
    // Its ancestry replaces the counts, so the match is identifiable
    // out of context.
    expect(row.closest('[role="option"]')?.textContent).toContain("Beta");
    // Non-matching siblings drop out.
    expect(screen.queryByText("Alpha")).toBeNull();
  });

  it("zero matches shows a no-results line, not the empty-hub state", async () => {
    // "No topics yet" + the Move-to-hub CTA claim the HUB is empty; a
    // query with no matches is a different fact and must not surface
    // a hub-move action the user never asked for.
    render(<DrillDownTree mode="select" onClose={() => {}} />);
    await screen.findByText("Alpha");

    fireEvent.change(screen.getByPlaceholderText("Search topics"), {
      target: { value: "zzz-nothing" },
    });

    await screen.findByText('No topics match "zzz-nothing"');
    expect(screen.queryByText("No topics")).toBeNull();
    expect(screen.queryByText(/Move to /)).toBeNull();
  });

  it("clears the query on Escape before leaving the level", async () => {
    render(<DrillDownTree mode="select" onClose={() => {}} />);
    await screen.findByText("Alpha");
    const input = screen.getByPlaceholderText("Search topics");

    fireEvent.change(input, { target: { value: "gamma" } });
    await screen.findByText("Gamma-deep");

    fireEvent.keyDown(input, { key: "Escape" });

    // First Escape restores the level instead of closing the picker.
    await screen.findByText("Alpha");
    expect((input as HTMLInputElement).value).toBe("");
  });
});
