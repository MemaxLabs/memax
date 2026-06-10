import { describe, expect, it } from "vitest";
import {
  barDomainReducer,
  createInitialBarDomainState,
} from "@/lib/bar-domain";

describe("bar domain reducer", () => {
  it("resets remember state", () => {
    const next = barDomainReducer(
      {
        ...createInitialBarDomainState(),
        rememberState: "sent",
        rememberedTitle: "Saved",
      },
      { type: "resetRemember" },
    );

    expect(next.rememberState).toBe("idle");
    expect(next.rememberedTitle).toBe("");
  });

  it("resets recall fields", () => {
    const next = barDomainReducer(
      {
        ...createInitialBarDomainState(),
        recallQuery: "query",
        askAnswer: "answer",
        aiLoading: true,
        aiStreaming: true,
      },
      { type: "resetRecall" },
    );

    expect(next.recallQuery).toBe("");
    expect(next.askAnswer).toBe("");
    expect(next.aiLoading).toBe(false);
    expect(next.aiStreaming).toBe(false);
  });

  it("updates staged files via updater", () => {
    const next = barDomainReducer(createInitialBarDomainState(), {
      type: "updateStagedFiles",
      updater: () => [
        {
          id: "file-1",
          name: "doc.txt",
          content: "hello",
          binary: false,
          contentType: "text/plain",
          size: 5,
          status: "pending",
        },
      ],
    });

    expect(next.stagedFiles).toHaveLength(1);
    expect(next.stagedFiles[0]?.id).toBe("file-1");
  });
});
