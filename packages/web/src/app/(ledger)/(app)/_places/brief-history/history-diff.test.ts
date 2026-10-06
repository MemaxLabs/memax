import { describe, expect, it } from "vitest";
import { en } from "@/i18n/locales/en";
import { zh } from "@/i18n/locales/zh";
import type { BriefStructure } from "@/lib/v2/data/brief";
import { DEMO_VERSIONS } from "@/lib/v2/data/demo-brief-data";
import type { Actor } from "@/lib/v2/data/records";
import { diffFromParent, diffVersions } from "./history-diff";
import { versionMeta, versionTitle, versionWhen } from "./history-copy";

const versions = DEMO_VERSIONS["memax-v2"]!;
const name = (actor: Actor | null) =>
  actor?.kind === "person" ? (actor.name ?? "") : "Dream";

const brief = (
  sections: BriefStructure["sections"],
  title = "Brief",
): BriefStructure => ({ title, summary: null, sections });

describe("what changed between two Brief versions", () => {
  it("reads facts added, taken out and moved, and prose reworded by its cites", () => {
    const from = brief([
      {
        key: "decisions",
        heading: "Decisions",
        items: [
          { ref: "M-1" },
          { ref: "M-2" },
          { text: "Old words.", cites: ["M-1"] },
        ],
      },
      { key: "conventions", heading: "Conventions", items: [{ ref: "M-3" }] },
    ]);
    const to = brief([
      {
        key: "decisions",
        heading: "Choices",
        items: [
          { ref: "M-1" },
          { ref: "M-3" },
          { text: "New words.", cites: ["M-1"] },
        ],
      },
      { key: "conventions", heading: "Conventions", items: [{ ref: "M-4" }] },
    ]);
    const diff = diffVersions(from, to);
    expect(diff.sections).toEqual([
      {
        key: "decisions",
        heading: "Choices",
        changes: [
          { kind: "renamed", from: "Decisions" },
          { kind: "moved", ref: "M-3", from: "Conventions", note: null },
          {
            kind: "reworded",
            ref: null,
            before: "Old words.",
            after: "New words.",
            note: null,
          },
          { kind: "removed", ref: "M-2", note: null },
        ],
      },
      {
        key: "conventions",
        heading: "Conventions",
        changes: [{ kind: "added", ref: "M-4", note: null }],
      },
    ]);
    expect(diff.unchanged).toEqual(["M-1"]);
    expect(diff.count).toBe(5);
  });

  it("lists what a Dream edition did to memories first, as BriefHistory.png does", () => {
    const diff = diffFromParent(versions[0]!, versions);
    expect(
      diff.sections.map((s) => [s.key, s.changes.map((c) => c.kind)]),
    ).toEqual([
      ["decisions", ["reworded", "flagged"]],
      ["conventions", ["added"]],
    ]);
    expect(diff.sections[1]!.changes[0]).toEqual({
      kind: "added",
      ref: "M-0436",
      note: "written by Claude Code, which may write here",
    });
    expect(diff.count).toBe(3);
    expect(diff.unchanged).toEqual(["M-0102", "M-0098", "M-0071", "M-0112"]);
  });

  it("titles each version by who changed it and what", () => {
    const b = en.ledger.brief;
    const titles = versions.map((v) =>
      versionTitle(b, v, diffFromParent(v, versions), name),
    );
    expect(titles).toEqual([
      "Dream rewrote it",
      "You edited one fact",
      "You added M-0219",
      "You added M-0102",
      "Jiahao added M-0098",
      "The first Brief",
    ]);
    expect(
      versionTitle(
        zh.ledger.brief,
        versions[4]!,
        diffFromParent(versions[4]!, versions),
        name,
      ),
    ).toBe("Jiahao添加了 M-0098");
  });

  it("dates and counts each version", () => {
    const b = en.ledger.brief;
    const now = new Date("2026-10-05T14:40:00-07:00");
    const tz = "America/Vancouver";
    const meta = versions.map((v) =>
      versionMeta(
        b,
        v,
        diffFromParent(v, versions),
        versionWhen(b, v.at, now, tz, "en"),
      ),
    );
    expect(meta).toEqual([
      "Today, 03:12 · 3 changes",
      "Oct 4, 16:02 · 1 change",
      "Oct 2, 10:58 · 1 added",
      "Sep 30, 18:40 · 1 added",
      "Sep 29, 11:20 · 1 added",
      "Sep 28, 15:20 · 7 facts",
    ]);
    expect(versionWhen(zh.ledger.brief, versions[1]!.at, now, tz, "zh")).toBe(
      "10月4日 16:02",
    );
  });
});
