import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { MemaxError } from "memax-sdk";

// A fake memax client: one space on V2 holding M-0201, kept, which a
// memory citing it goes with; and a V1 hub.
const SPACE = "22222222-2222-4222-8222-222222222222";
const MEMORY = "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a70";

const space = {
  id: SPACE,
  tenant_id: SPACE,
  slug: "memax-v2",
  name: "memax-v2",
  kind: "project",
  role: "owner",
  v2_enabled_at: "2026-10-01T00:00:00Z",
};

const memory = (over: Record<string, unknown> = {}) => ({
  id: MEMORY,
  ref: "M-0201",
  space_id: SPACE,
  statement:
    "Jiahao is away from Oct 12 to Oct 26, so route reviews to Ziyang.",
  section: "conventions",
  kind: "fact",
  state: "kept",
  lifecycle: "kept",
  version: 3,
  ...over,
});

const preview = (over: Record<string, unknown> = {}) => ({
  ref: "M-0201",
  version: 3,
  carries: [
    {
      id: "c",
      ref: "M-0202",
      reason: "cites",
      with: "M-0201",
      lifecycle: "kept",
      kind: "fact",
    },
  ],
  files: [
    { id: "t1", kind: "agents_md", label: "AGENTS.md", delivery: "local" },
    { id: "t2", kind: "chatgpt", label: "ChatGPT project", delivery: "copy" },
  ],
  agents: 5,
  readers: 4,
  allowed: true,
  ...over,
});

const tombstone = (status: string) => ({
  id: MEMORY,
  op_id: MEMORY,
  ref: "M-0201",
  kind: "memory",
  object_id: MEMORY,
  space_id: SPACE,
  status,
  steps: [
    { kind: "asked", status: "done" },
    { kind: "removed", status: "done" },
    {
      kind: "target",
      status: status === "done" ? "done" : "waiting",
      reason: status === "done" ? undefined : "compiling",
      target: {
        id: "t1",
        kind: "agents_md",
        label: "AGENTS.md",
        delivery: "local",
      },
    },
    {
      kind: "target",
      status: "held",
      reason: "hand_edit",
      target: {
        id: "t3",
        kind: "cursor_mdc",
        label: ".cursor/rules/memax.mdc",
        delivery: "local",
      },
    },
    ...[1, 2, 3, 4].map(() => ({
      kind: "agent",
      status: "waiting",
      reason: "next_read",
    })),
    {
      kind: "agent",
      status: "waiting",
      reason: "paused",
      agent: {
        connection_id: "g",
        agent: "gemini_cli",
        display_name: "Gemini CLI",
      },
    },
  ],
  unreachable: [],
});

const state = {
  memory: memory(),
  preview: preview(),
  forgot: [] as { ref: string; input: unknown; opts: unknown }[],
  forgetError: undefined as MemaxError | undefined,
  polls: 0,
};

const fakeClient = {
  v2: {
    spaces: {
      list: vi.fn(async () => ({
        items: [
          {
            ...space,
            id: "11111111-1111-4111-8111-111111111111",
            slug: "v1",
            v2_enabled_at: undefined,
          },
          space,
        ],
      })),
    },
    memories: {
      get: vi.fn(async (ref: string) => {
        if (ref !== "M-0201" && ref !== MEMORY)
          throw new MemaxError("Nothing by that reference.", "not_found", 404);
        return { memory: state.memory, versions: [], receipts: { items: [] } };
      }),
      previewForget: vi.fn(async () => state.preview),
      forget: vi.fn(async (ref: string, input: unknown, opts: unknown) => {
        if (state.forgetError) throw state.forgetError;
        state.forgot.push({ ref, input, opts });
        return {
          outcome: "applied",
          policy: { effect: "apply" },
          memory: memory({
            statement: "",
            lifecycle: "forgotten",
            state: "forgotten",
          }),
          receipts: [],
          tombstone: tombstone("propagating"),
          memories: [],
        };
      }),
      tombstone: vi.fn(async () => {
        state.polls++;
        return tombstone("done");
      }),
    },
  },
};

vi.mock("../lib/client.js", () => ({ getClient: () => fakeClient }));
vi.mock("../lib/config.js", () => ({
  loadConfig: () => ({ api_url: "https://api.memax.app" }),
}));
const prompt = vi.hoisted(() => ({ answer: "" }));
vi.mock("../lib/prompt.js", () => ({ ask: async () => prompt.answer }));
const v1 = vi.hoisted(() => ({ deleted: [] as string[] }));
vi.mock("./delete.js", () => ({
  deleteCommand: async (id: string) => void v1.deleted.push(id),
}));

const { forgetCommand, removesLine } = await import("./forget.js");

let out: string[];
let errs: string[];
const tty = { in: process.stdin.isTTY };

beforeEach(() => {
  out = [];
  errs = [];
  state.memory = memory();
  state.preview = preview();
  state.forgot = [];
  state.forgetError = undefined;
  state.polls = 0;
  prompt.answer = "";
  v1.deleted = [];
  vi.spyOn(console, "log").mockImplementation(
    (...a: unknown[]) => void out.push(a.join(" ")),
  );
  vi.spyOn(console, "error").mockImplementation(
    (...a: unknown[]) => void errs.push(a.join(" ")),
  );
  vi.spyOn(process, "exit").mockImplementation(
    (code?: string | number | null) => {
      throw new Error(`exit ${code}`);
    },
  );
  process.stdin.isTTY = true;
});

afterEach(() => {
  vi.restoreAllMocks();
  process.stdin.isTTY = tty.in;
});

const printed = () => out.join("\n");

describe("memax forget", () => {
  it("says what it removes, and forgets once you type the ID", async () => {
    prompt.answer = "m-0201";
    await forgetCommand("M-0201", {});
    expect(printed()).toContain(
      "This removes it from Memax, 1 compiled file, 1 copy-out and 5 agents.",
    );
    expect(printed()).toContain("It takes M-0202 (cites it) with it.");
    expect(printed()).toContain("It cannot be undone. A tombstone stays.");
    expect(state.forgot).toHaveLength(1);
    const call = state.forgot[0];
    expect(call.ref).toBe(MEMORY);
    expect(call.input).toEqual({ carries: ["M-0202"] });
    expect(call.opts).toMatchObject({ ifMatch: 3, via: "cli" });
    expect(printed()).toContain("Forgotten M-0201");
    expect(printed()).toContain("1 file rewritten");
    expect(printed()).toContain(
      ".cursor/rules/memax.mdc has a hand edit, which Memax never writes over.",
    );
    expect(printed()).toContain(
      "5 agents will be told on their next read · Gemini CLI is paused",
    );
    expect(printed()).toContain(
      "The tombstone: https://memax.app/memax-v2/memories/M-0201",
    );
    expect(state.polls).toBeGreaterThan(0);
  });

  it("forgets nothing when the ID typed isn't the memory's", async () => {
    prompt.answer = "M-0202";
    await forgetCommand("M-0201", {});
    expect(state.forgot).toHaveLength(0);
    expect(printed()).toContain("That isn't M-0201. Nothing was forgotten.");
  });

  it("takes --confirm from a script, and only the right ID", async () => {
    process.stdin.isTTY = false;
    await expect(forgetCommand("M-0201", {})).rejects.toThrow("exit 1");
    expect(errs.join("\n")).toContain("Confirm with --confirm M-0201");
    await expect(
      forgetCommand("M-0201", { confirm: "M-0200" }),
    ).rejects.toThrow("exit 1");
    expect(errs.join("\n")).toContain("--confirm M-0200 isn't M-0201");
    expect(state.forgot).toHaveLength(0);
    await forgetCommand("M-0201", { confirm: "M-0201", note: " personal " });
    expect(state.forgot[0].input).toEqual({
      carries: ["M-0202"],
      note: "personal",
    });
  });

  it("never lets --yes stand in for the ID", async () => {
    process.stdin.isTTY = false;
    await expect(forgetCommand("M-0201", { yes: true })).rejects.toThrow(
      "exit 1",
    );
    expect(state.forgot).toHaveLength(0);
  });

  it("says why when policy refuses, before asking", async () => {
    state.preview = preview({
      allowed: false,
      policy: {
        effect: "refuse",
        code: "decision_needs_web",
        message:
          "Decisions in memax-v2 need a person on the web, and so does forgetting one. Forget M-0201 at memax.app.",
      },
    });
    await expect(forgetCommand("M-0201", {})).rejects.toThrow("exit 1");
    expect(errs.join("\n")).toContain("need a person on the web");
    expect(state.forgot).toHaveLength(0);
  });

  it("sends a person with a passkey to the web, where it asks for it", async () => {
    // The CLI can't answer a passkey check: the refusal says where to go.
    state.preview = preview({
      allowed: false,
      policy: {
        effect: "refuse",
        code: "needs_passkey",
        message:
          "You have a passkey, so forgetting M-0201 asks for it. Confirm with your passkey on memax.app.",
      },
    });
    await expect(forgetCommand("M-0201", {})).rejects.toThrow("exit 1");
    expect(errs.join("\n")).toContain(
      "You have a passkey, so forgetting M-0201 asks for it. Confirm with your passkey on memax.app.",
    );
    expect(state.forgot).toHaveLength(0);
  });

  it("asks again when what goes with it changed meanwhile", async () => {
    prompt.answer = "M-0201";
    state.forgetError = new MemaxError(
      "Forgetting M-0201 forgets M-0203 too.",
      "forget_carries",
      409,
      { carries: [{ ref: "M-0203" }] },
    );
    await expect(forgetCommand("M-0201", {})).rejects.toThrow("exit 1");
    expect(errs.join("\n")).toContain(
      "What goes with M-0201 changed while you confirmed (M-0203). Nothing was forgotten; run it again.",
    );
  });

  it("says a forgotten memory is forgotten", async () => {
    state.memory = memory({ lifecycle: "forgotten", state: "forgotten" });
    const previews = fakeClient.v2.memories.previewForget.mock.calls.length;
    await forgetCommand("M-0201", {});
    expect(printed()).toContain("M-0201 is already forgotten.");
    expect(fakeClient.v2.memories.previewForget.mock.calls.length).toBe(
      previews,
    );
    expect(state.forgot).toHaveLength(0);
  });

  it("keeps V1's delete for a V1 memory", async () => {
    await forgetCommand("44444444-4444-4444-8444-444444444444", {});
    expect(v1.deleted).toEqual(["44444444-4444-4444-8444-444444444444"]);
  });

  it("words what it removes", () => {
    expect(removesLine({ ...preview(), files: [], agents: 0 } as never)).toBe(
      "This removes it from Memax.",
    );
    expect(
      removesLine({
        ...preview(),
        files: [preview().files[0]],
        agents: 1,
      } as never),
    ).toBe("This removes it from Memax, 1 compiled file and 1 agent.");
  });
});
