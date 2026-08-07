import { describe, it, expect } from "vitest";
import { en } from "@/i18n/locales/en";
import { zh } from "@/i18n/locales/zh";
import {
  buildAgentHandoffBundle,
  buildAgentHandoffPrompt,
  MAX_HANDOFF_QUOTES,
  type HandoffCardMeta,
} from "./agent-handoff";

const meta: HandoffCardMeta = {
  kind: "nextup",
  hubName: "Memax",
  generatedAt: "2026-08-05T03:00:00Z",
};

const item = {
  title: "Pick a backup strategy for the Neon database",
  why: "You asked yourself in July and never answered.",
  quotes: [
    {
      memory_id: "m1",
      when: "2026-07-20T09:00:00Z",
      excerpt: "Where do backups actually live?",
    },
  ],
};

describe("buildAgentHandoffPrompt", () => {
  it("emits the full brief: task, why, receipts, constraints, success bar", () => {
    const prompt = buildAgentHandoffPrompt(item, meta, en)!;
    expect(prompt).toContain(en.board.handoffPreamble);
    expect(prompt).toContain(
      "<task>\nPick a backup strategy for the Neon database",
    );
    expect(prompt).toContain(
      "<why_now>\nYou asked yourself in July and never answered.\n</why_now>",
    );
    expect(prompt).toContain('<context source="memax" card="nextup"');
    expect(prompt).toContain('hub="Memax"');
    expect(prompt).toContain('generated="2026-08-05"');
    expect(prompt).toContain(
      '- 2026-07-20 — "Where do backups actually live?" (memax_memory_id: m1)',
    );
    expect(prompt).toContain(
      `<constraints>\n${en.board.handoffConstraints}\n</constraints>`,
    );
    expect(prompt).toContain(
      `<success_criteria>\n${en.board.handoffSuccess}\n</success_criteria>`,
    );
    // A single task carries no numbering — index/of only appear when
    // the whole card is handed over at once.
    expect(prompt).not.toContain("index=");
  });

  it("uses zh scaffolding for zh users while keeping the tags English", () => {
    const prompt = buildAgentHandoffPrompt(item, meta, zh)!;
    expect(prompt).toContain(zh.board.handoffPreamble);
    expect(prompt).toContain(zh.board.handoffConstraints);
    expect(prompt).toContain(zh.board.handoffSuccess);
    expect(prompt).toContain("<task>");
    expect(prompt).toContain("<success_criteria>");
    // The user's own words are never translated — quotes stay verbatim.
    expect(prompt).toContain("Where do backups actually live?");
    expect(prompt).not.toContain(en.board.handoffPreamble);
  });

  it("omits why_now when the item has none", () => {
    const prompt = buildAgentHandoffPrompt(
      { ...item, why: "" },
      meta,
      en,
    ) as string;
    expect(prompt).not.toContain("<why_now>");
    expect(prompt).toContain("<context");
  });

  it("tells the agent when an item quotes nothing usable", () => {
    const prompt = buildAgentHandoffPrompt(
      { title: "Ship the migration", quotes: [] },
      meta,
      en,
    )!;
    expect(prompt).toContain(en.board.handoffNoContext);
  });

  it("tolerates malformed quotes and collapses multi-line excerpts", () => {
    const prompt = buildAgentHandoffPrompt(
      {
        title: "Ship the migration",
        quotes: [
          null,
          "not an object",
          { memory_id: "m1" },
          { excerpt: 42 },
          { excerpt: "  keeps\n  its meaning  ", when: "not-a-date" },
        ],
      },
      meta,
      en,
    )!;
    expect(prompt).toContain('- "keeps its meaning"');
    expect(prompt).not.toContain("42");
    expect(prompt).not.toContain("not-a-date");
    expect(prompt).not.toContain(en.board.handoffNoContext);
  });

  it("caps the receipts so a handoff stays a brief", () => {
    const quotes = Array.from({ length: MAX_HANDOFF_QUOTES + 4 }, (_, i) => ({
      memory_id: `m${i}`,
      excerpt: `excerpt ${i}`,
    }));
    const prompt = buildAgentHandoffPrompt(
      { title: "Ship the migration", quotes },
      meta,
      en,
    )!;
    expect(prompt).toContain("excerpt 0");
    expect(prompt).toContain(`excerpt ${MAX_HANDOFF_QUOTES - 1}`);
    expect(prompt).not.toContain(`excerpt ${MAX_HANDOFF_QUOTES}`);
  });

  it("keeps user data from breaking the context tag", () => {
    const prompt = buildAgentHandoffPrompt(
      item,
      { ...meta, hubName: 'a"<b>' },
      en,
    )!;
    expect(prompt).toContain('hub="a b"');
    expect(prompt).toContain('<context source="memax"');
  });

  it("returns null when there is no task to hand off", () => {
    expect(buildAgentHandoffPrompt({ title: "   " }, meta, en)).toBeNull();
    expect(buildAgentHandoffPrompt({}, meta, en)).toBeNull();
  });

  it("omits unknown card provenance instead of writing empty attributes", () => {
    const prompt = buildAgentHandoffPrompt(item, { kind: "nextup" }, en)!;
    expect(prompt).toContain('<context source="memax" card="nextup">');
  });
});

describe("buildAgentHandoffBundle", () => {
  const second = {
    title: "Finish the migration script",
    why: "You said tomorrow; that was three weeks ago.",
    quotes: [{ memory_id: "m2", excerpt: "Writing the script tomorrow." }],
  };

  it("numbers every task and states the brief once", () => {
    const prompt = buildAgentHandoffBundle([item, second], meta, en)!;
    expect(prompt).toContain('<task index="1" of="2">');
    expect(prompt).toContain('<task index="2" of="2">');
    expect(prompt).toContain("Pick a backup strategy for the Neon database");
    expect(prompt).toContain("Finish the migration script");
    // Each task keeps its own receipts...
    expect(prompt).toContain("Where do backups actually live?");
    expect(prompt).toContain("Writing the script tomorrow.");
    // ...but the envelope is stated once, not stapled per task.
    expect(prompt.split(en.board.handoffConstraints)).toHaveLength(2);
    expect(prompt.split(en.board.handoffPreamble)).toHaveLength(2);
  });

  it("drops titleless items and falls back to the single-task shape", () => {
    const prompt = buildAgentHandoffBundle(
      [item, { why: "orphan" }],
      meta,
      en,
    )!;
    expect(prompt).not.toContain("index=");
    expect(prompt).not.toContain("orphan");
  });

  it("returns null when no item survives", () => {
    expect(buildAgentHandoffBundle([], meta, en)).toBeNull();
    expect(buildAgentHandoffBundle([{ why: "orphan" }], meta, en)).toBeNull();
  });
});
