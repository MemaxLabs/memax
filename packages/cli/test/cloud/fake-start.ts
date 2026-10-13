// A fake `POST /v2/spaces/{space}/session-starts` for the cloud start's
// tests: it answers what a test sets (an answer, a status, or nothing at
// all), and records every request with its headers and body. And a
// repository as a sandbox clones it: compiled files with Memax's header,
// a .git with HEAD on a branch.
import {
  mkdirSync,
  mkdtempSync,
  realpathSync,
  rmSync,
  writeFileSync,
} from "node:fs";
import {
  createServer,
  type IncomingHttpHeaders,
  type IncomingMessage,
  type Server,
  type ServerResponse,
} from "node:http";
import {
  createServer as createHttpsServer,
  type Server as HttpsServer,
} from "node:https";
import { connect as netConnect, type AddressInfo, type Socket } from "node:net";
import { tmpdir } from "node:os";
import { join } from "node:path";
import type { V2 } from "memax-sdk";

export interface Seen {
  method: string;
  path: string;
  headers: IncomingHttpHeaders;
  body: V2.SessionStartInput;
}

export interface FakeStart {
  url: string;
  requests: Seen[];
  /** The answer's data (the envelope is added). */
  answer: V2.SessionStart;
  /** Answer with this status and an error instead. */
  status?: number;
  /** Never answer. */
  hang?: boolean;
  stop(): Promise<void>;
}

export const COMMIT = "a41c9e0b7d3f2a1c9e0b7d3f2a1c9e0b7d3f2a1c";

export function answer(over: Partial<V2.SessionStart> = {}): V2.SessionStart {
  return {
    space: { id: "11111111-1111-4111-8111-111111111111", slug: "acme-web" },
    files: [],
    forgotten: [],
    gates: [],
    notices: [],
    handoffs: [],
    ...over,
  };
}

/** The fake, over HTTPS when given a key and certificate. */
export async function fakeStart(tls?: {
  key: string;
  cert: string;
}): Promise<FakeStart> {
  let server: Server | HttpsServer;
  const f: FakeStart = {
    url: "",
    requests: [],
    answer: answer(),
    async stop() {
      server.closeAllConnections();
      await new Promise((r) => server.close(r));
    },
  };
  const handle = (req: IncomingMessage, res: ServerResponse) => {
    const chunks: Buffer[] = [];
    req.on("data", (c: Buffer) => chunks.push(c));
    req.on("end", () => {
      let body: V2.SessionStartInput;
      try {
        body = JSON.parse(Buffer.concat(chunks).toString("utf8"));
      } catch {
        body = {} as V2.SessionStartInput;
      }
      f.requests.push({
        method: req.method ?? "",
        path: req.url ?? "",
        headers: req.headers,
        body,
      });
      if (f.hang) return;
      res.setHeader("Content-Type", "application/json");
      if (f.status && f.status !== 200) {
        res.writeHead(f.status);
        res.end(
          JSON.stringify({
            error: { code: "refused", message: "Injected." },
          }),
        );
        return;
      }
      res.writeHead(200);
      res.end(JSON.stringify({ data: f.answer }));
    });
  };
  server = tls ? createHttpsServer(tls, handle) : createServer(handle);
  await new Promise<void>((r) => server.listen(0, "127.0.0.1", r));
  const port = (server.address() as AddressInfo).port;
  f.url = tls ? `https://localhost:${port}` : `http://127.0.0.1:${port}`;
  return f;
}

/**
 * An egress proxy as sandboxes have: CONNECT tunnels, and plain HTTP
 * requests by absolute URL. It records what it was asked for.
 */
export async function fakeProxy(): Promise<{
  url: string;
  connects: string[];
  stop(): Promise<void>;
}> {
  const connects: string[] = [];
  const server = createServer((req, res) => {
    res.writeHead(502);
    res.end();
  });
  server.on("connect", (req: IncomingMessage, client: Socket, head: Buffer) => {
    connects.push(req.url ?? "");
    const [host, port] = (req.url ?? "").split(":");
    const upstream = netConnect(
      Number(port),
      host === "localhost" ? "127.0.0.1" : host,
      () => {
        client.write("HTTP/1.1 200 Connection Established\r\n\r\n");
        upstream.write(head);
        upstream.pipe(client);
        client.pipe(upstream);
      },
    );
    upstream.on("error", () => client.destroy());
    client.on("error", () => upstream.destroy());
  });
  await new Promise<void>((r) => server.listen(0, "127.0.0.1", r));
  return {
    url: `http://127.0.0.1:${(server.address() as AddressInfo).port}`,
    connects,
    async stop() {
      server.closeAllConnections();
      await new Promise((r) => server.close(r));
    },
  };
}

export function header(ref: string, slug = "acme-web"): string {
  return `<!-- Compiled by Memax from ${slug} at 2026-10-07 09:00 UTC (${ref}). Edit it at https://memax.app/${slug}/brief; edits here come back as proposals. -->`;
}

export interface Sandbox {
  base: string;
  /** The sandbox's HOME: no ~/.memax. */
  home: string;
  repo: string;
  /** Writes a compiled file. */
  compiled(ref: string, lines: string[], path?: string, slug?: string): void;
  cleanup(): void;
}

/** A clone in a fresh sandbox: HEAD on `copilot/fix-412` at COMMIT. */
export function sandbox(): Sandbox {
  const base = realpathSync(mkdtempSync(join(tmpdir(), "memax-cloud-")));
  const home = join(base, "home");
  const repo = join(base, "repo");
  mkdirSync(home, { recursive: true });
  mkdirSync(join(repo, ".git", "refs", "heads", "copilot"), {
    recursive: true,
  });
  writeFileSync(
    join(repo, ".git", "HEAD"),
    "ref: refs/heads/copilot/fix-412\n",
  );
  writeFileSync(
    join(repo, ".git", "refs", "heads", "copilot", "fix-412"),
    `${COMMIT}\n`,
  );
  return {
    base,
    home,
    repo,
    compiled(ref, lines, path = "AGENTS.md", slug = "acme-web") {
      writeFileSync(
        join(repo, path),
        [
          header(ref, slug),
          "# Acme web brief",
          "",
          "## Decisions",
          ...lines,
          "",
        ].join("\n"),
      );
    },
    cleanup() {
      rmSync(base, { recursive: true, force: true });
    },
  };
}
