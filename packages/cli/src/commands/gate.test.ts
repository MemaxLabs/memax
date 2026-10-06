import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { MemaxError } from "memax-sdk";

// A fake memax client: two spaces on V2 (one a team space whose decisions
// need the web) and a V1 hub, with one gate waiting in each V2 space.
const PROJECT = "22222222-2222-4222-8222-222222222222";
const TEAM = "33333333-3333-4333-8333-333333333333";

const space = (id: string, slug: string, kind = "project") => ({
  id,
  tenant_id: id,
  slug,
  name: slug,
  kind,
  role: "owner",
  v2_enabled_at: "2026-10-01T00:00:00Z",
});

const gate = (spaceId: string, over: Record<string, unknown> = {}) => ({
  id: spaceId === PROJECT ? "g-project" : "g-team",
  ref: "G-0012",
  space_id: spaceId,
  question: "Which deploy target should the v2 API use?",
  context: "M-0174 and M-0431 disagree.",
  options: [
    { label: "Fly.io, iad and ams", detail: "Matches the current API." },
    { label: "Railway" },
    { label: "Decide later" },
  ],
  status: "waiting",
  expires_at: "2026-10-13T09:30:00Z",
  agent: "codex",
  needs_web: spaceId === TEAM,
  version: 1,
  ...over,
});

const state = {
  answered: [] as { ref: string; input: unknown; opts: unknown }[],
  refuse: false,
  gates: {} as Record<string, ReturnType<typeof gate> | undefined>,
};

const fakeClient = {
  v2: {
    spaces: {
      list: vi.fn(async () => ({
        items: [
          {
            ...space("11111111-1111-4111-8111-111111111111", "v1-team"),
            v2_enabled_at: undefined,
          },
          space(PROJECT, "memax-v2"),
          space(TEAM, "acme", "team"),
        ],
      })),
    },
    gates: {
      list: vi.fn(async (spaceId: string) => ({
        items: state.gates[spaceId] ? [state.gates[spaceId]] : [],
        has_more: false,
      })),
      get: vi.fn(async (_ref: string, opts?: { space?: string }) => {
        const g = opts?.space ? state.gates[opts.space] : undefined;
        if (!g)
          throw new MemaxError("Nothing by that reference.", "not_found", 404);
        return g;
      }),
      answer: vi.fn(
        async (ref: string, input: { option: number }, opts: unknown) => {
          if (state.refuse) {
            throw new MemaxError(
              "Only owners answer decisions in memax-v2. Ask an owner.",
              "refused",
              403,
              {
                policy: {
                  effect: "refuse",
                  code: "owners_keep",
                  message:
                    "Only owners answer decisions in memax-v2. Ask an owner.",
                },
              },
            );
          }
          state.answered.push({ ref, input, opts });
          const g = gate(PROJECT, {
            status: "answered",
            version: 2,
            answer: {
              option: input.option,
              label: gate(PROJECT).options[input.option - 1].label,
              memory: { id: "m", ref: "M-0432" },
            },
          });
          return {
            outcome: "applied",
            policy: { effect: "apply" },
            gate: g,
            memory: { ref: "M-0432" },
            receipts: [],
          };
        },
      ),
    },
  },
};

vi.mock("../lib/client.js", () => ({ getClient: () => fakeClient }));
vi.mock("../lib/config.js", () => ({
  loadConfig: () => ({ api_url: "https://api.memax.app" }),
}));
const prompt = vi.hoisted(() => ({ answer: "" }));
vi.mock("../lib/prompt.js", () => ({ ask: async () => prompt.answer }));

const { gateCommand, parseOption, gateStatusLine } = await import("./gate.js");

let out: string[];
let errs: string[];
const tty = { out: process.stdout.isTTY, in: process.stdin.isTTY };

beforeEach(() => {
  out = [];
  errs = [];
  state.answered = [];
  state.refuse = false;
  state.gates = { [PROJECT]: gate(PROJECT) };
  prompt.answer = "";
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
  process.stdout.isTTY = true;
  process.stdin.isTTY = true;
});

afterEach(() => {
  vi.restoreAllMocks();
  process.stdout.isTTY = tty.out;
  process.stdin.isTTY = tty.in;
});

const printed = () => out.join("\n");

describe("memax gate", () => {
  it("lists what is waiting on you in spaces on V2", async () => {
    state.gates[TEAM] = gate(TEAM, {
      ref: "G-0003",
      question: "Ship on Fridays?",
    });
    await gateCommand(undefined, {});
    expect(printed()).toContain("Waiting on you");
    expect(printed()).toContain("G-0012");
    expect(printed()).toContain("memax-v2 · codex · until 2026-10-13");
    expect(printed()).toContain("Ship on Fridays?");
    expect(fakeClient.v2.gates.list).toHaveBeenCalledWith(PROJECT, {
      status: "waiting",
    });
    expect(fakeClient.v2.gates.list).toHaveBeenCalledTimes(2); // not the V1 hub
  });

  it("lists one line per gate in a pipe", async () => {
    process.stdout.isTTY = false;
    await gateCommand(undefined, { space: "memax-v2" });
    expect(out).toEqual([
      "G-0012\tmemax-v2\tcodex\tWhich deploy target should the v2 API use?",
    ]);
  });

  it("answers by number from a script, as a CLI keep with If-Match", async () => {
    await gateCommand("G-0012", { option: "2" });
    expect(printed()).toContain("Which deploy target should the v2 API use?");
    expect(printed()).toContain("Matches the current API.");
    expect(printed()).toContain("✓ Answered G-0012: Railway.");
    expect(printed()).toContain("Kept as M-0432, a decision by you.");
    const call = state.answered[0];
    expect(call.ref).toBe("g-project");
    expect(call.input).toEqual({ option: 2 });
    expect(call.opts).toMatchObject({ ifMatch: 1, via: "cli" });
    expect(
      String((call.opts as { idempotencyKey: string }).idempotencyKey),
    ).toMatch(/^[0-9a-f-]{36}$/);
  });

  it("answers by label, and interactively", async () => {
    await gateCommand("G-0012", { option: "decide LATER" });
    expect((state.answered[0].input as { option: number }).option).toBe(3);
    prompt.answer = "1";
    await gateCommand("G-0012", {});
    expect((state.answered[1].input as { option: number }).option).toBe(1);
    prompt.answer = "";
    await gateCommand("G-0012", {});
    expect(printed()).toContain("Left waiting.");
    expect(state.answered).toHaveLength(2);
  });

  it("points to the web where the space's decisions need a person there (D15)", async () => {
    state.gates = { [TEAM]: gate(TEAM) };
    await gateCommand("G-0012", {});
    expect(printed()).toContain(
      "Decisions in acme need a person on the web. Answer it at https://memax.app/acme/review?ref=G-0012",
    );
    await expect(gateCommand("G-0012", { option: "1" })).rejects.toThrow(
      "exit 1",
    );
    expect(errs.join("\n")).toContain("need a person on the web");
    expect(state.answered).toHaveLength(0);
  });

  it("asks which space when a display ID is in more than one", async () => {
    state.gates[TEAM] = gate(TEAM);
    await expect(gateCommand("G-0012", { option: "1" })).rejects.toThrow(
      "exit 1",
    );
    expect(errs.join("\n")).toContain(
      "in more than one of your spaces (memax-v2, acme). Say which with --space.",
    );
    await gateCommand("G-0012", { option: "1", space: "memax-v2" });
    expect(state.answered).toHaveLength(1);
  });

  it("says why it can't answer", async () => {
    await expect(gateCommand("G-0099", { space: "nowhere" })).rejects.toThrow(
      "exit 1",
    );
    expect(errs.join("\n")).toContain(
      "No space on the V2 record called nowhere",
    );
    state.gates = {};
    await expect(gateCommand("G-0099", {})).rejects.toThrow("exit 1");
    expect(errs.join("\n")).toContain("Gate not found: G-0099.");
    state.gates = { [PROJECT]: gate(PROJECT) };
    await expect(gateCommand("G-0012", { option: "7" })).rejects.toThrow(
      "exit 1",
    );
    expect(errs.join("\n")).toContain('"7" isn\'t one of the options');
    state.refuse = true;
    await expect(gateCommand("G-0012", { option: "1" })).rejects.toThrow(
      "exit 1",
    );
    expect(errs.join("\n")).toContain(
      "Only owners answer decisions in memax-v2.",
    );
  });

  it("shows a gate that already ended without answering it", async () => {
    state.gates = {
      [PROJECT]: gate(PROJECT, {
        status: "answered",
        answer: {
          option: 2,
          label: "Railway",
          memory: { id: "m", ref: "M-0432" },
        },
      }),
    };
    await gateCommand("G-0012", {});
    expect(printed()).toContain("Answered: Railway. Kept as M-0432.");
    await expect(gateCommand("G-0012", { option: "1" })).rejects.toThrow(
      "exit 1",
    );
    expect(errs.join("\n")).toContain("isn't waiting");
    expect(state.answered).toHaveLength(0);
  });
});

describe("parseOption and gateStatusLine", () => {
  const g = gate(PROJECT) as Parameters<typeof parseOption>[0];
  it("reads numbers in range and labels in any case", () => {
    expect(parseOption(g, " 2 ")).toBe(2);
    expect(parseOption(g, "0")).toBeUndefined();
    expect(parseOption(g, "4")).toBeUndefined();
    expect(parseOption(g, "railway")).toBe(2);
    expect(parseOption(g, "Heroku")).toBeUndefined();
  });
  it("says how a gate stands", () => {
    expect(gateStatusLine(g)).toBe("Waiting on you until 2026-10-13.");
    expect(gateStatusLine({ ...g, status: "expired" })).toContain("Expired");
    expect(gateStatusLine({ ...g, status: "withdrawn" })).toContain(
      "Withdrawn",
    );
  });
});
