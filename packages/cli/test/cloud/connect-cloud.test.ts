// memax connect --cloud <agent> against the fake /v2 server, in a
// temporary git repository: the connection and its start key, the
// committed hook file (merged, pinned, guarded, never with the key), the
// steps per agent, a new key only when asked, and Codex's committed files.
import { execFileSync } from "node:child_process";
import { randomBytes, randomUUID } from "node:crypto";
import {
  existsSync,
  mkdirSync,
  readdirSync,
  readFileSync,
  statSync,
  writeFileSync,
} from "node:fs";
import { join } from "node:path";
import type { V2 } from "memax-sdk";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import {
  CloudConnectError,
  cloudHook,
  runConnectCloud,
  START_PACKAGE,
  type CloudConnectDeps,
} from "../../src/lib/connect/cloud.js";
import { cloudFailed, renderCloud } from "../../src/commands/connect.js";
import type { RouteResult } from "../daemon/fake-v2.js";
import { harness, type Harness } from "../daemon/harness.js";

let h: Harness;
let space: V2.Space;
let made: Array<{ path: string; body: unknown }>;

const now = () => new Date().toISOString();
const secret = () => `mxk_${randomBytes(32).toString("hex")}`;

function startKey(): V2.StartKey {
  const s = secret();
  return {
    secret: s,
    prefix: s.slice(0, 8),
    expires_at: new Date(Date.now() + 180 * 86_400_000).toISOString(),
  };
}

function cloudConnection(
  agent: V2.AgentKind,
  sp: V2.Space,
): V2.AgentConnection {
  return {
    id: randomUUID(),
    person_id: "p-1",
    agent,
    display_name: `${agent} cloud agent · ${sp.slug}`,
    surface: "cloud",
    credential: { kind: "api_key", id: randomUUID(), active: true },
    max_autonomy: "read",
    state: "active",
    spaces: [
      {
        space_id: sp.id,
        slug: sp.slug,
        name: sp.name,
        kind: sp.kind,
        autonomy: "read",
        reads_7d: 0,
        writes_7d: 0,
        updated_at: now(),
      },
    ],
    reads_7d: 0,
    writes_7d: 0,
    created_receipt_id: randomUUID(),
    last_receipt_id: randomUUID(),
    created_at: now(),
    updated_at: now(),
  } as V2.AgentConnection;
}

/** POST /v2/cloud-agents and :rekey, as the server answers them. */
function installCloud() {
  h.fake.extra.push((method, path, _url, body): RouteResult | null => {
    if (method === "GET" && path === "/v2/agents")
      return { status: 200, data: { items: h.fake.agents } };
    if (method === "POST" && path === "/v2/cloud-agents") {
      const b = body as V2.CreateCloudAgentInput;
      const sp = h.fake.spaces.find((s) => s.slug === b.space);
      if (!sp) return null;
      if (sp.kind === "personal")
        return { status: 400, error: "invalid_request", message: "personal" };
      const c = cloudConnection(b.agent as V2.AgentKind, sp);
      h.fake.agents.push(c);
      made.push({ path, body });
      return {
        status: 201,
        data: {
          outcome: "applied",
          policy: { effect: "apply" },
          agent: c,
          key: startKey(),
          receipts: [],
        },
      };
    }
    const m = path.match(/^\/v2\/agents\/([^/:]+):rekey$/);
    if (method === "POST" && m) {
      const c = h.fake.agents.find((a) => a.id === m[1]);
      if (!c) return null;
      made.push({ path, body });
      return {
        status: 200,
        data: {
          outcome: "applied",
          policy: { effect: "apply" },
          agent: c,
          key: startKey(),
          receipts: [],
        },
      };
    }
    return null;
  });
}

beforeEach(async () => {
  h = await harness();
  made = [];
  installCloud();
  space = h.fake.addSpace("acme-web", "acme/web");
  execFileSync("git", ["init", "-q", h.repo]);
});
afterEach(() => h.cleanup());

const deps = (more: Partial<CloudConnectDeps> = {}): CloudConnectDeps => ({
  memax: h.memax,
  paths: h.paths,
  cwd: h.repo,
  apiUrl: "https://api.memax.app",
  ...more,
});
const read = (p: string) => readFileSync(join(h.repo, p), "utf8");

/** Every file under a folder, for the grep: the key is in none of them. */
function filesUnder(dir: string): string[] {
  if (!existsSync(dir)) return [];
  return readdirSync(dir).flatMap((name) => {
    const p = join(dir, name);
    return statSync(p).isDirectory() ? filesUnder(p) : [p];
  });
}
function keyIsInNoFile(key: string) {
  for (const f of [...filesUnder(h.repo), ...filesUnder(h.home)].filter(
    (f) => !f.includes(`${join(".git", "objects")}`),
  ))
    expect(readFileSync(f, "utf8"), f).not.toContain(key);
}

describe("memax connect --cloud", () => {
  it("pins the start package this repository builds", () => {
    const pkg = JSON.parse(
      readFileSync(
        join(import.meta.dirname, "..", "..", "..", "start", "package.json"),
        "utf8",
      ),
    ) as { name: string; version: string };
    expect(START_PACKAGE).toBe(`${pkg.name}@${pkg.version}`);
  });

  it("connects Copilot's cloud agent, prints the key once and commits a hook that names it", async () => {
    const r = await runConnectCloud("copilot", { space: "acme-web" }, deps());
    expect(r.connection).toMatchObject({
      state: "connected",
      autonomy: "read",
    });
    expect(r.key?.secret).toMatch(/^mxk_[0-9a-f]{64}$/);
    expect(made).toEqual([
      {
        path: "/v2/cloud-agents",
        body: { agent: "copilot", space: "acme-web" },
      },
    ]);
    expect(
      h.fake.calls("POST", /cloud-agents/)[0]!.headers["x-memax-via"],
    ).toBe("cli");
    expect(JSON.parse(read(".github/hooks/memax.json"))).toEqual({
      version: 1,
      hooks: {
        sessionStart: [
          {
            type: "command",
            bash: `[ -n "$MEMAX_START_KEY" ] && npx -y ${START_PACKAGE} --agent copilot || true`,
            timeoutSec: 20,
          },
        ],
      },
    });
    expect(r.hook).toEqual({
      path: ".github/hooks/memax.json",
      outcome: "written",
    });
    keyIsInNoFile(r.key!.secret);

    const text = renderCloud(r).join("\n");
    expect(text.split(r.key!.secret)).toHaveLength(2);
    expect(text).toContain("MEMAX_START_KEY");
    expect(text).toContain("api.memax.app");
    expect(text).toContain("CODEOWNERS entry for .github/hooks/memax.json");
    expect(cloudFailed(r)).toBe(false);
  });

  it("leaves a connected agent's key alone unless asked, then gives it a new one", async () => {
    const first = await runConnectCloud(
      "copilot",
      { space: "acme-web" },
      deps(),
    );
    const file = read(".github/hooks/memax.json");

    // No terminal, no --yes: nothing new, and it says how.
    const again = await runConnectCloud(
      "copilot",
      { space: "acme-web" },
      deps(),
    );
    expect(again.connection).toMatchObject({
      id: first.connection!.id,
      state: "already",
    });
    expect(again.key).toBeNull();
    expect(again.hook?.outcome).toBe("present");
    expect(again.note).toContain("--yes");
    expect(read(".github/hooks/memax.json")).toBe(file);
    expect(renderCloud(again).join("\n")).not.toContain("mxk_");

    // A person who says no keeps it; one who says yes gets a new key.
    const asked: string[] = [];
    const no = await runConnectCloud(
      "copilot",
      { space: "acme-web" },
      deps({ confirm: async (q) => (asked.push(q), false) }),
    );
    expect(no.key).toBeNull();
    expect(asked[0]).toContain("The old one stops working at once");
    const yes = await runConnectCloud(
      "copilot",
      { space: "acme-web" },
      deps({ confirm: async () => true }),
    );
    expect(yes.connection).toMatchObject({
      id: first.connection!.id,
      state: "rekeyed",
    });
    expect(yes.key?.secret).not.toBe(first.key?.secret);
    // --yes needs no terminal.
    const forced = await runConnectCloud(
      "copilot",
      { space: "acme-web", yes: true },
      deps(),
    );
    expect(forced.connection?.state).toBe("rekeyed");
    expect(made.map((m) => m.path)).toEqual([
      "/v2/cloud-agents",
      `/v2/agents/${first.connection!.id}:rekey`,
      `/v2/agents/${first.connection!.id}:rekey`,
    ]);
    keyIsInNoFile(yes.key!.secret);
  });

  it("merges Claude Code's hook into the repository's settings, guarded and pinned", async () => {
    mkdirSync(join(h.repo, ".claude"), { recursive: true });
    writeFileSync(
      join(h.repo, ".claude", "settings.json"),
      JSON.stringify({
        permissions: { allow: ["Bash(pnpm test)"] },
        hooks: {
          SessionStart: [
            {
              matcher: "startup",
              hooks: [
                { type: "command", command: "echo theirs" },
                {
                  type: "command",
                  command:
                    "npx -y @memaxlabs/start@0.9.0 --agent claude-code || true",
                },
              ],
            },
          ],
          Stop: [{ hooks: [{ type: "command", command: "echo stop" }] }],
        },
      }),
    );
    const r = await runConnectCloud(
      "claude-code",
      { space: "acme-web" },
      deps({ apiUrl: "https://staging-api.memax.app" }),
    );
    const s = JSON.parse(read(".claude/settings.json"));
    expect(s.permissions).toEqual({ allow: ["Bash(pnpm test)"] });
    expect(s.hooks.Stop).toHaveLength(1);
    // Their hook stays; the older pin is replaced, not doubled.
    expect(s.hooks.SessionStart).toEqual([
      {
        matcher: "startup",
        hooks: [{ type: "command", command: "echo theirs" }],
      },
      cloudHook("claude-code")!.entry,
    ]);
    expect(JSON.stringify(s)).toContain('[ \\"$CLAUDE_CODE_REMOTE\\" = true ]');
    expect(JSON.stringify(s)).not.toContain("0.9.0");
    // A server that isn't production's: the sandbox needs its address.
    expect(r.steps.join(" ")).toContain(
      "MEMAX_API_URL=https://staging-api.memax.app",
    );
    expect(r.steps.join(" ")).toContain(
      "network secret for staging-api.memax.app",
    );
    keyIsInNoFile(r.key!.secret);
  });

  it("prints the hook file instead with --no-hook", async () => {
    const r = await runConnectCloud(
      "claude-code",
      { space: "acme-web", hook: false },
      deps(),
    );
    expect(r.hook?.outcome).toBe("printed");
    expect(JSON.parse(r.hook!.content!)).toEqual({
      hooks: { SessionStart: [cloudHook("claude-code")!.entry] },
    });
    expect(filesUnder(h.repo).some((f) => f.includes(".claude"))).toBe(false);
    expect(r.hook!.content).not.toContain("mxk_");
  });

  it("says CODEOWNERS covers the hook when it does", async () => {
    mkdirSync(join(h.repo, ".github"), { recursive: true });
    writeFileSync(
      join(h.repo, ".github", "CODEOWNERS"),
      "/.github/hooks/ @acme/leads\n",
    );
    const r = await runConnectCloud("copilot", { space: "acme-web" }, deps());
    expect(r.owned).toBe(true);
    expect(renderCloud(r).join("\n")).not.toContain("CODEOWNERS entry");
  });

  it("gives Codex no key: it starts from the committed AGENTS.md", async () => {
    const r = await runConnectCloud("codex", { space: "acme-web" }, deps());
    expect(r.connection).toBeNull();
    expect(r.key).toBeNull();
    expect(r.hook).toBeNull();
    expect(r.steps.join(" ")).toContain("committed AGENTS.md");
    expect(made).toEqual([]);
  });

  it("refuses a Personal space before asking the server", async () => {
    h.fake.addPersonalSpace();
    await expect(
      runConnectCloud("copilot", { space: "personal" }, deps()),
    ).rejects.toBeInstanceOf(CloudConnectError);
    expect(made).toEqual([]);
  });
});
