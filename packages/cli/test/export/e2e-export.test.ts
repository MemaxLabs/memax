// Export end to end against the real stack (rule 14): the seeded memax-v2
// space, a Forget, the worker's sealer signing checkpoints, then `memax
// export` writes the archive the server streams and `memax verify-export`
// checks it against the server's published key; a changed receipt fails
// it. Needs what test/daemon/e2e-server.test.ts needs, so it runs only
// with MEMAX_E2E_SERVER=1:
//
//   MEMAX_E2E_SERVER=1 pnpm --filter memax-cli exec vitest run test/export/e2e-export.test.ts
import { execFileSync } from "node:child_process";
import { createHash } from "node:crypto";
import {
  mkdirSync,
  mkdtempSync,
  readFileSync,
  realpathSync,
  rmSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { Memax } from "memax-sdk";
import { afterAll, beforeAll, describe, expect, it } from "vitest";
import { startStack, type Stack } from "../daemon/e2e-stack.js";

const enabled = process.env.MEMAX_E2E_SERVER === "1";
const CLI = join(import.meta.dirname, "..", "..");
const BIN = join(CLI, "dist", "bin.js");
// The worker signs checkpoints with it; the API publishes its public half.
const SIGNING_KEY = "11".repeat(32);

describe.skipIf(!enabled)("memax export against the real server", () => {
  let stack: Stack;
  let memax: Memax;
  let base: string;
  let home: string;
  const env = () => ({
    ...process.env,
    HOME: home,
    MEMAX_API_URL: stack.apiUrl,
    MEMAX_API_KEY: "",
    XDG_CONFIG_HOME: "",
  });
  /** Runs the built CLI; returns its output and exit code. */
  const cli = (...args: string[]) => {
    try {
      const out = execFileSync(process.execPath, [BIN, ...args], {
        cwd: base,
        env: env(),
      }).toString();
      return { code: 0, out };
    } catch (err) {
      const e = err as { status: number; stdout: Buffer; stderr: Buffer };
      return { code: e.status, out: `${e.stdout}${e.stderr}` };
    }
  };

  beforeAll(async () => {
    execFileSync(
      process.execPath,
      [join(CLI, "node_modules", "typescript", "bin", "tsc"), "-p", CLI],
      { stdio: "inherit" },
    );
    stack = await startStack({
      env: { RECEIPT_SIGNING_KEY: SIGNING_KEY, SEALER_INTERVAL: "10s" },
    });
    memax = new Memax({ apiUrl: stack.apiUrl, apiKey: stack.token });
    base = realpathSync(mkdtempSync(join(tmpdir(), "memax-export-e2e-")));
    home = join(base, "home");
    mkdirSync(join(home, ".memax"), { recursive: true });
    writeFileSync(
      join(home, ".memax", "credentials.json"),
      JSON.stringify({
        access_token: stack.token,
        refresh_token: "",
        expires_at: Date.now() + 7_200_000,
      }),
      { mode: 0o600 },
    );
  }, 600_000);

  afterAll(async () => {
    await stack?.stop();
    if (base) rmSync(base, { recursive: true, force: true });
  }, 60_000);

  it("exports, verifies against the server's key, and catches a change", async () => {
    // Forget one memory, so the export holds a tombstone.
    const page = await memax.v2.memories.list("memax-v2", {
      state: "kept",
      limit: 50,
    });
    const victim = page.items.find((m) => m.kind === "fact")!;
    const preview = await memax.v2.memories.previewForget(victim.ref, {
      space: "memax-v2",
    });
    await memax.v2.memories.forget(
      victim.ref,
      { carries: preview.carries.map((c) => c.ref) },
      {
        space: "memax-v2",
        ifMatch: preview.version,
        idempotencyKey: "e2e-forget",
      },
    );
    // The sealer signs a checkpoint over it within its interval.
    const start = Date.now();
    for (;;) {
      const cps = await memax.v2.receipts.checkpoints("memax-v2", {
        limit: 1,
      });
      if (cps.seal.unsealed === 0 && cps.seal.sealed_receipts > 0) break;
      if (Date.now() - start > 90_000)
        throw new Error(
          `nothing was sealed\n${stack.logs.slice(-30).join("\n")}`,
        );
      await new Promise((r) => setTimeout(r, 250));
    }

    const exported = cli("export", "--space", "memax-v2", "--out", "out");
    expect(exported.code).toBe(0);
    expect(exported.out).toContain("Exported memax-v2");
    const dir = join(base, "out", "memax-v2");
    const tomb = readFileSync(
      join(dir, "tombstones", `${victim.ref}.md`),
      "utf8",
    );
    expect(tomb).toContain(`${victim.ref} was forgotten on`);
    for (const f of ["receipts.jsonl", "README.md", "decisions.md"]) {
      expect(readFileSync(join(dir, f), "utf8")).not.toContain(
        victim.statement,
      );
    }

    const verified = cli("verify-export", dir);
    expect(verified.code).toBe(0);
    expect(verified.out).toContain("Verified memax-v2");
    expect(verified.out).toMatch(
      new RegExp(
        `every signature verifies \\(ed25519:[0-9a-f]{16}, from ${stack.apiUrl.replace(/\./g, "\\.")}\\)`,
      ),
    );
    // Its own receipt (at least) was written after the last seal.
    expect(verified.out).toMatch(/\d+ receipts? came after the last seal/);

    // A sealed receipt changed, export.json rewritten to match: the chain
    // catches it.
    const receipts = join(dir, "receipts.jsonl");
    const lines = readFileSync(receipts, "utf8").split("\n");
    lines[0] = lines[0].replace(/"via":"[a-z]+"/, '"via":"github"');
    writeFileSync(receipts, lines.join("\n"));
    const m = JSON.parse(readFileSync(join(dir, "export.json"), "utf8"));
    const entry = m.files.find(
      (f: { path: string }) => f.path === "receipts.jsonl",
    );
    const data = readFileSync(receipts);
    entry.sha256 = createHash("sha256").update(data).digest("hex");
    entry.bytes = data.length;
    writeFileSync(join(dir, "export.json"), JSON.stringify(m));
    const tampered = cli("verify-export", dir);
    expect(tampered.code).toBe(1);
    expect(tampered.out).toMatch(
      /receipts\.jsonl: checkpoint 1 \(receipts 1–\d+, lines? 1(–\d+)? of receipts\.jsonl\): the Merkle root of its receipts differs from the signed one/,
    );
  }, 240_000);
});
