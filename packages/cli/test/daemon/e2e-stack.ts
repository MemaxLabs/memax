// The real stack for the end-to-end test: a fresh Postgres database with
// the migrations, the dev seeder's memax-v2, the compile service (Node),
// the Go worker and API server, and a small S3 stand-in for object storage.
import { execFileSync, spawn, type ChildProcess } from "node:child_process";
import { createHmac, randomBytes } from "node:crypto";
import { existsSync, mkdtempSync } from "node:fs";
import { createServer, type Server } from "node:http";
import { createServer as createNetServer, type AddressInfo } from "node:net";
import { tmpdir } from "node:os";
import { join } from "node:path";

export const REPO = join(import.meta.dirname, "..", "..", "..", "..");
const SERVER = join(REPO, "packages", "server");

export interface Stack {
  apiUrl: string;
  dbUrl: string;
  zz: string;
  token: string;
  logs: string[];
  stop(): Promise<void>;
}

const freePort = () =>
  new Promise<number>((resolve, reject) => {
    const s = createNetServer();
    s.listen(0, "127.0.0.1", () => {
      const port = (s.address() as AddressInfo).port;
      s.close(() => resolve(port));
    });
    s.on("error", reject);
  });

function b64url(v: Buffer | string): string {
  return Buffer.from(v).toString("base64url");
}

/** A session token as the server signs them (internal/auth/jwt.go). */
export function sessionToken(
  sub: string,
  secret: string,
  surface = "cli",
): string {
  const now = Math.floor(Date.now() / 1000);
  const head = b64url('{"alg":"HS256","typ":"JWT"}');
  const body = b64url(
    JSON.stringify({ sub, exp: now + 7200, iat: now, surface }),
  );
  const sig = createHmac("sha256", secret)
    .update(`${head}.${body}`)
    .digest("base64url");
  return `${head}.${body}.${sig}`;
}

/** Enough of S3 for the compile artifacts: PUT and GET by key. */
function fakeS3(): Promise<{ url: string; server: Server }> {
  const objects = new Map<string, { body: Buffer; type: string }>();
  const server = createServer((req, res) => {
    const chunks: Buffer[] = [];
    req.on("data", (c: Buffer) => chunks.push(c));
    req.on("end", () => {
      const key = decodeURIComponent((req.url ?? "/").split("?")[0]);
      let body: Buffer = Buffer.concat(chunks);
      const sha = String(req.headers["x-amz-content-sha256"] ?? "");
      if (
        sha.startsWith("STREAMING") ||
        String(req.headers["content-encoding"] ?? "").includes("aws-chunked")
      ) {
        body = unchunk(body);
      }
      if (req.method === "PUT") {
        objects.set(key, {
          body,
          type: String(
            req.headers["content-type"] ?? "application/octet-stream",
          ),
        });
        res.writeHead(200, { ETag: '"x"' });
        return res.end();
      }
      if (req.method === "GET" || req.method === "HEAD") {
        const o = objects.get(key);
        if (!o) {
          res.writeHead(404, { "Content-Type": "application/xml" });
          return res.end(
            "<Error><Code>NoSuchKey</Code><Message>no such key</Message></Error>",
          );
        }
        res.writeHead(200, {
          "Content-Type": o.type,
          "Content-Length": o.body.length,
        });
        return res.end(req.method === "GET" ? o.body : undefined);
      }
      if (req.method === "DELETE") {
        objects.delete(key);
        res.writeHead(204);
        return res.end();
      }
      res.writeHead(405);
      res.end();
    });
  });
  return new Promise((resolve) =>
    server.listen(0, "127.0.0.1", () =>
      resolve({
        url: `http://127.0.0.1:${(server.address() as AddressInfo).port}`,
        server,
      }),
    ),
  );
}

/** aws-chunked: `<hex>[;chunk-signature=…]\r\n<data>\r\n` … `0…\r\n<trailers>\r\n`. */
function unchunk(b: Buffer): Buffer {
  const out: Buffer[] = [];
  let i = 0;
  for (;;) {
    const nl = b.indexOf("\r\n", i);
    if (nl < 0) break;
    const size = parseInt(b.subarray(i, nl).toString().split(";")[0], 16);
    if (!size) break;
    out.push(b.subarray(nl + 2, nl + 2 + size));
    i = nl + 2 + size + 2;
  }
  return Buffer.concat(out);
}

function waitForLine(
  child: ChildProcess,
  test: (line: string) => RegExpMatchArray | null,
  ms: number,
  what: string,
  logs: string[],
) {
  return new Promise<RegExpMatchArray>((resolve, reject) => {
    const timer = setTimeout(
      () =>
        reject(
          new Error(
            `${what} didn't start within ${ms} ms:\n${logs.slice(-20).join("\n")}`,
          ),
        ),
      ms,
    );
    let buf = "";
    const onData = (chunk: Buffer) => {
      buf += String(chunk);
      let nl: number;
      while ((nl = buf.indexOf("\n")) >= 0) {
        const line = buf.slice(0, nl);
        buf = buf.slice(nl + 1);
        logs.push(`[${what}] ${line}`);
        const m = test(line);
        if (m) {
          clearTimeout(timer);
          resolve(m);
        }
      }
    };
    child.stdout?.on("data", onData);
    child.stderr?.on("data", onData);
    child.once("exit", (code) =>
      reject(
        new Error(
          `${what} exited with ${code}:\n${logs.slice(-20).join("\n")}`,
        ),
      ),
    );
  });
}

async function waitHealthy(url: string, ms: number): Promise<void> {
  const start = Date.now();
  while (Date.now() - start < ms) {
    try {
      const r = await fetch(`${url}/health`);
      if (r.ok && ((await r.json()) as { ready?: boolean }).ready) return;
    } catch {
      // not yet
    }
    await new Promise((r) => setTimeout(r, 200));
  }
  throw new Error(`the API server at ${url} wasn't ready within ${ms} ms`);
}

export async function startStack(): Promise<Stack> {
  const logs: string[] = [];
  const children: ChildProcess[] = [];
  const admin =
    process.env.MEMAX_E2E_DATABASE_URL ??
    process.env.TEST_DATABASE_URL ??
    "postgres://memax:memax@localhost:5432/memax?sslmode=disable";
  const db = `memax_cli_e2e_${randomBytes(4).toString("hex")}`;
  const u = new URL(admin);
  u.pathname = `/${db}`;
  const dbUrl = u.toString();
  const secret = randomBytes(24).toString("hex");

  // Go binaries: prebuilt (MEMAX_E2E_BIN), or built here.
  let bin = process.env.MEMAX_E2E_BIN ?? "";
  if (!bin || !existsSync(join(bin, "server"))) {
    bin = mkdtempSync(join(tmpdir(), "memax-e2e-bin-"));
    execFileSync(
      "go",
      [
        "build",
        "-p",
        "2",
        "-o",
        bin + "/",
        "./cmd/server",
        "./cmd/worker",
        "./cmd/migrate",
        "./cmd/devseed",
      ],
      { cwd: SERVER, stdio: "inherit" },
    );
  }
  // The compile service imports the built compiler.
  for (const pkg of ["compiler", "compile-service"]) {
    execFileSync(
      process.execPath,
      [
        join(REPO, "packages", pkg, "node_modules", "typescript", "bin", "tsc"),
        "-p",
        join(REPO, "packages", pkg),
      ],
      { stdio: "inherit" },
    );
  }

  execFileSync("psql", [
    admin,
    "-v",
    "ON_ERROR_STOP=1",
    "-qc",
    `CREATE DATABASE ${db}`,
  ]);
  const dropDb = () => {
    try {
      execFileSync("psql", [
        admin,
        "-qc",
        `DROP DATABASE IF EXISTS ${db} WITH (FORCE)`,
      ]);
    } catch {
      // best effort
    }
  };
  try {
    const env = {
      ...process.env,
      DATABASE_URL: dbUrl,
      MEMAX_ENV: "dev",
      REDIS_URL: "",
      APP_BASE_URL: "https://memax.app",
    };
    execFileSync(join(bin, "migrate"), {
      env: { ...env, MIGRATIONS_DIR: join(SERVER, "migrations") },
      stdio: ["ignore", "ignore", "inherit"],
    });

    const s3 = await fakeS3();
    const compile = spawn(
      process.execPath,
      [join(REPO, "packages", "compile-service", "dist", "main.js")],
      {
        env: {
          ...process.env,
          PORT: "0",
          HOST: "127.0.0.1",
          SHUTDOWN_GRACE_MS: "1000",
        },
      },
    );
    children.push(compile);
    const port = await waitForLine(
      compile,
      (l) => l.match(/"msg":"listening".*"port":(\d+)/),
      15_000,
      "compile service",
      logs,
    );
    const storage = {
      COMPILE_SERVICE_URL: `http://127.0.0.1:${port[1]}`,
      S3_ENDPOINT: s3.url,
      S3_ACCESS_KEY: "e2e",
      S3_SECRET_KEY: "e2e-secret",
      S3_BUCKET: "memax-e2e",
      S3_REGION: "us-east-1",
    };
    const worker = spawn(join(bin, "worker"), {
      env: { ...env, ...storage, HEALTH_PORT: String(await freePort()) },
    });
    children.push(worker);
    worker.stdout.on("data", (b) =>
      logs.push(`[worker] ${String(b).trimEnd()}`),
    );
    worker.stderr.on("data", (b) =>
      logs.push(`[worker] ${String(b).trimEnd()}`),
    );
    const apiPort = await freePort();
    const server = spawn(join(bin, "server"), {
      env: { ...env, ...storage, PORT: String(apiPort), JWT_SECRET: secret },
    });
    children.push(server);
    server.stdout.on("data", (b) =>
      logs.push(`[server] ${String(b).trimEnd()}`),
    );
    server.stderr.on("data", (b) =>
      logs.push(`[server] ${String(b).trimEnd()}`),
    );
    const apiUrl = `http://127.0.0.1:${apiPort}`;
    await waitHealthy(apiUrl, 60_000);

    const seeded = execFileSync(join(bin, "devseed"), {
      env,
      stdio: ["ignore", "pipe", "pipe"],
    }).toString();
    const zz = seeded.match(
      /Ziyang Zeng \(ziyang@example\.com, owner\) ([0-9a-f-]{36})/,
    )?.[1];
    if (!zz) throw new Error(`devseed didn't name ZZ:\n${seeded}`);
    return {
      apiUrl,
      dbUrl,
      zz,
      token: sessionToken(zz, secret),
      logs,
      async stop() {
        for (const c of children) c.kill("SIGTERM");
        await Promise.all(
          children.map((c) =>
            c.exitCode !== null ? null : new Promise((r) => c.once("exit", r)),
          ),
        );
        s3.server.close();
        dropDb();
      },
    };
  } catch (err) {
    for (const c of children) c.kill("SIGKILL");
    dropDb();
    throw err;
  }
}
