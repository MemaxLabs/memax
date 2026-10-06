import { describe, expect, it } from "vitest";
import { en } from "@/i18n/locales/en";
import { zh } from "@/i18n/locales/zh";
import type { AgentRefusal } from "../data/agents";
import { agoText, changedText, refusalText, unavailableText } from "./copy";

const copy = en.ledger.agents;
const NOW = new Date("2026-10-05T14:40:00-07:00");
const TZ = "America/Vancouver";
const vars = {
  agent: "Codex",
  level: "Propose",
  space: "memax-v2",
  role: "owner" as const,
};

describe("refusalText", () => {
  it("explains a raise without a verified web session, with a way on", () => {
    const r = refusalText(copy, "needs_web", vars);
    expect(r.signIn).toBe(true);
    expect(r.text).toBe(
      "Memax couldn't confirm this came from you on memax.app, so Codex stays at Propose. Sign in again here, then raise it.",
    );
    expect(refusalText(copy, "needs_web", { ...vars, dev: true }).text).toMatch(
      /set WEB_SURFACE_SECRET for the web app and the API\.$/,
    );
    expect(
      refusalText(copy, "needs_web", { ...vars, command: "resume" }).text,
    ).toContain("Codex stays paused");
  });

  it("words every refusal in both locales, with no placeholder left", () => {
    const all: AgentRefusal[] = [
      "needs_web",
      "key_max_propose",
      "not_your_agent",
      "not_allowed",
      "person_must_manage",
      "not_member",
      "surface_unverified",
      "invalid_transition",
      "not_found",
      "failed",
    ];
    for (const catalogue of [copy, zh.ledger.agents]) {
      for (const refusal of all) {
        const { text } = refusalText(catalogue, refusal, vars);
        expect(text, refusal).not.toMatch(/\{\w+\}/);
        expect(text.length).toBeGreaterThan(10);
      }
    }
  });

  it("tells a viewer to ask a member, and anyone else about the ceiling", () => {
    expect(
      refusalText(copy, "not_allowed", { ...vars, role: "viewer" }).text,
    ).toBe("Viewers can't raise what agents may do in memax-v2. Ask a member.");
    expect(refusalText(copy, "not_allowed", vars).text).toMatch(
      /^Codex can propose at most in memax-v2/,
    );
    expect(refusalText(copy, "failed", vars).text).toBe(
      "That didn't go through, so Codex stays at Propose. Try again.",
    );
  });
});

describe("changedText and unavailableText", () => {
  it("says what the agent does now", () => {
    expect(
      changedText(copy, "propose", { agent: "Codex", space: "memax-v2" }),
    ).toBe("Codex proposes in memax-v2 now. Its writes wait in Review.");
    expect(unavailableText(copy, "keyMaxPropose", "Codex")).toBe(
      "API keys propose at most. Connect Codex over OAuth to let it write.",
    );
  });
});

describe("agoText", () => {
  const ago = (iso: string | null, lower = false) =>
    agoText(copy, iso, NOW, TZ, "en", { lower });

  it("is relative within a day and absolute after, as the boards write it", () => {
    expect(ago("2026-10-05T14:38:00-07:00")).toBe("2 min ago");
    expect(ago("2026-10-05T14:26:00-07:00")).toBe("14 min ago");
    expect(ago("2026-10-05T13:40:00-07:00")).toBe("1 h ago");
    expect(ago("2026-10-05T11:40:00-07:00")).toBe("3 h ago");
    expect(ago("2026-10-04T17:05:00-07:00")).toBe("Yesterday");
    expect(ago("2026-10-04T17:05:00-07:00", true)).toBe("yesterday");
    expect(ago("2026-09-28T10:00:00-07:00")).toBe("Sep 28");
    expect(ago(null)).toBe("never");
    expect(ago("2026-10-05T14:39:40-07:00")).toBe("just now");
  });

  it("speaks Chinese", () => {
    expect(
      agoText(zh.ledger.agents, "2026-10-05T14:26:00-07:00", NOW, TZ, "zh"),
    ).toBe("14 分钟前");
    expect(
      agoText(zh.ledger.agents, "2026-09-28T10:00:00-07:00", NOW, TZ, "zh"),
    ).toBe("9月28日");
  });
});
