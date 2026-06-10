import { describe, expect, it } from "vitest";
import { en } from "@/i18n/locales/en";
import {
  linkedAgentAttributionCopy,
  standaloneAgentAttributionText,
} from "@/lib/memory-attribution-copy";
import type { ResolvedMemoryAttribution } from "@/lib/memory-attribution";

function interpolate(template: string, vars: Record<string, string>) {
  return Object.entries(vars).reduce(
    (result, [key, value]) => result.replaceAll(`{${key}}`, value),
    template,
  );
}

function makeAttribution(
  overrides: Partial<ResolvedMemoryAttribution>,
): ResolvedMemoryAttribution {
  return {
    hasAgent: true,
    isOwnMemory: true,
    authorName: null,
    authorAvatar: null,
    createdByType: "agent",
    initiationType: "agent_proactive",
    isHumanRequestedAgent: false,
    isAgentCapture: true,
    isLegacyUnknownAgentAttribution: false,
    isSystem: false,
    agentDisplayName: "Codex",
    agentIconEmoji: null,
    agentIdentity: null,
    ...overrides,
  };
}

describe("memory attribution copy", () => {
  it("keeps regular standalone agent capture wording passive", () => {
    expect(
      standaloneAgentAttributionText(
        makeAttribution({}),
        en,
        interpolate,
        "regular",
      ),
    ).toBe("Captured by Codex");
  });

  it("keeps compact agent-first wording terse for standalone agent rows", () => {
    expect(
      standaloneAgentAttributionText(
        makeAttribution({}),
        en,
        interpolate,
        "compact",
      ),
    ).toBe("captured");
  });

  it("uses the saved-with phrasing for human-requested agent actions", () => {
    expect(
      standaloneAgentAttributionText(
        makeAttribution({
          createdByType: "human",
          initiationType: "human_requested_agent",
          isHumanRequestedAgent: true,
          isAgentCapture: false,
        }),
        en,
        interpolate,
        "regular",
      ),
    ).toBe("You saved with Codex");
  });

  it("returns passive linked copy fragments for metadata surfaces", () => {
    expect(linkedAgentAttributionCopy(makeAttribution({}), en)).toEqual({
      prefix: "captured by",
      suffix: "",
    });
  });

  it("keeps legacy unknown agent rows neutral instead of forcing capture semantics", () => {
    expect(
      standaloneAgentAttributionText(
        makeAttribution({
          initiationType: "unknown",
          isLegacyUnknownAgentAttribution: true,
        }),
        en,
        interpolate,
        "regular",
      ),
    ).toBe("From Codex");

    expect(
      linkedAgentAttributionCopy(
        makeAttribution({
          initiationType: "unknown",
          isLegacyUnknownAgentAttribution: true,
        }),
        en,
      ),
    ).toEqual({
      prefix: "via",
      suffix: "",
    });
  });
});
