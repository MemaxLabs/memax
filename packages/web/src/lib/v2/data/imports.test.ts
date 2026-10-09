import type { V2 } from "memax-sdk";
import { describe, expect, it, vi } from "vitest";
import { demoImportView } from "./demo-imports-data";
import { createDemoImports } from "./imports-demo";
import {
  bulkMemories,
  fileOfRef,
  importInProgress,
  openConflicts,
  sourceGroups,
  waitingMemories,
} from "./imports";
import { createSdkImports, toImportView } from "./imports-sdk";
import type { SpaceSummary } from "./types";

const space: SpaceSummary = {
  id: "s1",
  slug: "memax-v2",
  name: "memax-v2",
  kind: "project",
  role: "owner",
  kept: null,
  agents: null,
  people: null,
  waiting: null,
};

function memory(
  id: string,
  ref: string,
  statement: string,
  more: Partial<V2.Memory> = {},
): V2.Memory {
  return {
    id,
    ref,
    space_id: "s1",
    tenant_id: "t1",
    statement,
    section: "conventions",
    kind: "fact",
    state: "proposed",
    lifecycle: "proposed",
    flags: [],
    trust: "repository",
    version: 1,
    sources: [],
    created_at: "2026-10-05T16:02:00Z",
    updated_at: "2026-10-05T16:02:00Z",
    ...more,
  } as V2.Memory;
}

/** An import as /v2 serves it: two files agreeing, a disagreement, and a web source. */
const wire: V2.ImportView = {
  import: {
    id: "imp1",
    space_id: "s1",
    tenant_id: "t1",
    actor_kind: "person",
    client: "memax-cli 2.0.0",
    files: [
      {
        path: "CLAUDE.md",
        kind: "claude_md",
        agent: "claude-code",
        location: "repository",
        statements: 3,
        skipped: 1,
        hidden_characters: 0,
      },
      {
        path: "GEMINI.md",
        kind: "gemini_md",
        agent: "gemini-cli",
        location: "repository",
        statements: 2,
        skipped: 0,
        hidden_characters: 0,
      },
    ],
    skipped: [{ ref: "CLAUDE.md:9", reason: "secret", detail: "GitHub token" }],
    counts: {
      items: 5,
      proposed: 4,
      folded: 1,
      existing: 0,
      refused: 0,
      conflicts: 1,
    },
    check: { state: "checked" },
    origin: "init",
    created_at: "2026-10-05T16:02:00Z",
  },
  items: [
    {
      position: 0,
      key: "f0l1",
      ref: "CLAUDE.md:1",
      location: "repository",
      outcome: "proposed",
      memory: { id: "m1", ref: "M-0001" },
      hidden_characters: 0,
    },
    {
      position: 1,
      key: "f1l1",
      ref: "GEMINI.md:1",
      location: "repository",
      outcome: "folded",
      memory: { id: "m1", ref: "M-0001" },
      folded_into: 0,
      hidden_characters: 0,
    },
    {
      position: 2,
      key: "f0l2",
      ref: "CLAUDE.md:2",
      location: "repository",
      outcome: "proposed",
      memory: { id: "m2", ref: "M-0002" },
      hidden_characters: 0,
    },
    {
      position: 3,
      key: "f1l2",
      ref: "GEMINI.md:2",
      location: "repository",
      outcome: "proposed",
      memory: { id: "m3", ref: "M-0003" },
      hidden_characters: 0,
    },
    {
      position: 4,
      key: "f0l3",
      ref: "CLAUDE.md:3",
      location: "repository",
      outcome: "proposed",
      memory: { id: "m4", ref: "M-0004" },
      hidden_characters: 0,
    },
  ],
  memories: [
    {
      memory: memory("m1", "M-0001", "Background jobs use River."),
      outcome: "proposed",
      items: 2,
      bulk: true,
    },
    {
      memory: memory("m2", "M-0002", "Run tests with `pnpm test`.", {
        state: "conflict",
      }),
      outcome: "proposed",
      items: 1,
      bulk: false,
      held: "conflict",
      conflict: 1,
    },
    {
      memory: memory("m3", "M-0003", "Run `npm test`."),
      outcome: "proposed",
      items: 1,
      bulk: false,
      held: "conflict",
      conflict: 1,
    },
    {
      memory: memory("m4", "M-0004", "MCP needs OAuth 2.1.", {
        trust: "external",
        sources: [
          {
            id: "x",
            kind: "url",
            ref: "the spec",
            uri: "https://modelcontextprotocol.io/spec",
            locator: {},
            external: true,
            trust: "external",
            created_at: "2026-10-05T16:02:00Z",
          },
        ],
      }),
      outcome: "proposed",
      items: 1,
      bulk: false,
      held: "quarantined",
    },
  ],
  conflicts: [
    {
      id: "c1",
      n: 1,
      subject: "Test command",
      members: [
        { id: "m2", ref: "M-0002" },
        { id: "m3", ref: "M-0003" },
      ],
      state: "open",
      created_receipt_id: "r1",
      last_receipt_id: "r1",
      created_at: "2026-10-05T16:02:01Z",
    },
  ],
  progress: { proposals: 4, working: 0, judged: 4, failed: 0, ready: true },
};

describe("the imports mapper", () => {
  it("joins each memory to the statements it stands for, with their files and agent", () => {
    const view = toImportView(wire);
    expect(view.summary).toMatchObject({
      id: "imp1",
      client: "memax-cli 2.0.0",
      check: "checked",
      counts: { items: 5, proposed: 4, folded: 1 },
      skipped: [
        { ref: "CLAUDE.md:9", reason: "secret", detail: "GitHub token" },
      ],
    });
    // The spec's gemini-cli is the Ledger registry's gemini.
    expect(view.summary.files.map((f) => f.agent)).toEqual([
      "claude-code",
      "gemini",
    ]);
    const [river, tests, , web] = view.memories;
    expect(river).toMatchObject({
      ref: "M-0001",
      refs: ["CLAUDE.md:1", "GEMINI.md:1"],
      files: ["CLAUDE.md", "GEMINI.md"],
      agent: "claude-code",
      items: 2,
      bulk: true,
      held: null,
    });
    expect(tests).toMatchObject({
      held: "conflict",
      conflict: 1,
      state: "conflict",
    });
    expect(web).toMatchObject({
      held: "quarantined",
      external: "modelcontextprotocol.io",
      trust: "external",
    });
    expect(view.conflicts[0]).toMatchObject({
      n: 1,
      subject: "Test command",
      suggestion: null,
      state: "open",
      members: [
        {
          ref: "M-0002",
          statement: "Run tests with `pnpm test`.",
          refs: ["CLAUDE.md:2"],
          agent: "claude-code",
        },
        {
          ref: "M-0003",
          statement: "Run `npm test`.",
          refs: ["GEMINI.md:2"],
          agent: "gemini",
        },
      ],
    });
  });

  it("knows what can be kept in bulk and what is still open", () => {
    const view = toImportView(wire);
    expect(bulkMemories(view).map((m) => m.ref)).toEqual(["M-0001"]);
    expect(waitingMemories(view)).toHaveLength(4);
    expect(openConflicts(view)).toHaveLength(1);
    expect(importInProgress(view)).toBe(true);
    expect(sourceGroups(view)).toEqual([
      { key: "all", files: [], count: 4 },
      { key: "claude_md", files: ["CLAUDE.md"], count: 3 },
      { key: "gemini_md", files: ["GEMINI.md"], count: 2 },
    ]);
    expect(fileOfRef("CLAUDE.md:12")).toBe("CLAUDE.md");
    expect(fileOfRef("~/.codex/memories/a.md")).toBe("~/.codex/memories/a.md");
  });

  it("sends bulk keeps and settlements as the spec has them", async () => {
    const client = {
      v2: {
        imports: {
          list: vi.fn(async () => ({ items: [wire.import], has_more: false })),
          get: vi.fn(async () => wire),
          settle: vi.fn(async () => ({
            outcome: "applied",
            policy: { effect: "apply" },
            conflict: {
              ...wire.conflicts[0]!,
              state: "settled",
              choice: "keep_one",
              chosen: { id: "m2", ref: "M-0002" },
            },
            memories: [wire.memories[1]!.memory, wire.memories[2]!.memory],
            receipts: [],
          })),
        },
        memories: {
          keepMany: vi.fn(async () => ({
            items: [
              { memory: "M-0001", ref: "M-0001", outcome: "applied" },
              {
                memory: "M-0002",
                ref: "M-0002",
                outcome: "refused",
                policy: { effect: "refuse", code: "in_conflict" },
              },
              {
                memory: "M-0009",
                outcome: "failed",
                error: { code: "edit_clash", message: "x" },
              },
            ],
          })),
          rejectMany: vi.fn(async () => ({ items: [] })),
        },
      },
    };
    const source = createSdkImports(client as never);
    expect((await source.list({ space }))[0]!.id).toBe("imp1");
    const settled = await source.settle({
      space,
      importId: "imp1",
      n: 1,
      choice: { choice: "keep_one", keep: "M-0002" },
      idempotencyKey: "k1",
    });
    expect(client.v2.imports.settle).toHaveBeenCalledWith(
      "memax-v2",
      "imp1",
      1,
      { choice: "keep_one", keep: "M-0002" },
      { idempotencyKey: "k1" },
    );
    expect(settled).toMatchObject({
      state: "settled",
      chosen: "M-0002",
      members: [
        { statement: "Run tests with `pnpm test`." },
        { statement: "Run `npm test`." },
      ],
    });
    const out = await source.keep({
      space,
      items: [{ ref: "M-0001", version: 1 }],
      idempotencyKey: "k2",
    });
    expect(client.v2.memories.keepMany).toHaveBeenCalledWith(
      "memax-v2",
      { items: [{ memory: "M-0001", version: 1 }] },
      { idempotencyKey: "k2" },
    );
    expect(out).toEqual({
      applied: ["M-0001"],
      refused: [{ ref: "M-0002", code: "in_conflict" }],
      failed: [{ ref: "M-0009", code: "edit_clash" }],
    });
  });
});

describe("the demo import", () => {
  it("adds up the way the server's would", () => {
    const view = demoImportView();
    const s = view.summary;
    expect(view.memories).toHaveLength(s.counts.proposed);
    expect(view.memories.reduce((n, m) => n + m.items, 0)).toBe(s.counts.items);
    expect(s.counts.items - s.counts.proposed).toBe(s.counts.folded);
    expect(s.files.reduce((n, f) => n + f.statements, 0)).toBe(s.counts.items);
    expect(view.conflicts).toHaveLength(s.counts.conflicts);
    expect(bulkMemories(view)).toHaveLength(25);
  });

  it("settles and keeps in bulk by the server's rules", async () => {
    const demo = createDemoImports({ nextRef: () => "M-0600" });
    const id = demoImportView().summary.id;
    const conflict = await demo.settle({
      space,
      importId: id,
      n: 2,
      choice: { choice: "keep_suggestion" },
      idempotencyKey: "k",
    });
    expect(conflict).toMatchObject({ state: "settled", chosen: "M-0600" });
    const view = (await demo.get({ space, id }))!;
    expect(view.memories.find((m) => m.ref === "M-0600")?.state).toBe("kept");
    await expect(
      demo.settle({
        space,
        importId: id,
        n: 2,
        choice: { choice: "leave_open" },
        idempotencyKey: "k2",
      }),
    ).rejects.toThrow();
    const out = await demo.keep({
      space,
      items: [
        { ref: bulkMemories(view)[0]!.ref, version: 1 },
        { ref: "M-0501", version: 1 },
      ],
      idempotencyKey: "k3",
    });
    expect(out.applied).toHaveLength(1);
    expect(out.refused).toEqual([{ ref: "M-0501", code: "in_conflict" }]);
  });
});
