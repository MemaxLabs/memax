import { describe, expect, it } from "vitest";
import { summarizeRun } from "./dreams.js";

describe("summarizeRun", () => {
  it("includes only non-zero dream outcomes", () => {
    expect(
      summarizeRun({
        id: "r1",
        owner_id: "u1",
        hub_id: "h1",
        status: "completed",
        started_at: "",
        finished_at: "",
        memories_scanned: 12,
        duplicates_merged: 2,
        contradictions_found: 1,
        memories_archived: 0,
        memories_organized: 5,
        topics_restructured: 0,
        report: "",
      }),
    ).toEqual(["2 merged", "1 contradictions", "5 organized"]);
  });

  it("returns an empty summary when no changes were made", () => {
    expect(
      summarizeRun({
        id: "r2",
        owner_id: "u1",
        hub_id: "h1",
        status: "completed",
        started_at: "",
        finished_at: "",
        memories_scanned: 2,
        duplicates_merged: 0,
        contradictions_found: 0,
        memories_archived: 0,
        memories_organized: 0,
        topics_restructured: 0,
        report: "",
      }),
    ).toEqual([]);
  });
});
