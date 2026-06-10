import type { Translations } from "@/i18n/locales/en";
import type { ResolvedMemoryAttribution } from "@/lib/memory-attribution";

export interface LinkedAgentAttributionCopy {
  prefix: string;
  suffix: string;
}

export function standaloneAgentAttributionText(
  attribution: ResolvedMemoryAttribution,
  t: Translations,
  interpolate: (template: string, vars: Record<string, string>) => string,
  variant: "compact" | "regular",
): string {
  if (attribution.isLegacyUnknownAgentAttribution) {
    return interpolate(t.attribution.fromAgent, {
      agent: attribution.agentDisplayName,
    });
  }
  if (attribution.isHumanRequestedAgent) {
    return variant === "compact"
      ? interpolate(t.attribution.withAgent, {
          agent: attribution.agentDisplayName,
        })
      : interpolate(t.attribution.youViaPushed, {
          agent: attribution.agentDisplayName,
        });
  }
  return variant === "compact"
    ? t.attribution.captured
    : interpolate(t.attribution.capturedByAgent, {
        agent: attribution.agentDisplayName,
      });
}

export function linkedAgentAttributionCopy(
  attribution: ResolvedMemoryAttribution,
  t: Translations,
): LinkedAgentAttributionCopy {
  if (attribution.isLegacyUnknownAgentAttribution) {
    return {
      prefix: t.attribution.via,
      suffix: "",
    };
  }
  if (attribution.isHumanRequestedAgent) {
    return {
      prefix: t.attribution.savedWithAgentPrefix,
      suffix: t.attribution.savedWithAgentSuffix,
    };
  }
  return {
    prefix: t.attribution.capturedByAgentPrefix,
    suffix: t.attribution.capturedByAgentSuffix,
  };
}
