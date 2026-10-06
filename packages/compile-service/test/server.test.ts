import { readFileSync } from "node:fs";
import { request } from "node:http";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

import { compile, parseBack } from "@memaxlabs/compiler";
import { afterEach, describe, expect, it } from "vitest";

import { toPointer } from "../src/errors.js";
import type { Level, Logger } from "../src/log.js";
import { createService, type Service } from "../src/server.js";

const here = dirname(fileURLToPath(import.meta.url));
const fixture = join(here, "../../compiler/test/fixtures/demo-memax-v2");
const demoInput = JSON.parse(readFileSync(join(fixture, "input.json"), "utf8"));
const demoAgents = readFileSync(join(fixture, "expected/AGENTS.md"), "utf8");

interface Line {
  level: Level;
  msg: string;
  fields: Record<string, unknown>;
}

function memoryLogger(): Logger & { lines: Line[] } {
  const lines: Line[] = [];
  return {
    lines,
    log: (level, msg, fields = {}) => lines.push({ level, msg, fields }),
  };
}

let running: Service[] = [];

async function start(opts: Parameters<typeof createService>[0] = {}) {
  const log = memoryLogger();
  const service = createService({ log, ...opts });
  const addr = await service.listen(0, "127.0.0.1");
  running.push(service);
  return { service, log, base: `http://127.0.0.1:${addr.port}` };
}

afterEach(async () => {
  await Promise.all(running.map((s) => s.close(100)));
  running = [];
});

async function post(
  base: string,
  path: string,
  body: unknown,
  headers: Record<string, string> = {},
) {
  const res = await fetch(base + path, {
    method: "POST",
    headers: { "content-type": "application/json", ...headers },
    body: typeof body === "string" ? body : JSON.stringify(body),
  });
  return {
    status: res.status,
    headers: res.headers,
    json: (await res.json()) as Record<string, any>,
  };
}

describe("POST /compile", () => {
  it("compiles the demo space exactly as the compiler does", async () => {
    const { base, log } = await start();
    const res = await post(base, "/compile", demoInput, {
      "x-request-id": "req-1",
    });
    expect(res.status).toBe(200);
    expect(res.headers.get("x-request-id")).toBe("req-1");
    expect(res.json).toEqual(JSON.parse(JSON.stringify(compile(demoInput))));
    const agents = res.json.files.find(
      (f: { path: string }) => f.path === "AGENTS.md",
    );
    expect(agents.content).toBe(demoAgents);
    expect(res.json.copies[0].label).toBe("ChatGPT project instructions");
    expect(log.lines).toHaveLength(1);
    expect(log.lines[0].fields).toMatchObject({
      request_id: "req-1",
      path: "/compile",
      status: 200,
    });
  });

  it("refuses an invalid input with every issue as a JSON pointer", async () => {
    const { base } = await start();
    const bad = structuredClone(demoInput);
    bad.memories[2].state = "proposed";
    bad.brief.sections[1].items[0].ref = "nope";
    bad.targets[0].size_budget = 10;
    const res = await post(base, "/compile", bad);
    expect(res.status).toBe(422);
    expect(res.json.error.code).toBe("invalid_input");
    const paths = res.json.error.issues.map(
      (i: { instancePath: string }) => i.instancePath,
    );
    expect(paths).toEqual(
      expect.arrayContaining([
        "/memories/2/state",
        "/brief/sections/1/items/0/ref",
        "/targets/0/size_budget",
      ]),
    );
    for (const issue of res.json.error.issues)
      expect(issue.message).toBeTruthy();
  });

  it("refuses quarantined content", async () => {
    const { base } = await start();
    const bad = structuredClone(demoInput);
    bad.memories[0].trust = "external";
    const res = await post(base, "/compile", bad);
    expect(res.status).toBe(422);
    expect(res.json.error.issues[0].message).toMatch(/quarantined/);
  });

  it("never logs what it compiles", async () => {
    const { base, log } = await start();
    await post(base, "/compile", demoInput);
    await post(base, "/compile", { ...demoInput, version: 9 });
    const logged = JSON.stringify(log.lines);
    for (const m of demoInput.memories)
      expect(logged).not.toContain(m.statement);
    expect(logged).not.toContain(demoInput.brief.title);
  });
});

describe("POST /parse-back", () => {
  const compiled = compile(demoInput).files.find(
    (f) => f.path === "AGENTS.md",
  )!;
  const edited = compiled.content
    .replace(
      "Never run `npm install` at the root.",
      "Never run `npm install` anywhere.",
    )
    .replace(
      "- Every write tool returns a receipt ID the caller can cite. [M-0112]\n",
      "",
    )
    .replace(
      "## Open\n",
      "## Open\n\n- Prefer named exports in React components.\n",
    );

  it("reads a hand edit back against the delivered file", async () => {
    const { base } = await start();
    const res = await post(base, "/parse-back", {
      last: { content: compiled.content, drift_sha256: compiled.drift_sha256 },
      current: edited,
    });
    expect(res.status).toBe(200);
    const expected = parseBack(compiled, edited);
    expect(res.json.changes).toEqual(expected.changes);
    expect(res.json.drift).toEqual(expected.drift);
    expect(res.json.drifted).toBe(true);
    expect(res.json.drift_sha256).toMatch(/^[0-9a-f]{64}$/);
    const kinds = res.json.changes.map(
      (c: { kind: string; ref?: string }) => `${c.kind}:${c.ref ?? ""}`,
    );
    expect(kinds).toEqual(
      expect.arrayContaining(["edit:M-0071", "new:", "remove:M-0112"]),
    );
  });

  it("says when nothing drifted, line endings aside", async () => {
    const { base } = await start();
    const res = await post(base, "/parse-back", {
      last: { content: compiled.content, drift_sha256: compiled.drift_sha256 },
      current: compiled.content.replace(/\n/g, "\r\n"),
    });
    expect(res.json.drifted).toBe(false);
    expect(res.json.changes).toEqual([]);
    expect(res.json.drift_sha256).toBe(compiled.drift_sha256);
  });

  it("takes the last file as a plain string", async () => {
    const { base } = await start();
    const res = await post(base, "/parse-back", {
      last: compiled.content,
      current: edited,
    });
    expect(res.json.drifted).toBe(true);
    expect(res.json.changes.length).toBe(3);
  });

  it("refuses a malformed request with every issue", async () => {
    const { base } = await start();
    const res = await post(base, "/parse-back", {
      last: { content: 3, drift_sha256: "x" },
    });
    expect(res.status).toBe(422);
    expect(
      res.json.error.issues.map(
        (i: { instancePath: string }) => i.instancePath,
      ),
    ).toEqual(["/last/content", "/last/drift_sha256", "/current"]);
  });
});

describe("the server", () => {
  it("answers health with the contract and the adapters", async () => {
    const { base } = await start();
    const res = await fetch(base + "/health");
    expect(res.status).toBe(200);
    const body = (await res.json()) as Record<string, any>;
    expect(body.status).toBe("ok");
    expect(body.contract_version).toBe(1);
    const defaults = body.adapters
      .filter((a: { default: boolean }) => a.default)
      .map((a: { kind: string }) => a.kind);
    expect(defaults).toEqual([
      "agents_md",
      "claude_md",
      "cursor_mdc",
      "chatgpt",
    ]);
    expect(
      body.adapters.find((a: { kind: string }) => a.kind === "chatgpt")
        .default_path,
    ).toBeNull();
  });

  it("refuses what isn't a JSON request", async () => {
    const { base } = await start();
    const notJSON = await post(base, "/compile", "{", {});
    expect(notJSON.status).toBe(400);
    expect(notJSON.json.error.code).toBe("invalid_json");
    const text = await fetch(base + "/compile", {
      method: "POST",
      headers: { "content-type": "text/plain" },
      body: "{}",
    });
    expect(text.status).toBe(415);
    const missing = await fetch(base + "/nowhere");
    expect(missing.status).toBe(404);
    expect(((await missing.json()) as Record<string, any>).error.code).toBe(
      "not_found",
    );
    const wrong = await fetch(base + "/compile");
    expect(wrong.status).toBe(405);
    expect(wrong.headers.get("allow")).toBe("POST");
  });

  it("refuses a body over the limit, declared or streamed", async () => {
    const { base } = await start({ maxBodyBytes: 1024 });
    const big = JSON.stringify({ last: "x".repeat(2000), current: "" });
    const declared = await post(base, "/parse-back", big);
    expect(declared.status).toBe(413);
    expect(declared.json.error.code).toBe("too_large");
    const streamed = await new Promise<number>((resolve, reject) => {
      const url = new URL(base + "/parse-back");
      const req = request(
        {
          host: url.hostname,
          port: url.port,
          path: url.pathname,
          method: "POST",
          headers: {
            "content-type": "application/json",
            "transfer-encoding": "chunked",
          },
        },
        (res) => {
          res.resume();
          resolve(res.statusCode ?? 0);
        },
      );
      req.on("error", reject);
      req.write("[".repeat(800));
      req.end("1".repeat(800));
    });
    expect(streamed).toBe(413);
  });

  it("finishes in-flight requests on close, and answers 503 while draining", async () => {
    const { base, service } = await start();
    const url = new URL(base + "/compile");
    const slow = new Promise<number>((resolve, reject) => {
      const req = request(
        {
          host: url.hostname,
          port: url.port,
          path: url.pathname,
          method: "POST",
          headers: { "content-type": "application/json" },
        },
        (res) => {
          res.resume();
          resolve(res.statusCode ?? 0);
        },
      );
      req.on("error", reject);
      const body = JSON.stringify(demoInput);
      req.write(body.slice(0, 10));
      setTimeout(() => req.end(body.slice(10)), 150);
    });
    await new Promise((r) => setTimeout(r, 50));
    const closing = service.close(5_000);
    expect(service.draining()).toBe(true);
    expect(await slow).toBe(200);
    await closing;
    await expect(fetch(base + "/health")).rejects.toThrow();
    running = running.filter((s) => s !== service);
  });
});

describe("toPointer", () => {
  it.each([
    ["input", ""],
    ["version", "/version"],
    ["memories[3].state", "/memories/3/state"],
    [
      "brief.sections[0].items[2].cites[1]",
      "/brief/sections/0/items/2/cites/1",
    ],
    ["targets", "/targets"],
  ])("%s → %s", (path, pointer) => {
    expect(toPointer(path)).toBe(pointer);
  });
});
