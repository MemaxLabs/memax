// The registry against the API's contract and against itself: every agent
// the API's AgentKind names is here and nothing else is; every file kind
// is a TargetKind; and what each agent's hook and home file say it loads
// agrees with its row of the load matrix.
import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";
import {
  AGENTS,
  AGENT_IDS,
  HOME_CANONICAL,
  READERS,
  USER_FILES,
  agentOf,
  capOf,
  readerOf,
  type FileKind,
  type UserFile,
} from "../src/index.js";

const spec = readFileSync(
  join(dirname(fileURLToPath(import.meta.url)), "../../server/openapi/v2.yaml"),
  "utf8",
);

/** A string enum of the spec's components, by its schema name. */
function specEnum(name: string): string[] {
  const at = spec.indexOf(`\n    ${name}:\n`);
  expect(at, `${name} in v2.yaml`).toBeGreaterThan(0);
  const rest = spec.slice(at + name.length + 7);
  const next = rest.search(/\n {4}\S/);
  const m = /\n\s+enum: \[([^\]]*)\]/.exec(
    next < 0 ? rest : rest.slice(0, next),
  );
  expect(m, `${name}'s enum`).not.toBeNull();
  return m![1].split(",").map((s) => s.trim());
}

/** The matrix's user file for each home kind. */
const HOME_FILE: Partial<Record<FileKind, UserFile>> = {
  claude_user: "claude_rules_file",
  codex_user: "codex_agents_md",
  gemini_user: "gemini_agents_md",
  copilot_user: "copilot_instructions",
  windsurf_user: "windsurf_global_rules",
};

describe("the registry and the API", () => {
  it("has every agent the API's AgentKind names, and only those", () => {
    const kinds = specEnum("AgentKind").filter((k) => k !== "other");
    expect([...AGENT_IDS].sort()).toEqual([...kinds].sort());
  });

  it("names only file kinds the API's TargetKind has", () => {
    const kinds = new Set(specEnum("TargetKind"));
    for (const a of AGENTS) {
      for (const k of [
        ...(a.hook?.loads ?? []),
        ...(a.hook?.prints ?? []),
        ...(a.home ? [a.home.kind] : []),
      ])
        expect(kinds.has(k), `${a.id}: ${k}`).toBe(true);
    }
  });

  it("gives each agent its own id, key and mark", () => {
    for (const field of ["id", "key", "mark", "name"] as const) {
      const values = AGENTS.map((a) => a[field]);
      expect(new Set(values).size, field).toBe(values.length);
    }
    expect(agentOf("gemini")?.id).toBe("gemini-cli");
    expect(agentOf("gemini-cli")?.key).toBe("gemini");
    expect(agentOf("aider")).toBeUndefined();
  });
});

describe("the registry and the load matrix", () => {
  const row = (id: string) => READERS.find((r) => r.agent === id);

  it("has a row for every agent installed on a machine, and every row says how it knows", () => {
    for (const a of AGENTS)
      if (!a.connector) expect(row(a.id), a.id).toBeDefined();
    const ids = READERS.map((r) => r.id);
    expect(new Set(ids).size).toBe(ids.length);
    for (const r of READERS) {
      expect(r.method.length, r.id).toBeGreaterThan(0);
      expect(r.sources.length, r.id).toBeGreaterThan(0);
      if (r.agent) expect(AGENT_IDS).toContain(r.agent);
    }
  });

  it("agrees with what each hook says its agent loads at session start", () => {
    for (const a of AGENTS) {
      if (!a.hook) continue;
      const r = row(a.id)!;
      for (const k of a.hook.loads) {
        const cell = r.repo[k as keyof typeof r.repo];
        expect(cell?.loads, `${a.id} loads ${k}`).toMatch(
          /^(yes|import|text|when)$/,
        );
      }
      // A printed file is one the agent loads no file of: only the hook says it.
      for (const k of a.hook.prints) {
        expect(k).toBe("agents_md");
        expect(r.user.canonical?.loads, `${a.id} prints ${k}`).toBe("hook");
      }
    }
  });

  it("agrees with the home file each agent has, at its default path", () => {
    for (const a of AGENTS) {
      if (!a.home) continue;
      const file = HOME_FILE[a.home.kind]!;
      expect(USER_FILES[file], a.id).toBe(`~/${a.home.path}`);
      expect(row(a.id)!.user[file]?.loads, a.id).toMatch(/^(yes|when)$/);
    }
    expect(USER_FILES.canonical).toBe(`~/${HOME_CANONICAL}`);
  });

  it("keeps the limits the compiler holds files to", () => {
    expect(capOf("windsurf", { user: "windsurf_global_rules" })).toEqual({
      chars: 6000,
    });
    expect(capOf("antigravity", { user: "gemini_agents_md" })).toEqual({
      bytes: 24000,
    });
    expect(capOf("codex", { repo: "agents_md" })).toEqual({ bytes: 32768 });
    expect(() => capOf("junie", { repo: "agents_md" })).toThrow(/no cap/);
    expect(readerOf("antigravity")).toMatchObject({ mark: "AG" });
  });
});
