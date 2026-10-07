import { MemaxError } from "memax-sdk";
import { describe, expect, it } from "vitest";
import { en } from "@/i18n/locales/en";
import { zh } from "@/i18n/locales/zh";
import { consentFootnote, spaceMetaLine } from "../consent-copy";
import {
  cleanClientName,
  compiledFor,
  ConsentDecisionError,
  ConsentLoadError,
  consentEnding,
  consentRefusal,
  followable,
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

  it("puts spaces on V2 first, then projects, teams and Personal", () => {
    const [project, team, personal] = demoConsentRequest.spaces;
    const view = toConsentRequest({
      ...demoConsentRequest,
      client_name: "Claude",
      agent_name: "claude-ai",
      spaces: [
        { ...personal!, on_v2: false, name: "Personal", memories: 12 },
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
    expect(view.client).toEqual({
      name: "Claude",
      agent: "claude",
      host: null,
    });
    expect(view.person).toBe("Ziyang");
    // V1 counts its V1 memories and has no level; V2 its kept ones.
    expect(view.spaces[3]).toMatchObject({
      memories: 12,
      autonomy: null,
      ceiling: null,
    });
    expect(view.spaces[0]).toMatchObject({
      memories: 214,
      autonomy: "propose",
      ceiling: "write",
    });
  });

  it("keeps abilities it knows and drops ones it doesn't", () => {
    const view = toConsentRequest({
      ...demoConsentRequest,
      spaces: [
        {
          ...demoConsentRequest.spaces[0]!,
          can: ["read_brief", "teleport"],
          cannot: ["forget"],
        },
      ],
    });
    expect(view.spaces[0]!.can).toEqual(["read_brief"]);
    expect(view.spaces[0]!.cannot).toEqual(["forget"]);
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

  it("reads the API's refusals as endings and decisions", () => {
    const e = (code: string, status: number) =>
      new MemaxError("", code, status);
    expect(consentEnding(e("consent_request_expired", 410))).toBe("expired");
    expect(consentEnding(e("consent_request_not_found", 404))).toBe("gone");
    expect(consentEnding(e("consent_by_person_on_web", 403))).toBe("refused");
    expect(consentEnding(e("network_error", 502))).toBe("failed");
    expect(consentEnding(new ConsentLoadError("gone"))).toBe("gone");
    expect(consentEnding(new Error("x"))).toBe("failed");
    expect(consentRefusal(e("consent_space", 422)).refusal).toBe("space");
    expect(consentRefusal(e("consent_request_expired", 410))).toMatchObject({
      refusal: "ended",
      ending: "expired",
    });
    expect(consentRefusal(e("network_error", 502)).refusal).toBe("failed");
    const own = new ConsentDecisionError("space");
    expect(consentRefusal(own)).toBe(own);
  });

  it("follows only http(s) URLs", () => {
    expect(followable("https://claude.ai/api/mcp/auth_callback?code=c")).toBe(
      true,
    );
    expect(followable("http://127.0.0.1:1455/callback?code=c")).toBe(true);
    expect(followable("javascript:alert(1)")).toBe(false);
    expect(followable("data:text/html,x")).toBe(false);
    expect(followable("/relative")).toBe(false);
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
    ceiling: "write",
    can: [],
    cannot: [],
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

  it("offers only what a person may change later, up to the ceiling", () => {
    const at = (
      autonomy: "read" | "propose",
      ceiling: "read" | "propose" | "write",
    ) => consentFootnote(EN, "Codex", space({ autonomy, ceiling }));
    expect(at("propose", "write")).toBe(
      "You can allow Write, or disconnect Codex, any time in Agents.",
    );
    expect(at("read", "write")).toBe(
      "You can let Codex propose or write, or disconnect it, any time in Agents.",
    );
    expect(at("read", "propose")).toBe(
      "You can let Codex propose, or disconnect it, any time in Agents.",
    );
    // A viewer's agent, or a scope that only proposes: nothing to raise.
    expect(at("propose", "propose")).toBe(
      "You can disconnect Codex any time in Agents.",
    );
    expect(at("read", "read")).toBe(
      "You can disconnect Codex any time in Agents.",
    );
    expect(consentFootnote(EN, "Codex", space({ onV2: false }))).toBe(
      "You can disconnect Codex any time in Agents.",
    );
    expect(consentFootnote(EN, "Codex", undefined)).toBe(
      "You can disconnect Codex any time in Agents.",
    );
  });
});
