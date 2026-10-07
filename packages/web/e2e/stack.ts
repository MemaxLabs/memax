// The real API for the web session's end-to-end test (auth-bff.e2e.ts): a
// fresh Postgres database with the migrations and the dev seeder's
// memax-v2, and the Go API server on a fixed port (the web app is built
// against it: NEXT_PUBLIC_API_URL is inlined at build time). No worker and
// no compile service: a Keep's jobs are queued, not run.
//
// It needs Go, psql and a Postgres role that can CREATE DATABASE
// (E2E_DATABASE_URL or TEST_DATABASE_URL). MEMAX_E2E_BIN may point at a
// directory of prebuilt server, migrate and devseed binaries.
import { execFileSync, spawn, type ChildProcess } from "node:child_process";
import { randomBytes } from "node:crypto";
import { existsSync, mkdtempSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

// Playwright loads e2e/ as CommonJS (as e2e/handoff.ts does).
const SERVER = join(__dirname, "..", "..", "server");

/** The port the API listens on; the web build points at it. */
export const STACK_API_PORT = Number(process.env.E2E_API_PORT ?? 18080);
export const STACK_API_URL = `http://127.0.0.1:${STACK_API_PORT}`;
/** WEB_SURFACE_SECRET, shared by the API and the web app under test. */
export const STACK_SURFACE_SECRET =
  process.env.E2E_SURFACE_SECRET ?? "e2e-web-surface-secret-0123456789abcdef";

export interface Stack {
  apiUrl: string;
  /** The demo's owner, Ziyang Zeng. */
  zz: string;
  /** JWT_SECRET, to sign tokens a test needs (a CLI login). */
  jwtSecret: string;
  /** One value from the database. */
  query(sql: string): string;
  /** A one-time sign-in code for ZZ (or `user`), delivered to the web app. */
  webCode(user?: string): string;
  logs: string[];
  stop(): Promise<void>;
}

async function waitHealthy(url: string, ms: number, logs: string[]) {
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
  throw new Error(
    `the API at ${url} wasn't ready within ${ms} ms:\n${logs.slice(-30).join("\n")}`,
  );
}

export async function startStack(): Promise<Stack> {
  const admin =
    process.env.E2E_DATABASE_URL ??
    process.env.TEST_DATABASE_URL ??
    "postgres://memax:memax@localhost:5432/memax?sslmode=disable";
  const db = `memax_web_e2e_${randomBytes(4).toString("hex")}`;
  const u = new URL(admin);
  u.pathname = `/${db}`;
  const dbUrl = u.toString();
  const jwtSecret = randomBytes(24).toString("hex");
  const logs: string[] = [];

  let bin = process.env.MEMAX_E2E_BIN ?? "";
  if (!bin || !existsSync(join(bin, "server"))) {
    bin = mkdtempSync(join(tmpdir(), "memax-web-e2e-bin-"));
    execFileSync(
      "go",
      [
        "build",
        "-o",
        bin + "/",
        "./cmd/server",
        "./cmd/migrate",
        "./cmd/devseed",
      ],
      { cwd: SERVER, stdio: "inherit" },
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
  let server: ChildProcess | undefined;
  try {
    const env = {
      ...process.env,
      DATABASE_URL: dbUrl,
      MEMAX_ENV: "dev",
      REDIS_URL: "",
      COMPILE_SERVICE_URL: "",
      APP_BASE_URL: process.env.E2E_APP_URL ?? "http://localhost:3100",
      JWT_SECRET: jwtSecret,
      WEB_SURFACE_SECRET: STACK_SURFACE_SECRET,
    };
    execFileSync(join(bin, "migrate"), {
      env: { ...env, MIGRATIONS_DIR: join(SERVER, "migrations") },
      stdio: ["ignore", "ignore", "inherit"],
    });
    const seeded = execFileSync(join(bin, "devseed"), {
      env,
      stdio: ["ignore", "pipe", "pipe"],
    }).toString();
    const zz = seeded.match(
      /Ziyang Zeng \(ziyang@example\.com, owner\) ([0-9a-f-]{36})/,
    )?.[1];
    if (!zz) throw new Error(`devseed didn't name ZZ:\n${seeded}`);

    server = spawn(join(bin, "server"), {
      env: { ...env, PORT: String(STACK_API_PORT) },
    });
    server.stdout?.on("data", (b) => logs.push(`[api] ${String(b).trimEnd()}`));
    server.stderr?.on("data", (b) => logs.push(`[api] ${String(b).trimEnd()}`));
    await waitHealthy(STACK_API_URL, 60_000, logs);

    const query = (sql: string) =>
      execFileSync("psql", [dbUrl, "-v", "ON_ERROR_STOP=1", "-tAc", sql])
        .toString()
        .trim();
    return {
      apiUrl: STACK_API_URL,
      zz,
      jwtSecret,
      query,
      logs,
      webCode(user = zz) {
        const code = randomBytes(32).toString("hex");
        query(
          `INSERT INTO auth_codes (code, user_id, expires_at, surface) VALUES ('${code}', '${user}', now() + interval '1 minute', 'web')`,
        );
        return code;
      },
      async stop() {
        if (server && server.exitCode === null) {
          server.kill("SIGTERM");
          await new Promise((r) => server!.once("exit", r));
        }
        dropDb();
      },
    };
  } catch (err) {
    server?.kill("SIGKILL");
    dropDb();
    throw err;
  }
}
