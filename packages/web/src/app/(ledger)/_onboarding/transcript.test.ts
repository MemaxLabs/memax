import { describe, expect, it } from "vitest";
import { en } from "@/i18n/locales/en";
import { demoImportView } from "@/lib/v2/data/demo-imports-data";
import type { TargetView } from "@/lib/v2/data/targets";
import { firstRunLines, gitStatusLines, proposalsByFile } from "./transcript";

const copy = en.ledger.onboarding.firstRun;

describe("FirstRun's terminal", () => {
  it("waits for memax init until an import arrives", () => {
    expect(
      firstRunLines(copy, {
        viewer: "Ziyang Zeng",
        space: null,
        agents: [],
        chatgptPending: true,
        view: null,
        host: "memax.app",
      }),
    ).toEqual([
      { kind: "cmd", text: "npx memax-cli init" },
      { kind: "dim", text: "Waiting for memax init on your machine…" },
    ]);
  });

  it("prints what init did, in the CLI's layout", () => {
    const lines = firstRunLines(copy, {
      viewer: "ziyang",
      space: "memax-v2",
      agents: [
        { agent: "claude-code", autonomy: "propose" },
        { agent: "cursor", autonomy: "read" },
      ],
      chatgptPending: true,
      view: demoImportView(),
      host: "memax.app",
    });
    const text = lines.map((l) => `${l.kind}|${l.text ?? ""}`);
    expect(text).toContain("dim|Signed in as ziyang · space memax-v2");
    expect(text).toContain("ok|CC  Claude Code   ~/.claude      propose");
    expect(text).toContain("ok|CU  Cursor        .cursor/       read");
    expect(text).toContain(
      "proposed|GPT ChatGPT       connector · finish in your browser",
    );
    expect(text).toContain("dim|  7 duplicates folded · 2 secrets skipped");
    expect(text).toContain("warn|6 files, 3 conflicts, 1 Brief.");
    expect(text).toContain(
      "proposed|33 memories proposed. Nothing is kept until you say so.",
    );
    expect(text).toContain(
      "out|  Next: memax review   or   memax.app/memax-v2/review",
    );
    expect(text.find((l) => l.includes("CLAUDE.md"))).toMatch(
      /CLAUDE\.md\s+18 →\s+18/,
    );
  });

  it("says the judge is still reading until the import is ready", () => {
    const view = demoImportView();
    view.progress = { ...view.progress, ready: false, working: 12 };
    const lines = firstRunLines(copy, {
      viewer: null,
      space: "memax-v2",
      agents: [],
      chatgptPending: false,
      view,
      host: "memax.app",
    });
    expect(lines.at(-1)).toEqual({
      kind: "dim",
      text: "The judge is reading 33 proposals…",
    });
  });

  it("counts each file's statements that became proposals", () => {
    const counts = proposalsByFile(demoImportView());
    expect(counts.get("CLAUDE.md")).toBe(18);
    expect(counts.get(".cursor/rules/style.mdc")).toBe(2);
  });
});

function target(more: Partial<TargetView>): TargetView {
  return {
    id: more.kind ?? "t",
    slug: more.kind ?? "t",
    kind: "agents_md",
    label: "AGENTS.md",
    path: "AGENTS.md",
    reads: null,
    syncState: "in_sync",
    delivery: "local",
    settings: { include: "kept_only", stale: "mark", sizeBudget: 25600 },
    version: 1,
    openDrift: 0,
    holding: [],
    lastCompile: null,
    ...more,
  };
}

describe("CompileDone's git status", () => {
  it("marks files init read as modified and the rest as new, never deleted", () => {
    expect(
      gitStatusLines(
        [
          target({ kind: "agents_md", path: "AGENTS.md" }),
          target({ kind: "claude_md", path: "CLAUDE.md", label: "CLAUDE.md" }),
          target({
            kind: "cursor_mdc",
            path: ".cursor/rules",
            label: ".cursor/rules",
            lastCompile: {
              ref: "C-1",
              status: "delivered",
              at: "2026-10-05T16:10:00Z",
              bytes: 1,
              lines: 1,
              refs: [],
              cites: [],
              dropped: [],
              files: [".cursor/rules/memax-tests.mdc"],
            },
          }),
          target({
            kind: "chatgpt",
            path: null,
            label: "ChatGPT project",
            delivery: "copy",
          }),
          target({ kind: "gemini_md", path: "GEMINI.md", syncState: "off" }),
        ],
        ["CLAUDE.md", ".cursor/rules/style.mdc"],
      ),
    ).toEqual([
      "?? AGENTS.md",
      " M CLAUDE.md",
      "?? .cursor/rules/memax-tests.mdc",
    ]);
  });
});
