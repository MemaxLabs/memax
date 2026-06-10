// @vitest-environment jsdom

import { describe, it, expect } from "vitest";
import { render, screen } from "@testing-library/react";
import type { Translations } from "@/i18n/locales/en";
import { buildHubHeaderStatsLine } from "./hub-header-stats";

const t = {
  topics: {
    memories: "{n} memories",
    topics: "{n} topics",
  },
  hubs: {
    members: "{n} members",
  },
  hubHeader: {
    stats: {
      pendingReview: "{n} to review",
      cta: "Start with your first memory",
    },
  },
} as unknown as Translations;

const interpolate = (template: string, vars: Record<string, string>) =>
  template.replace(/\{(\w+)\}/g, (_, key) => vars[key] ?? "");

describe("buildHubHeaderStatsLine", () => {
  it("shows review count without surfacing the old unassigned stat", () => {
    render(
      <div>
        {buildHubHeaderStatsLine({
          stats: {
            memories: 12,
            topics: 4,
            inbox: 1,
            pendingReview: 3,
          },
          t,
          interpolate,
        })}
      </div>,
    );

    expect(screen.getByText(/3 to review/)).toBeTruthy();
    expect(screen.queryByText(/unassigned/i)).toBeNull();
  });
});
