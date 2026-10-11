import { describe, expect, it, vi } from "vitest";
import { Memax, MemaxError, refusalOf } from "../index.js";
import type { V2 } from "../index.js";

function jsonResponse(body: unknown, init?: ResponseInit): Response {
  return new Response(JSON.stringify(body), {
    status: 200,
    headers: { "Content-Type": "application/json" },
    ...init,
  });
}

function client(response: Response) {
  const fetchMock = vi.fn(async () => response.clone());
  const memax = new Memax({
    apiUrl: "https://api.memax.app",
    apiKey: "mxk_test",
    fetch: fetchMock,
    maxRetries: 0,
  });
  const call = (i = 0) => {
    const [url, init] = fetchMock.mock.calls[i] as unknown as [
      string,
      RequestInit & { headers: Record<string, string> },
    ];
    return {
      url,
      method: init.method,
      headers: init.headers,
      body: init.body === undefined ? undefined : JSON.parse(String(init.body)),
    };
  };
  return { memax, call };
}

const read: V2.Read = {
  id: "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a70",
  ref: "R-5512",
  space_id: "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5c",
  reader_kind: "agent",
  connection_id: "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a61",
  person_id: "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5d",
  agent: "claude-code",
  kind: "compile_load",
  via: "cli",
  session_ref: "7c2f",
  compile: "C-0874",
  memories: 12,
  memory_refs: [],
  read_at: "2026-10-05T14:02:00Z",
  recorded_at: "2026-10-05T14:02:01Z",
};

describe("memax.v2.receipts.checkpoints", () => {
  it("lists the sealed chain with its keys", async () => {
    const page: V2.CheckpointPage = {
      items: [],
      has_more: false,
      seal: { sealed_receipts: 1284, checkpoints: 40, unsealed: 2 },
      keys: [
        {
          key_id: "ed25519:fe812c12f3ab4ce6",
          algorithm: "ed25519",
          public_key: "AAAA",
        },
      ],
    };
    const { memax, call } = client(jsonResponse({ data: page }));
    const got = await memax.v2.receipts.checkpoints("memax-v2", { limit: 5 });
    expect(got.seal.sealed_receipts).toBe(1284);
    expect(call().url).toBe(
      "https://api.memax.app/v2/spaces/memax-v2/checkpoints?limit=5",
    );
  });
});

describe("memax.v2.reads", () => {
  it("lists a space's reads, with the week's count", async () => {
    const page: V2.ReadPage = { items: [read], has_more: false, reads_7d: 41 };
    const { memax, call } = client(jsonResponse({ data: page }));
    const got = await memax.v2.reads.list("memax-v2", { limit: 20 });
    expect(got.reads_7d).toBe(41);
    expect(got.items[0].ref).toBe("R-5512");
    const c = call();
    expect(c.method).toBe("GET");
    expect(c.url).toBe(
      "https://api.memax.app/v2/spaces/memax-v2/reads?limit=20",
    );
  });

  it("reports a compile load with its key", async () => {
    const res: V2.CompileLoadResult = { read };
    const { memax, call } = client(
      jsonResponse({ data: res }, { status: 201 }),
    );
    const got = await memax.v2.reads.recordCompileLoad(
      "memax-v2",
      { compile: "C-0874", agent: "claude-code", session_ref: "7c2f" },
      { idempotencyKey: "load-7c2f", via: "cli" },
    );
    expect(got.read.memories).toBe(12);
    const c = call();
    expect(c.method).toBe("POST");
    expect(c.url).toBe(
      "https://api.memax.app/v2/spaces/memax-v2/compile-loads",
    );
    expect(c.headers["Idempotency-Key"]).toBe("load-7c2f");
    expect(c.headers["X-Memax-Via"]).toBe("cli");
    expect(c.body).toEqual({
      compile: "C-0874",
      agent: "claude-code",
      session_ref: "7c2f",
    });
  });

  it("surfaces a space the agent isn't connected to as a refusal", async () => {
    const { memax } = client(
      jsonResponse(
        {
          error: {
            code: "refused",
            message:
              "Codex isn't connected to elsewhere, so it can't read it. Connect it in Agents.",
            details: {
              policy: { effect: "refuse", code: "agent_not_connected" },
            },
          },
        },
        { status: 403 },
      ),
    );
    const err = await memax.v2.reads
      .recordCompileLoad(
        "elsewhere",
        { compile: "C-0874" },
        { idempotencyKey: "k" },
      )
      .catch((e: unknown) => e);
    expect(err).toBeInstanceOf(MemaxError);
    expect(refusalOf(err)?.code).toBe("agent_not_connected");
  });
});

describe("memax.v2.sessionStarts.start", () => {
  it("names the compiles the files hold and reads what changed", async () => {
    const answer: V2.SessionStart = {
      space: { id: "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5c", slug: "acme-web" },
      files: [
        {
          path: "AGENTS.md",
          here: "C-0881",
          latest: "C-0903",
          hand_edited: false,
          added: [
            {
              refs: ["M-0233"],
              text: "- Use pnpm, not npm, in packages/web. [M-0233]",
            },
          ],
          changed: [],
          gone: ["M-0177"],
          more: 0,
        },
      ],
      forgotten: [
        { ref: "M-0201", with: [], at: "2026-10-10T17:02:00Z", in_files: true },
      ],
      gates: [
        {
          ref: "G-0012",
          question: "Move background jobs to River Pro?",
          agent: "codex",
          expires_at: "2026-10-14T00:00:00Z",
        },
      ],
      notices: [],
      handoffs: [],
    };
    const { memax, call } = client(jsonResponse({ data: answer }));
    const input: V2.SessionStartInput = {
      agent: "copilot",
      session_ref: "8f2c",
      source: "startup",
      files: [
        {
          path: "AGENTS.md",
          compile: "C-0881",
          drift_sha256: "5d1e".padEnd(64, "0"),
        },
      ],
      commit: "a41c9e0",
      branch: "copilot/fix-412",
    };
    const got = await memax.v2.sessionStarts.start("acme-web", input, {
      idempotencyKey: "start-8f2c",
      via: "cli",
    });
    expect(got.files[0].added[0].refs).toEqual(["M-0233"]);
    expect(got.forgotten[0].in_files).toBe(true);
    const c = call();
    expect(c.method).toBe("POST");
    expect(c.url).toBe(
      "https://api.memax.app/v2/spaces/acme-web/session-starts",
    );
    expect(c.headers["Idempotency-Key"]).toBe("start-8f2c");
    expect(c.headers["X-Memax-Via"]).toBe("cli");
    expect(c.body).toEqual(input);
  });
});

describe("memax.v2.checks", () => {
  it("reads the conditions a machine checks", async () => {
    const answer: V2.SpaceConditions = {
      space_id: "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5c",
      repository: "MemaxLabs/memax",
      items: [
        {
          memory: "M-0187",
          conditions: [
            {
              condition: {
                id: "c1",
                kind: "path_exists",
                path: "packages/server/internal/ask/config.go",
                origin: "derived",
                from: "statement",
              },
              hash: "a".repeat(64),
              armed: true,
            },
          ],
        },
      ],
    };
    const { memax, call } = client(jsonResponse({ data: answer }));
    const got = await memax.v2.checks.conditions("memax-v2");
    expect(got.items[0].conditions[0].armed).toBe(true);
    const c = call();
    expect(c.method).toBe("GET");
    expect(c.url).toBe("https://api.memax.app/v2/spaces/memax-v2/conditions");
  });

  it("reports a run and hears what it marked stale", async () => {
    const result: V2.CheckRunResult = {
      run: {
        id: "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5d",
        space_id: "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5c",
        observer: "device",
        repository: "MemaxLabs/memax",
        branch: "main",
        commit: "b".repeat(40),
        commit_at: "2026-10-11T09:00:00Z",
        trigger: "head_moved",
        checked_at: "2026-10-11T09:01:00Z",
        recorded_at: "2026-10-11T09:01:01Z",
        complete: true,
        counts: { holds: 0, fails: 1, unknown: 0, ignored: 0 },
        capped: false,
      },
      flagged: ["M-0187"],
    };
    const { memax, call } = client(
      jsonResponse({ data: result }, { status: 201 }),
    );
    const input: V2.CheckRunInput = {
      repository: "MemaxLabs/memax",
      branch: "main",
      commit: "b".repeat(40),
      commit_at: "2026-10-11T09:00:00Z",
      trigger: "head_moved",
      device: "dev-1",
      complete: true,
      results: [
        {
          memory: "M-0187",
          condition: "c1",
          hash: "a".repeat(64),
          outcome: "fails",
          evidence: {
            change: "renamed",
            path: "packages/server/internal/ask/config.go",
            renamed_to: "packages/server/internal/ask/tiers.go",
          },
        },
      ],
    };
    const got = await memax.v2.checks.report("memax-v2", input, {
      idempotencyKey: "run-b",
      via: "cli",
    });
    expect(got.flagged).toEqual(["M-0187"]);
    const c = call();
    expect(c.method).toBe("POST");
    expect(c.url).toBe("https://api.memax.app/v2/spaces/memax-v2/check-runs");
    expect(c.headers["Idempotency-Key"]).toBe("run-b");
    expect(c.headers["X-Memax-Via"]).toBe("cli");
    expect(c.body).toEqual(input);
  });
});
