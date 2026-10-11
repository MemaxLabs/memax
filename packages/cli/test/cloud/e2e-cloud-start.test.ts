// A cloud sandbox's start against the real stack (cloud warm-start design
// §12): Postgres with the migrations, devseed's memax-v2, the compile
// service, the worker and the API server. The committed AGENTS.md is the
// space's first compile (C-1); the record changes (a memory kept, one
// edited, one forgotten, a gate asked) and compiles again (C-2), not
// committed; a fresh clone runs the built @memaxlabs/start bundle as a
// sandbox does (a bare environment, a fresh HOME, no ~/.memax, no daemon,
// the agent's event on stdin) and hears exactly that.
//
// The credential is a start key: the person connects Copilot's cloud agent
// to the space (POST /v2/cloud-agents), and the sandbox holds only the
// key's secret.
//
// It needs Go, Postgres (a role that can CREATE DATABASE), psql, git and
// the workspace's node_modules, so it runs only with MEMAX_E2E_SERVER=1:
//
//   MEMAX_E2E_SERVER=1 pnpm --filter memax-cli exec vitest run test/cloud/e2e-cloud-start.test.ts
import { execFileSync, spawn } from "node:child_process";
import { randomUUID } from "node:crypto";
import {
  mkdirSync,
  mkdtempSync,
  realpathSync,
  rmSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { Memax, type V2 } from "memax-sdk";
import { afterAll, beforeAll, describe, expect, it } from "vitest";
import { estimateTokens, MAX_CHARS } from "../../src/lib/hook/render.js";
import { grantToken, startStack, type Stack } from "../daemon/e2e-stack.js";

const enabled = process.env.MEMAX_E2E_SERVER === "1";
if (!enabled) {
  console.log(
    "Skipping the cloud start against the real server: set MEMAX_E2E_SERVER=1 to run it.",
  );
}

const CLI = join(import.meta.dirname, "..", "..");
const START = join(CLI, "..", "start");
const SPACE = "memax-v2";

describe.skipIf(!enabled)(
  "a cloud sandbox's start against the real server",
  () => {
    let stack: Stack;
    let memax: Memax;
    let agent: Memax;
    let base: string;
    let origin: string;
    let clone: string;
    let bundle: string;
    let startKey: string;
    let cloudAgent: V2.AgentConnection;
    let pnpm: V2.Memory;
    let edited: string;
    let forgotten: string;
    let forgottenWords: string;
    let gate: V2.Gate;
    let c1: string;

    const git = (cwd: string, ...args: string[]) =>
      execFileSync(
        "git",
        ["-c", "user.email=e2e@memax.test", "-c", "user.name=e2e", ...args],
        {
          cwd,
          stdio: ["ignore", "pipe", "pipe"],
        },
      ).toString();
    const agentsMd = async (): Promise<{ ref: string; content: string }> => {
      const t = (await memax.v2.targets.list(SPACE)).items.find(
        (x) => x.kind === "agents_md",
      )!;
      const p = await memax.v2.targets.preview(t.id);
      return { ref: p.compile?.ref ?? "", content: p.files[0]?.content ?? "" };
    };
    async function until<T>(
      check: () => Promise<T | undefined | false>,
      ms: number,
      what: string,
    ): Promise<T> {
      const start = Date.now();
      for (;;) {
        const v = await check().catch(() => undefined);
        if (v) return v;
        if (Date.now() - start > ms)
          throw new Error(
            `timed out after ${ms} ms waiting for ${what}\n${stack.logs.slice(-30).join("\n")}`,
          );
        await new Promise((r) => setTimeout(r, 100));
      }
    }

    /** The bundle in the clone, as a sandbox runs it. */
    function startInSandbox(
      env: Record<string, string>,
      session = randomUUID(),
    ) {
      const home = mkdtempSync(join(tmpdir(), "memax-sandbox-home-"));
      const started = performance.now();
      const child = spawn(process.execPath, [bundle, "--agent", "copilot"], {
        cwd: clone,
        env: { HOME: home, PATH: process.env.PATH ?? "/usr/bin:/bin", ...env },
        stdio: ["pipe", "pipe", "pipe"],
      });
      child.stdin!.end(
        JSON.stringify({
          sessionId: session,
          cwd: clone,
          source: "new",
          initialPrompt: "Which package manager?",
        }),
      );
      let stdout = "";
      child.stdout!.on("data", (b) => (stdout += String(b)));
      return new Promise<{ code: number | null; stdout: string; ms: number }>(
        (resolve) =>
          child.on("exit", (code) => {
            rmSync(home, { recursive: true, force: true });
            resolve({ code, stdout, ms: performance.now() - started });
          }),
      );
    }

    beforeAll(async () => {
      base = realpathSync(mkdtempSync(join(tmpdir(), "memax-cloud-e2e-")));
      bundle = join(base, "memax-start.js");
      execFileSync(
        process.execPath,
        [join(START, "scripts", "build.mjs"), "--outfile", bundle],
        { cwd: START, stdio: "inherit" },
      );
      stack = await startStack();
      memax = new Memax({ apiUrl: stack.apiUrl, apiKey: stack.token });
      // Claude Code (devseed's grant) asks the gate, as an agent.
      const grant = execFileSync("psql", [
        stack.dbUrl,
        "-Atc",
        "SELECT id FROM oauth_grants WHERE agent_name = 'claude-code'",
      ])
        .toString()
        .trim();
      agent = new Memax({
        apiUrl: stack.apiUrl,
        apiKey: grantToken(stack.zz, "claude-code", grant, stack.secret),
      });
      // The person connects Copilot's cloud agent and gets its start key.
      const made = await memax.v2.agents.connectCloud(
        { agent: "copilot", space: SPACE },
        { idempotencyKey: randomUUID(), via: "cli" },
      );
      startKey = made.key!.secret;
      cloudAgent = made.agent;

      // 1. The first compile, committed.
      const first = await until(
        async () => {
          const a = await agentsMd();
          return a.ref && a.content ? a : undefined;
        },
        60_000,
        "the first compile of AGENTS.md",
      );
      c1 = first.ref;
      origin = join(base, "origin");
      mkdirSync(origin);
      git(origin, "init", "-q", "-b", "main");
      writeFileSync(join(origin, "AGENTS.md"), first.content);
      git(origin, "add", "AGENTS.md");
      git(origin, "commit", "-q", "-m", `AGENTS.md at ${c1}`);
    }, 600_000);

    afterAll(async () => {
      await stack?.stop();
      if (base) rmSync(base, { recursive: true, force: true });
    }, 60_000);

    it("hears what changed since the committed compile, and counts the load", async () => {
      // 2. The record changes: a memory kept, one edited, one forgotten, a gate asked.
      pnpm = (
        await memax.v2.memories.remember(
          SPACE,
          {
            statement: "Use pnpm, not npm, in packages/web.",
            section: "conventions",
          },
          { idempotencyKey: randomUUID() },
        )
      ).memory!;
      const first = await agentsMd();
      const cited = [...first.content.matchAll(/^- (.+) \[(M-\d+)\]$/gm)].map(
        (m) => ({ text: m[1], ref: m[2] }),
      );
      expect(cited.length).toBeGreaterThan(2);
      edited = cited[0].ref;
      const m = await memax.v2.memories.get(edited, { space: SPACE });
      await memax.v2.memories.edit(
        edited,
        { statement: `${m.memory.statement} Staging first, always.` },
        {
          space: SPACE,
          ifMatch: m.memory.version,
          idempotencyKey: randomUUID(),
        },
      );
      for (const c of cited.slice(1).reverse()) {
        const preview = await memax.v2.memories.previewForget(c.ref, {
          space: SPACE,
        });
        if (!preview.allowed || preview.carries.length > 0) continue;
        await memax.v2.memories.forget(
          c.ref,
          { carries: [] },
          {
            space: SPACE,
            ifMatch: preview.version,
            idempotencyKey: randomUUID(),
          },
        );
        forgotten = c.ref;
        forgottenWords = c.text;
        break;
      }
      expect(forgotten).toBeTruthy();
      gate = (
        await agent.v2.gates.request(
          SPACE,
          {
            question: "Move background jobs to River Pro?",
            options: [{ label: "Yes" }, { label: "Not yet" }],
          },
          { idempotencyKey: randomUUID() },
        )
      ).gate;
      // 3. The second compile, not committed.
      const second = await until(
        async () => {
          const a = await agentsMd();
          return a.content.includes(pnpm.ref) &&
            !a.content.includes(`[${forgotten}]`) &&
            a.ref !== c1
            ? a
            : undefined;
        },
        60_000,
        "the second compile",
      );

      // 4. A fresh clone, and the bundle as the sandbox runs it.
      clone = join(base, "clone");
      git(base, "clone", "-q", origin, clone);
      const r = await startInSandbox({
        MEMAX_API_URL: stack.apiUrl,
        MEMAX_START_KEY: startKey,
      });
      expect(r.code).toBe(0);
      const block = (JSON.parse(r.stdout) as { additionalContext: string })
        .additionalContext;
      console.log(
        `the cloud start on the real stack: ${Math.round(r.ms)} ms\n${block}`,
      );
      expect(block).toContain(
        `AGENTS.md here is ${c1}; the space has compiled ${second.ref} since.`,
      );
      expect(block).toContain(
        `- Use pnpm, not npm, in packages/web. [${pnpm.ref}]`,
      );
      expect(block).toMatch(
        new RegExp(`Changed:\\n- .*Staging first, always\\. \\[${edited}\\]`),
      );
      expect(block).toMatch(
        new RegExp(
          `- ${forgotten} \\(\\d{4}-\\d{2}-\\d{2}\\), still in AGENTS.md here`,
        ),
      );
      expect(block).not.toContain(forgottenWords);
      expect(block).toContain(
        `${gate.ref} "Move background jobs to River Pro?" (asked by`,
      );
      expect(block.length).toBeLessThanOrEqual(MAX_CHARS);
      expect(estimateTokens(block)).toBeLessThanOrEqual(3000);
      expect(r.ms).toBeLessThan(1_000);

      // 5. The reads: a compile load of C-1 by the connection, and a digest
      // read of the lines it printed.
      const reads = await until(
        async () => {
          const page = await memax.v2.reads.list(SPACE);
          const load = page.items.find(
            (x) =>
              x.kind === "compile_load" &&
              x.compile === c1 &&
              x.connection_id === cloudAgent.id,
          );
          const digest = page.items.find(
            (x) => x.kind === "digest" && x.connection_id === cloudAgent.id,
          );
          return load && digest ? { load, digest } : undefined;
        },
        10_000,
        "the start's reads",
      );
      expect(reads.load.agent).toBe("copilot");
      expect(cloudAgent.surface).toBe("cloud");
      // The start key reaches nothing else.
      const other = await fetch(`${stack.apiUrl}/v2/spaces/${SPACE}/memories`, {
        headers: { Authorization: `Bearer ${startKey}` },
      });
      expect(other.status).toBe(403);
      expect(reads.load.via).toBe("cli");
      expect(reads.digest.memory_refs).toContain(pnpm.ref);
    }, 180_000);

    it("says nothing once the clone holds the latest compile, against a black hole, or once disconnected", async () => {
      await agent.v2.gates.withdraw(
        gate.ref,
        {},
        { space: SPACE, ifMatch: gate.version, idempotencyKey: randomUUID() },
      );
      const latest = await agentsMd();
      writeFileSync(join(clone, "AGENTS.md"), latest.content);
      git(clone, "commit", "-q", "-am", `AGENTS.md at ${latest.ref}`);
      const env = { MEMAX_API_URL: stack.apiUrl, MEMAX_START_KEY: startKey };
      expect(await startInSandbox(env)).toMatchObject({ code: 0, stdout: "" });

      git(clone, "checkout", "-q", "HEAD~1");
      const hole = await startInSandbox({
        ...env,
        MEMAX_API_URL: "http://10.255.255.1:81",
      });
      expect(hole).toMatchObject({ code: 0, stdout: "" });
      expect(hole.ms).toBeLessThan(3_500);

      // A new key (the clone holds C-1 again): the old key stops at once,
      // the new one starts under the same connection.
      const rekeyed = await memax.v2.agents.rekey(
        cloudAgent.id,
        {},
        { idempotencyKey: randomUUID() },
      );
      expect(rekeyed.agent.id).toBe(cloudAgent.id);
      const fresh = { ...env, MEMAX_START_KEY: rekeyed.key!.secret };
      expect(await startInSandbox(env)).toMatchObject({ code: 0, stdout: "" });
      expect((await startInSandbox(fresh)).stdout).toContain("[");

      await memax.v2.agents.disconnect(
        cloudAgent.id,
        {},
        { idempotencyKey: randomUUID() },
      );
      expect(await startInSandbox(fresh)).toMatchObject({
        code: 0,
        stdout: "",
      });
    }, 120_000);
  },
);
