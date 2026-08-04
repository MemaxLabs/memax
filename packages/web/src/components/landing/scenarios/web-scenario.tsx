"use client";

import { Sparkles } from "lucide-react";
import { AGENT_IDENTITIES } from "@memaxlabs/ui/tokens/agents";
import { useLocale } from "@/i18n";

/**
 * Coded recreation of the memax.app experience — the memory feed with agent
 * provenance rows, then a recall that synthesizes them into an answer with
 * sources. Narrative: Riley dog-foods memax while brainstorming memax itself,
 * dumping scrappy notes across Claude Code and Codex. Jordan (via OpenClaw)
 * asks "what's riley cooking in our team hub?" and memax synthesizes the
 * scattered notes into the product's own elevator pitch. The test-test-test
 * stray note stays in sources but never pollutes the answer — the
 * signal-from-noise claim is demonstrated, not stated. The demo is the pitch.
 */
export function WebScenario() {
  const { t } = useLocale();

  const demoEntries = [
    {
      agentSlug: "claude-code",
      time: t.landing.demoNote1Time,
      content: t.landing.demoNote1Content,
    },
    {
      agentSlug: "codex",
      time: t.landing.demoNote2Time,
      content: t.landing.demoNote2Content,
    },
    {
      agentSlug: "claude-code",
      time: t.landing.demoNote3Time,
      content: t.landing.demoNote3Content,
    },
  ];

  const askerAgent = AGENT_IDENTITIES[t.landing.demoAskerAgent] ?? null;
  const AskerIcon = askerAgent?.icon ?? Sparkles;
  const askerColor = askerAgent?.color ?? "oklch(0.65 0.12 220)";

  return (
    <div>
      <div className="divide-y divide-[oklch(from_var(--foreground)_l_c_h/0.06)]">
        {demoEntries.map((entry, idx) => {
          const agent = AGENT_IDENTITIES[entry.agentSlug];
          const Icon = agent?.icon ?? Sparkles;
          const color = agent?.color ?? "oklch(0.65 0.12 220)";
          return (
            <div
              key={idx}
              className="flex items-start gap-3 px-4 py-3 sm:px-5 sm:py-3.5"
            >
              {/* Agent icon — 28/32px, real AGENT_IDENTITIES shape + color */}
              <div
                className="w-7 h-7 sm:w-8 sm:h-8 rounded-chrome flex items-center justify-center shrink-0 mt-0.5"
                style={{
                  backgroundColor: `oklch(from ${color} l c h / 0.12)`,
                }}
              >
                <Icon
                  className="w-3.5 h-3.5 sm:w-4 sm:h-4"
                  style={{ color }}
                  strokeWidth={1.8}
                />
              </div>
              <div className="min-w-0 flex-1">
                {/* Attribution row — "Riley · saved via Claude Code" left,
                    timestamp right-aligned. Matches production MemoryRow. */}
                <div className="flex items-baseline gap-1.5 text-[13px] sm:text-sm">
                  <span className="font-medium text-fg-1">
                    {t.landing.demoActor}
                  </span>
                  <span className="text-fg-4">{t.landing.savedVia}</span>
                  <span className="text-fg-3">
                    {agent?.displayName ?? entry.agentSlug}
                  </span>
                  <span className="text-fg-4 ml-auto text-[12px]">
                    {entry.time}
                  </span>
                </div>
                <p className="text-fg-2 text-sm sm:text-[15px] mt-0.5 leading-snug">
                  {entry.content}
                </p>
              </div>
            </div>
          );
        })}
      </div>

      {/* Recall — Jordan (via OpenClaw) asks; memax synthesizes Riley's
          scattered notes into the product pitch. Asker row mirrors the
          save-row two-line provenance pattern. */}
      <div className="border-t-2 border-[oklch(from_var(--foreground)_l_c_h/0.08)] px-4 py-3.5 sm:px-5 sm:py-4 bg-[oklch(from_var(--foreground)_l_c_h/0.03)]">
        <div className="flex items-start gap-3 mb-3.5">
          <div
            className="w-7 h-7 sm:w-8 sm:h-8 rounded-chrome flex items-center justify-center shrink-0 mt-0.5"
            style={{
              backgroundColor: `oklch(from ${askerColor} l c h / 0.12)`,
            }}
          >
            <AskerIcon
              className="w-3.5 h-3.5 sm:w-4 sm:h-4"
              style={{ color: askerColor }}
              strokeWidth={1.8}
            />
          </div>
          <div className="min-w-0 flex-1">
            <div className="flex items-baseline gap-1.5 text-[13px] sm:text-sm">
              <span className="font-medium text-fg-1">
                {t.landing.demoAsker}
              </span>
              <span className="text-fg-4">{t.landing.askedVia}</span>
              <span className="text-fg-3">
                {askerAgent?.displayName ?? t.landing.demoAskerAgent}
              </span>
              <span className="text-fg-4 ml-auto text-[12px]">
                {t.landing.demoAskerTime}
              </span>
            </div>
            {/* Query on line 2 under the same icon — mirrors the save rows'
                "attribution + content" stack. */}
            <p className="mt-1 text-fg-2 text-sm sm:text-[15px] leading-snug">
              {t.landing.recallQuery}
            </p>
          </div>
        </div>

        {/* memax's synthesized answer — breathing ✦ is the memax voice
            marker, inline with the first word so the star belongs to the
            response, not the question. */}
        <p className="text-fg-1 text-sm sm:text-[15px] leading-relaxed">
          <span
            className="state-slow-breathe mr-1.5 inline-block leading-none"
            style={{
              color: "var(--signature)",
              verticalAlign: "0.08em",
            }}
          >
            {"✦"}
          </span>
          {t.landing.recallAnswer}
        </p>
        <div className="mt-3 flex flex-wrap items-center gap-x-2 gap-y-1 text-[12px] text-fg-4">
          <span>{t.landing.sourcesLabel}:</span>
          {demoEntries.map((entry, idx) => {
            const agent = AGENT_IDENTITIES[entry.agentSlug];
            return (
              <span key={idx} className="inline-flex items-center gap-1">
                <span className="text-fg-3">
                  {agent?.displayName ?? entry.agentSlug}
                </span>
                <span>{entry.time}</span>
                {idx < demoEntries.length - 1 && (
                  <span className="text-fg-4/50">·</span>
                )}
              </span>
            );
          })}
        </div>
      </div>
    </div>
  );
}
