/**
 * The compile service's HTTP contract, run against every entry: the Node
 * server (server.test.ts) and the Workers entry under workerd
 * (worker.test.ts). Both call the same handler, and these tests keep it
 * that way: anything an entry answers differently fails here.
 */
import { readFileSync } from "node:fs";
import { request } from "node:http";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

import { compile, parseBack } from "@memaxlabs/compiler";
import { afterAll, describe, expect, it } from "vitest";

const here = dirname(fileURLToPath(import.meta.url));
const fixture = join(here, "../../compiler/test/fixtures/demo-memax-v2");
export const demoInput = JSON.parse(
  readFileSync(join(fixture, "input.json"), "utf8"),
);
const demoAgents = readFileSync(join(fixture, "expected/AGENTS.md"), "utf8");

/** The token every suite's service requires. */
export const TOKEN = "compile-service-test-token-0123456789abcdef";

export interface Running {
  base: string;
  /** Everything the service logged so far, as one string. */
  logs(): string;
}

export interface Config {
  maxBodyBytes?: number;
}

/** Starts (or reuses) a service for a config; the suite stops none itself. */
export type Start = (config: Config) => Promise<Running>;

export interface Posted {
  status: number;
  headers: Headers;
  json: Record<string, any>;
}

export async function post(
  base: string,
  path: string,
  body: unknown,
  headers: Record<string, string> = {},
): Promise<Posted> {
  const res = await fetch(base + path, {
    method: "POST",
    headers: {
      "content-type": "application/json",
      authorization: `Bearer ${TOKEN}`,
      ...headers,
    },
    body: typeof body === "string" ? body : JSON.stringify(body),
  });
  return {
    status: res.status,
    headers: res.headers,
    json: (await res.json()) as Record<string, any>,
  };
}

/** Reads the logs until `check` holds (workerd's arrive asynchronously). */
export async function eventually(
  read: () => string,
  check: (logs: string) => boolean,
): Promise<string> {
  for (let i = 0; i < 50; i++) {
    const logs = read();
    if (check(logs)) return logs;
    await new Promise((r) => setTimeout(r, 20));
  }
  return read();
}

export function contract(
  name: string,
  start: Start,
  stop: () => Promise<void>,
) {
  afterAll(stop);

  describe(`${name}: POST /compile`, () => {
    it("compiles the demo space exactly as the compiler does", async () => {
      const { base, logs } = await start({});
      const res = await post(base, "/compile", demoInput, {
        "x-request-id": "req-compile-demo",
      });
      expect(res.status).toBe(200);
      expect(res.headers.get("x-request-id")).toBe("req-compile-demo");
      expect(res.json).toEqual(JSON.parse(JSON.stringify(compile(demoInput))));
      const agents = res.json.files.find(
        (f: { path: string }) => f.path === "AGENTS.md",
      );
      expect(agents.content).toBe(demoAgents);
      expect(res.json.copies[0].label).toBe("ChatGPT project instructions");
      const logged = await eventually(logs, (l) =>
        l.includes("req-compile-demo"),
      );
      const line = logged
        .split("\n")
        .find((l) => l.includes("req-compile-demo"));
      expect(JSON.parse(line ?? "{}")).toMatchObject({
        msg: "request",
        request_id: "req-compile-demo",
        method: "POST",
        path: "/compile",
        status: 200,
        service: "compile",
      });
    });

    it("refuses an invalid input with every issue as a JSON pointer", async () => {
      const { base } = await start({});
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
      const { base } = await start({});
      const bad = structuredClone(demoInput);
      bad.memories[0].trust = "external";
      const res = await post(base, "/compile", bad);
      expect(res.status).toBe(422);
      expect(res.json.error.issues[0].message).toMatch(/quarantined/);
    });

    it("never logs what it compiles, or the token", async () => {
      const { base, logs } = await start({});
      await post(base, "/compile", demoInput, { "x-request-id": "req-quiet" });
      await post(
        base,
        "/compile",
        { ...demoInput, version: 9 },
        { "x-request-id": "req-quiet-2" },
      );
      await post(base, "/compile", demoInput, {
        authorization: "Bearer wrong-token-never-logged",
        "x-request-id": "req-quiet-3",
      });
      const logged = await eventually(logs, (l) => l.includes("req-quiet-3"));
      for (const m of demoInput.memories)
        expect(logged).not.toContain(m.statement);
      expect(logged).not.toContain(demoInput.brief.title);
      expect(logged).not.toContain(TOKEN);
      expect(logged).not.toContain("wrong-token-never-logged");
    });
  });

  describe(`${name}: POST /parse-back`, () => {
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
      const { base } = await start({});
      const res = await post(base, "/parse-back", {
        last: {
          content: compiled.content,
          drift_sha256: compiled.drift_sha256,
        },
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
      const { base } = await start({});
      const res = await post(base, "/parse-back", {
        last: {
          content: compiled.content,
          drift_sha256: compiled.drift_sha256,
        },
        current: compiled.content.replace(/\n/g, "\r\n"),
      });
      expect(res.json.drifted).toBe(false);
      expect(res.json.changes).toEqual([]);
      expect(res.json.drift_sha256).toBe(compiled.drift_sha256);
    });

    it("takes the last file as a plain string", async () => {
      const { base } = await start({});
      const res = await post(base, "/parse-back", {
        last: compiled.content,
        current: edited,
      });
      expect(res.json.drifted).toBe(true);
      expect(res.json.changes.length).toBe(3);
    });

    it("refuses a malformed request with every issue", async () => {
      const { base } = await start({});
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

  describe(`${name}: the service`, () => {
    it("answers health with the contract and the adapters, without a token", async () => {
      const { base } = await start({});
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
      const head = await fetch(base + "/health", { method: "HEAD" });
      expect(head.status).toBe(200);
      expect(await head.text()).toBe("");
    });

    it("refuses every other route without the token, before reading the body", async () => {
      const { base } = await start({});
      for (const authorization of [
        undefined,
        "Bearer wrong-token",
        `Basic ${TOKEN}`,
        `Bearer ${TOKEN}x`,
        `Bearer ${TOKEN.slice(0, -1)}`,
        "Bearer ",
      ]) {
        const headers: Record<string, string> = {
          "content-type": "application/json",
        };
        if (authorization) headers.authorization = authorization;
        for (const path of ["/compile", "/parse-back", "/nowhere"]) {
          const res = await fetch(base + path, {
            method: "POST",
            headers,
            body: JSON.stringify(demoInput),
          });
          expect(res.status, `${path} with ${authorization}`).toBe(401);
          expect(res.headers.get("www-authenticate")).toBe(
            'Bearer realm="memax-compile"',
          );
          const body = (await res.json()) as Record<string, any>;
          expect(body.error.code).toBe("unauthorized");
        }
      }
      // A bearer scheme in any case, and the right token, passes.
      const ok = await post(base, "/compile", demoInput, {
        authorization: `bearer ${TOKEN}`,
      });
      expect(ok.status).toBe(200);
    });

    it("refuses what isn't a JSON request", async () => {
      const { base } = await start({});
      const auth = { authorization: `Bearer ${TOKEN}` };
      const notJSON = await post(base, "/compile", "{", {});
      expect(notJSON.status).toBe(400);
      expect(notJSON.json.error.code).toBe("invalid_json");
      const text = await fetch(base + "/compile", {
        method: "POST",
        headers: { "content-type": "text/plain", ...auth },
        body: "{}",
      });
      expect(text.status).toBe(415);
      const missing = await fetch(base + "/nowhere", { headers: auth });
      expect(missing.status).toBe(404);
      expect(((await missing.json()) as Record<string, any>).error.code).toBe(
        "not_found",
      );
      const wrong = await fetch(base + "/compile", { headers: auth });
      expect(wrong.status).toBe(405);
      expect(wrong.headers.get("allow")).toBe("POST");
      const health = await fetch(base + "/health", { method: "POST" });
      expect(health.status).toBe(405);
      expect(health.headers.get("allow")).toBe("GET");
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
              authorization: `Bearer ${TOKEN}`,
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
      // Under the limit still compiles.
      const small = await post(base, "/parse-back", {
        last: "a\n",
        current: "a\n",
      });
      expect(small.status).toBe(200);
    });
  });
}
