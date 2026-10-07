import { MemaxError } from "memax-sdk";
import { describe, expect, it } from "vitest";
import { en } from "@/i18n/locales/en";
import { zh } from "@/i18n/locales/zh";
import { consentFootnote, spaceMetaLine } from "../consent-copy";
import {
  cleanClientName,
  compiledFor,
  ConsentLoadError,
  consentEnding,
  shortName,
  toConsentRequest,
  type ConsentSpaceView,
} from "./consent";
import { demoConsentRequest } from "./consent-demo";

const targets = [
  { kind: "agents_md", path: "AGENTS.md" },
  { kind: "claude_md", path: "CLAUDE.md" },
  { kind: "cursor_mdc", path: ".cursor/rules/memax.mdc" },
  { kind: "chatgpt" },
];

describe("the consent request", () => {
  it("names the file each agent reads, else the canonical one", () => {
    expect(compiledFor("codex", targets)).toBe("AGENTS.md");
    expect(compiledFor("claude-code", targets)).toBe("CLAUDE.md");
    expect(compiledFor("cursor", targets)).toBe(".cursor/rules/memax.mdc");
    expect(compiledFor("chatgpt", targets)).toBe("AGENTS.md");
    expect(compiledFor("some-agent", targets)).toBe("AGENTS.md");
    expect(compiledFor("codex", [{ kind: "chatgpt" }])).toBeNull();
    expect(compiledFor("codex", [])).toBeNull();
  });

  it("puts spaces on V2 first, then projects, teams and Personal, and sends no more than propose", () => {
    const [project, team, personal] = demoConsentRequest.hubs;
    const view = toConsentRequest({
      ...demoConsentRequest,
      client_name: "Claude",
      agent_name: "claude-ai",
      consent_scope: "memax:write memax:propose memax:read",
      hubs: [
        { ...personal!, on_v2: false, name: "Personal" },
        { ...team!, name: "Zeta team" },
        { ...team!, id: "t2", name: "Alpha team" },
        project!,
      ],
    });
    expect(view.spaces.map((s) => s.name)).toEqual([
      "memax-v2",
      "Alpha team",
      "Zeta team",
      "Personal",
    ]);
    expect(view.permissions).toEqual(["memax:propose", "memax:read"]);
    expect(view.client).toEqual({
      name: "Claude",
      agent: "claude",
      host: null,
    });
    // V1 counts its V1 memories, V2 its kept ones.
    expect(view.spaces[3]!.memories).toBe(0);
    expect(view.spaces[0]!.memories).toBe(214);
  });

  it("keeps abilities it knows and drops ones it doesn't", () => {
    const view = toConsentRequest({
      ...demoConsentRequest,
      hubs: [
        {
          ...demoConsentRequest.hubs[0]!,
          can: ["read_brief", "teleport"],
          cannot: ["forget"],
        },
        { ...demoConsentRequest.hubs[1]!, can: undefined, cannot: undefined },
      ],
    });
    expect(view.spaces[0]!.can).toEqual(["read_brief"]);
    expect(view.spaces[0]!.described).toBe(true);
    // An older server says nothing: the page then claims nothing.
    expect(view.spaces[1]!.described).toBe(false);
  });

  it("shows a client's name as one line of text", () => {
    expect(cleanClientName("  Co‮dex​\n  CLI ")).toBe("Codex CLI");
    expect(shortName("Codex")).toBe("Codex");
    expect(shortName("x".repeat(40), 10)).toBe(`${"x".repeat(9)}…`);
    expect(shortName("Acme Research Assistant for Very Long", 20)).toBe(
      "Acme Research…",
    );
    // Characters, not UTF-16 units.
    expect(shortName("😀".repeat(12), 10)).toBe(`${"😀".repeat(9)}…`);
  });

  it("reads the API's refusals as endings", () => {
    expect(
      consentEnding(new MemaxError("", "consent_request_expired", 410)),
    ).toBe("expired");
    expect(
      consentEnding(new MemaxError("", "consent_request_not_found", 404)),
    ).toBe("gone");
    expect(
      consentEnding(new MemaxError("", "invalid_consent_token", 403)),
    ).toBe("gone");
    expect(
      consentEnding(new MemaxError("", "missing_consent_request", 400)),
    ).toBe("missing");
    expect(consentEnding(new MemaxError("", "network_error", 502))).toBe(
      "failed",
    );
    expect(consentEnding(new ConsentLoadError("gone"))).toBe("gone");
    expect(consentEnding(new Error("x"))).toBe("failed");
  });
});

function space(over: Partial<ConsentSpaceView>): ConsentSpaceView {
  return {
    id: "s",
    name: "s",
    kind: "project",
    onV2: true,
    disabled: false,
    memories: null,
    people: null,
    compiles: null,
    autonomy: "propose",
    can: [],
    cannot: [],
    described: true,
    ...over,
  };
}

describe("consent copy", () => {
  const EN = en.ledger.consent;
  const ZH = zh.ledger.consent;

  it("describes each kind of space", () => {
    expect(spaceMetaLine(EN, space({ memories: 214 }))).toBe(
      "Project · 214 memories",
    );
    expect(spaceMetaLine(EN, space({ memories: 1 }))).toBe(
      "Project · 1 memory",
    );
    expect(spaceMetaLine(EN, space({}))).toBe("Project");
    expect(spaceMetaLine(EN, space({ kind: "team", people: 2 }))).toBe(
      "Team · 2 people",
    );
    expect(spaceMetaLine(EN, space({ kind: "team", people: 1 }))).toBe(
      "Team · 1 person",
    );
    expect(spaceMetaLine(EN, space({ kind: "personal", memories: 61 }))).toBe(
      "Just you",
    );
    expect(
      spaceMetaLine(EN, space({ kind: "team", people: 3, onV2: false })),
    ).toBe("Team · 3 people · still on V1");
    expect(spaceMetaLine(ZH, space({ memories: 214 }))).toBe(
      "项目 · 214 条记忆",
    );
  });

  it("says what can change later, for the level the agent gets", () => {
    expect(consentFootnote(EN, "Codex", space({}))).toBe(
      "You can allow Write, or disconnect Codex, any time in Agents.",
    );
    expect(consentFootnote(EN, "Cursor", space({ autonomy: "read" }))).toBe(
      "You can let Cursor propose or write, or disconnect it, any time in Agents.",
    );
    expect(consentFootnote(EN, "Codex", space({ onV2: false }))).toBe(
      "You can disconnect Codex any time in Agents.",
    );
    expect(consentFootnote(EN, "Codex", undefined)).toBe(
      "You can allow Write, or disconnect Codex, any time in Agents.",
    );
  });
});
