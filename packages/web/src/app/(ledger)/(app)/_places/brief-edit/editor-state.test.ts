import { describe, expect, it } from "vitest";
import { createDemoSource } from "@/lib/v2/data/demo-source";
import { DEMO_SPACES } from "@/lib/v2/data/demo-dataset";
import type { BriefView } from "@/lib/v2/data/brief";
import {
  changesOf,
  donePlan,
  draftFacts,
  draftOf,
  editorReducer,
  errorsOf,
  sectionKeyFor,
  type EditorAction,
  type EditorState,
} from "./editor-state";

const v2 = DEMO_SPACES.find((s) => s.slug === "memax-v2")!;

function demoBrief(): BriefView {
  const brief = createDemoSource().brief.peek?.(v2.slug);
  if (!brief) throw new Error("no demo brief");
  return brief;
}

const run = (state: EditorState, ...actions: EditorAction[]) =>
  actions.reduce(editorReducer, state);

const keys = (state: EditorState, section: string) =>
  state.sections.find((s) => s.key === section)!.items.map((i) => i.key);

const kept = (brief: BriefView) => (ref: string) =>
  brief.memories[ref]?.kept === true;

describe("the Brief editor's draft", () => {
  it("starts from the page without the proposals waiting in Review", () => {
    const state = draftOf(demoBrief());
    expect(state.base).toBe(6);
    expect(keys(state, "conventions")).toEqual([
      "M-0071",
      "M-0112",
      "M-0436",
      "M-0441",
      "M-0442",
    ]);
    expect(keys(state, "overview")).toEqual(["prose-0", "prose-1"]);
    expect(changesOf(state)).toEqual([]);
  });

  it("reorders within a section, and says so once", () => {
    const state = run(draftOf(demoBrief()), {
      type: "move",
      key: "M-0102",
      delta: 1,
    });
    expect(keys(state, "decisions")).toEqual([
      "M-0219",
      "M-0098",
      "M-0102",
      "M-0187",
    ]);
    expect(changesOf(state)).toEqual([
      { kind: "reordered", key: "r:decisions", section: "Decisions" },
    ]);
  });

  it("regroups past a section's edge: ⌥↑ into the one above, ⌥↓ into the one below", () => {
    const start = draftOf(demoBrief());
    // Up once to the top of Conventions, once more into Decisions.
    const up = run(
      start,
      { type: "move", key: "M-0112", delta: -1 },
      { type: "move", key: "M-0112", delta: -1 },
    );
    expect(keys(up, "decisions").at(-1)).toBe("M-0112");
    expect(keys(up, "conventions")).not.toContain("M-0112");
    expect(changesOf(up)).toEqual([
      {
        kind: "moved",
        key: "m:M-0112",
        ref: "M-0112",
        section: "Decisions",
        text: "Every write tool returns a receipt ID the caller can cite.",
        from: "Conventions",
      },
    ]);
    const down = run(start, { type: "move", key: "M-0187", delta: 1 });
    expect(keys(down, "conventions")[0]).toBe("M-0187");
    // The first fact of the first section has nowhere further up to go.
    expect(run(start, { type: "move", key: "prose-0", delta: -1 })).toBe(start);
  });

  it("drops a fact where it's dragged, closing its own gap first", () => {
    const state = run(draftOf(demoBrief()), {
      type: "moveTo",
      key: "M-0219",
      section: "decisions",
      index: 2,
    });
    expect(keys(state, "decisions")).toEqual([
      "M-0102",
      "M-0219",
      "M-0098",
      "M-0187",
    ]);
  });

  it("edits words in place: Esc puts them back, ⌘↵ keeps them", () => {
    const start = draftOf(demoBrief());
    const typed = run(
      start,
      { type: "edit", key: "M-0102" },
      { type: "text", key: "M-0102", text: "Remote MCP is stateless." },
    );
    expect(typed.editing?.key).toBe("M-0102");
    const cancelled = run(typed, { type: "cancel" });
    expect(cancelled.editing).toBeNull();
    expect(changesOf(cancelled)).toEqual([]);
    const keptEdit = run(typed, { type: "keep" });
    expect(changesOf(keptEdit)).toEqual([
      expect.objectContaining({
        kind: "edited",
        ref: "M-0102",
        after: "Remote MCP is stateless.",
      }),
    ]);
  });

  it("cites and uncites on prose only", () => {
    const start = draftOf(demoBrief());
    const cited = run(
      start,
      { type: "cite", key: "prose-1", ref: "M-0219" },
      { type: "cite", key: "M-0098", ref: "M-0219" },
    );
    expect(
      cited.sections[0]!.items.find((i) => i.key === "prose-1")!.cites,
    ).toEqual(["M-0012", "M-0219"]);
    expect(
      cited.sections[1]!.items.find((i) => i.key === "M-0098")!.cites,
    ).toEqual([]);
    expect(changesOf(cited)).toEqual([
      expect.objectContaining({ kind: "cited", ref: "M-0219" }),
    ]);
    const uncited = run(cited, {
      type: "uncite",
      key: "prose-1",
      ref: "M-0012",
    });
    expect(changesOf(uncited).map((c) => c.kind)).toEqual(["cited", "uncited"]);
  });

  it("checks Done: prose cites a kept memory, facts have words, sections a heading", () => {
    const brief = demoBrief();
    const bad = run(
      draftOf(brief),
      { type: "uncite", key: "prose-0", ref: "M-0001" },
      { type: "text", key: "M-0098", text: "  " },
      { type: "heading", section: "open", heading: "" },
      // The open line still cites the kept M-0174, so it passes.
    );
    const errors = errorsOf(bad, kept(brief));
    expect([...errors]).toEqual([
      ["prose-0", "proseNeedsCite"],
      ["M-0098", "emptyFact"],
      ["s:open", "emptyHeading"],
    ]);
    // An empty new fact is dropped, not an error.
    const blank = run(draftOf(brief), { type: "add", section: "conventions" });
    expect(errorsOf(blank, kept(brief)).size).toBe(0);
  });

  it("builds Done's payload: remember new facts, edit changed words, then the version", () => {
    const state = run(
      draftOf(demoBrief()),
      { type: "move", key: "M-0112", delta: -1 },
      { type: "move", key: "M-0112", delta: -1 },
      { type: "edit", key: "M-0102" },
      {
        type: "text",
        key: "M-0102",
        text: "Remote MCP is stateless streamable HTTP with OAuth 2.1, per spec 2026‑07‑28. Sessions are never pinned to a machine.",
      },
      { type: "keep" },
      { type: "add", section: "conventions", after: "M-0071" },
      {
        type: "text",
        key: "new-1",
        text: "Pin shared dependency versions with the pnpm catalog.",
      },
      { type: "remove", key: "M-0442" },
    );
    expect(changesOf(state).map((c) => c.kind)).toEqual([
      "edited",
      "moved",
      "added",
      "removed",
    ]);
    expect(draftFacts(state)).toBe(13);
    const plan = donePlan(state);
    expect(plan.remember).toEqual([
      {
        key: "new-1",
        section: "conventions",
        statement: "Pin shared dependency versions with the pnpm catalog.",
      },
    ]);
    expect(plan.edits).toEqual([
      {
        ref: "M-0102",
        version: 1,
        statement:
          "Remote MCP is stateless streamable HTTP with OAuth 2.1, per spec 2026‑07‑28. Sessions are never pinned to a machine.",
      },
    ]);
    const structure = plan.structure(new Map([["new-1", "M-0439"]]));
    expect(structure.title).toBe("Memax V2 engineering brief");
    expect(structure.sections.map((s) => s.key)).toEqual([
      "overview",
      "decisions",
      "conventions",
      "open",
    ]);
    expect(structure.sections[1]!.items).toEqual([
      { ref: "M-0219" },
      { ref: "M-0102" },
      { ref: "M-0098" },
      { ref: "M-0187" },
      { ref: "M-0112" },
    ]);
    // The unplaced kept facts are placed now; the new fact after M-0071.
    expect(structure.sections[2]!.items).toEqual([
      { ref: "M-0071" },
      { ref: "M-0439" },
      { ref: "M-0436" },
      { ref: "M-0441" },
    ]);
    expect(structure.sections[3]!.items).toEqual([
      {
        text: "Fly.io or Railway for the v2 API is undecided. Ask before changing deploy config.",
        cites: ["M-0431", "M-0174"],
      },
    ]);
  });

  it("adds a section with a key the compiler accepts, unique in the Brief", () => {
    expect(sectionKeyFor("Web app", [])).toBe("web-app");
    expect(sectionKeyFor("Decisions", ["decisions"])).toBe("decisions-2");
    expect(sectionKeyFor("2026 plans", [])).toBe("plans");
    expect(sectionKeyFor("网页", [])).toBe("section");
    const state = run(draftOf(demoBrief()), {
      type: "addSection",
      heading: "New section",
    });
    expect(state.sections.at(-1)).toMatchObject({
      key: "new-section",
      originalHeading: null,
      items: [],
    });
    expect(changesOf(state)).toEqual([
      { kind: "section", key: "s:new-section", section: "New section" },
    ]);
    // An empty section doesn't compile, so Done leaves it out.
    expect(
      donePlan(state)
        .structure(new Map())
        .sections.map((s) => s.key),
    ).not.toContain("new-section");
  });
});
