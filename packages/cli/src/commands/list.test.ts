import { describe, expect, it } from "vitest";
import { buildNextPageCommand } from "./list.js";

describe("buildNextPageCommand", () => {
  it("carries relevant options into the next-page command", () => {
    const cmd = buildNextPageCommand(
      {
        sort: "relevant",
        limit: "50",
        hub: "team-hub",
      },
      "12|2026-04-08T10:00:00Z",
    );

    expect(cmd).toBe(
      "memax list --sort relevant --limit 50 --hub team-hub --cursor '12|2026-04-08T10:00:00Z'",
    );
  });

  it("preserves --topic-id across pages", () => {
    const cmd = buildNextPageCommand(
      {
        hub: "team-hub",
        topicId: "01H9X0ABCDEF",
      },
      "12|2026-04-08T10:00:00Z",
    );

    expect(cmd).toBe(
      "memax list --hub team-hub --topic-id 01H9X0ABCDEF --cursor '12|2026-04-08T10:00:00Z'",
    );
  });
});
