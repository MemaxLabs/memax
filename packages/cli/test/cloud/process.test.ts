// The cloud start as a cloud sandbox runs it: the built `@memaxlabs/start`
// bundle (and `memax hook cloud-start`), each in its own process, with a
// bare environment (a fresh HOME, PATH, the API's URL and the key; no
// ~/.memax, no daemon) and the agent's event on stdin. A preload records
// every process it would start and every socket. HTTPS goes through an
// egress proxy's CONNECT tunnel, as in the sandboxes.
import { execFileSync, spawn } from "node:child_process";
import { mkdtempSync, readdirSync, readFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterAll, beforeAll, beforeEach, describe, expect, it } from "vitest";
import {
  answer,
  fakeProxy,
  fakeStart,
  sandbox,
  type FakeStart,
  type Sandbox,
} from "./fake-start.js";

const cli = join(import.meta.dirname, "..", "..");
const start = join(cli, "..", "start");
const cache = join(cli, "node_modules", ".cache", "memax-cloud-test");
const bundle = join(cache, "memax-start.js");
const built = join(cache, "dist");
const preload = join(import.meta.dirname, "..", "hook", "preload-events.mjs");
const KEY = "mxk_" + "7".repeat(64);

let fake: FakeStart;
let sb: Sandbox;
let events: string;

beforeAll(async () => {
  rmSync(cache, { recursive: true, force: true });
  execFileSync(
    process.execPath,
    [join(start, "scripts", "build.mjs"), "--outfile", bundle],
    {
      cwd: start,
      stdio: "inherit",
    },
  );
  execFileSync(
    process.execPath,
    [
      join(cli, "node_modules", "typescript", "bin", "tsc"),
      "-p",
      cli,
      "--outDir",
      built,
    ],
    { stdio: "inherit" },
  );
  fake = await fakeStart();
}, 180_000);
afterAll(async () => {
  await fake?.stop();
});
beforeEach(() => {
  sb?.cleanup();
  sb = sandbox();
  events = join(sb.base, "events.log");
  fake.requests.length = 0;
  fake.hang = false;
  fake.status = undefined;
  fake.answer = answer({
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
        gone: [],
        more: 0,
      },
    ],
  });
  sb.compiled("C-0881", ["- Deploy to staging first. [M-0219]"]);
});

interface Run {
  code: number | null;
  stdout: string;
  stderr: string;
  ms: number;
  pid: number;
}

function sandboxEnv(over: Record<string, string> = {}): NodeJS.ProcessEnv {
  return {
    HOME: sb.home,
    PATH: process.env.PATH ?? "/usr/bin:/bin",
    MEMAX_API_URL: fake.url,
    MEMAX_START_KEY: KEY,
    MEMAX_TEST_EVENTS: events,
    NODE_OPTIONS: `--import ${preload}`,
    ...over,
  };
}

function runStart(
  args: string[],
  stdin: Record<string, unknown>,
  env: NodeJS.ProcessEnv,
): Promise<Run> {
  const started = performance.now();
  const child = spawn(process.execPath, args, {
    cwd: sb.repo,
    env,
    stdio: ["pipe", "pipe", "pipe"],
  });
  let stdout = "";
  let stderr = "";
  child.stdout!.on("data", (b) => (stdout += String(b)));
  child.stderr!.on("data", (b) => (stderr += String(b)));
  child.stdin!.end(JSON.stringify(stdin));
  return new Promise((resolve) =>
    child.on("exit", (code) =>
      resolve({
        code,
        stdout,
        stderr,
        ms: performance.now() - started,
        pid: child.pid!,
      }),
    ),
  );
}

const copilotEvent = () => ({
  sessionId: `cp-${Math.random().toString(16).slice(2)}`,
  cwd: sb.repo,
  source: "new",
  initialPrompt: "Fix issue 412",
});

function eventsOf(pid: number): string[] {
  try {
    return readFileSync(events, "utf8")
      .trim()
      .split("\n")
      .filter((l) => l.startsWith(`${pid} `))
      .map((l) => l.slice(String(pid).length + 1));
  } catch {
    return [];
  }
}

describe("@memaxlabs/start in a cloud sandbox", () => {
  it("is one file that imports only Node's built-ins", () => {
    const src = readFileSync(bundle, "utf8");
    expect(src.startsWith("#!/usr/bin/env node\n")).toBe(true);
    const imports = [...src.matchAll(/^import .* from "([^"]+)";$/gm)].map(
      (m) => m[1],
    );
    expect(imports.length).toBeGreaterThan(0);
    expect(imports.filter((i) => !i.startsWith("node:"))).toEqual([]);
    expect(src).not.toMatch(/\bimport\(\s*["'](?!node:)/);
  });

  it("prints Copilot's block from one request, exits 0, and starts no process", async () => {
    const r = await runStart(
      [bundle, "--agent", "copilot"],
      copilotEvent(),
      sandboxEnv(),
    );
    expect(r.code).toBe(0);
    const block = (JSON.parse(r.stdout) as { additionalContext: string })
      .additionalContext;
    expect(block).toContain(
      "AGENTS.md here is C-0881; the space has compiled C-0903 since.",
    );
    expect(block).toContain("- Use pnpm, not npm, in packages/web. [M-0233]");
    expect(fake.requests).toHaveLength(1);
    expect(fake.requests[0].headers["user-agent"]).toBe("memax-start/1.0.0");
    expect(JSON.stringify(fake.requests[0].body)).not.toContain("issue 412");
    expect(eventsOf(r.pid).filter((e) => e.startsWith("spawn "))).toEqual([]);
    console.log(`memax-start, the package installed: ${Math.round(r.ms)} ms`);
    expect(r.ms).toBeLessThan(2_000);
  });

  it("never prints the key, with --debug either, and says why it was quiet", async () => {
    fake.status = 401;
    const r = await runStart(
      [bundle, "--agent", "claude-code", "--debug"],
      copilotEvent(),
      sandboxEnv(),
    );
    expect(r).toMatchObject({ code: 0, stdout: "" });
    expect(r.stderr).toContain(
      "memax cloud-start: the API answered 401 refused",
    );
    expect(r.stdout + r.stderr).not.toContain(KEY);
    expect(r.stdout + r.stderr).not.toContain("7777");
  });

  it("prints nothing within 3 s when the API never answers", async () => {
    fake.hang = true;
    const r = await runStart(
      [bundle, "--agent", "copilot"],
      copilotEvent(),
      sandboxEnv(),
    );
    expect(r).toMatchObject({ code: 0, stdout: "" });
    expect(r.ms).toBeLessThan(3_000);
  });

  it("prints nothing and exits 0 against a black hole", async () => {
    const r = await runStart(
      [bundle, "--agent", "copilot"],
      copilotEvent(),
      sandboxEnv({ MEMAX_API_URL: "http://10.255.255.1:81" }),
    );
    expect(r).toMatchObject({ code: 0, stdout: "" });
    expect(r.ms).toBeLessThan(3_500);
  });

  it("keeps its own files under $HOME/.memax/start, which never make the sandbox a person's machine", async () => {
    const first = await runStart(
      [bundle, "--agent", "copilot"],
      copilotEvent(),
      sandboxEnv(),
    );
    expect(first.stdout).toContain("[M-0233]");
    expect(readdirSync(join(sb.home, ".memax"))).toEqual(["start"]);
    // A new session in the same sandbox hears it all again: the committed
    // files it loads didn't change. The same session resuming, only news.
    const event = copilotEvent();
    const second = await runStart(
      [bundle, "--agent", "copilot"],
      event,
      sandboxEnv(),
    );
    expect(second.stdout).toContain("[M-0233]");
    const resumed = await runStart(
      [bundle, "--agent", "copilot"],
      { ...event, source: "resume" },
      sandboxEnv(),
    );
    expect(fake.requests).toHaveLength(3);
    expect(resumed).toMatchObject({ code: 0, stdout: "" });
  });

  it("runs the same as `memax hook cloud-start`", async () => {
    const r = await runStart(
      [join(built, "bin.js"), "hook", "cloud-start", "--agent", "codex"],
      copilotEvent(),
      sandboxEnv(),
    );
    expect(r.code).toBe(0);
    expect(r.stdout.startsWith('<memax-context space="acme-web">')).toBe(true);
    expect(r.stdout).toContain("[M-0233]");
  });
});

describe("through an egress proxy", () => {
  let dir = "";
  let tlsFake: FakeStart | undefined;
  let ca = "";
  beforeAll(async () => {
    try {
      execFileSync("openssl", ["version"], { stdio: "ignore" });
    } catch {
      return; // no openssl: the test below says so
    }
    dir = mkdtempSync(join(tmpdir(), "memax-cloud-tls-"));
    execFileSync(
      "openssl",
      [
        "req",
        "-x509",
        "-newkey",
        "rsa:2048",
        "-nodes",
        "-days",
        "1",
        "-keyout",
        join(dir, "key.pem"),
        "-out",
        join(dir, "cert.pem"),
        "-subj",
        "/CN=localhost",
        "-addext",
        "subjectAltName=DNS:localhost",
      ],
      { stdio: "ignore" },
    );
    ca = join(dir, "cert.pem");
    tlsFake = await fakeStart({
      key: readFileSync(join(dir, "key.pem"), "utf8"),
      cert: readFileSync(ca, "utf8"),
    });
    tlsFake.answer = answer({
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
          gone: [],
          more: 0,
        },
      ],
    });
  }, 60_000);
  afterAll(async () => {
    await tlsFake?.stop();
    if (dir) rmSync(dir, { recursive: true, force: true });
  });

  it("tunnels HTTPS with CONNECT, and NO_PROXY skips the proxy", async (ctx) => {
    if (!tlsFake) ctx.skip();
    const proxy = await fakeProxy();
    try {
      const env = sandboxEnv({
        MEMAX_API_URL: tlsFake!.url,
        HTTPS_PROXY: proxy.url,
        NODE_EXTRA_CA_CERTS: ca,
      });
      const r = await runStart(
        [bundle, "--agent", "copilot"],
        copilotEvent(),
        env,
      );
      expect(r.code).toBe(0);
      expect(r.stdout).toContain("[M-0233]");
      expect(proxy.connects).toEqual([
        `localhost:${new URL(tlsFake!.url).port}`,
      ]);
      expect(tlsFake!.requests.at(-1)?.headers.authorization).toBe(
        `Bearer ${KEY}`,
      );

      const direct = await runStart(
        [bundle, "--agent", "copilot"],
        copilotEvent(),
        { ...env, NO_PROXY: "localhost" },
      );
      expect(direct.stdout).toContain("[M-0233]");
      expect(proxy.connects).toHaveLength(1);
    } finally {
      await proxy.stop();
    }
  });
});
