"use client";

import { MemaxWordmark } from "@memaxlabs/ui";
import { Globe, Moon, Sparkles, Sun } from "lucide-react";
import { AGENT_IDENTITIES } from "@memaxlabs/ui/tokens/agents";
import { useLocale } from "@/i18n";
import { useTheme } from "next-themes";
import { DOCS_URL } from "@/lib/urls";
import { HeroWaitlist } from "./hero-waitlist";
import { OverviewStrip } from "./overview-strip";

export function LandingFull() {
  const { t, locale, setLocale } = useLocale();
  const { theme, setTheme } = useTheme();

  const cycleTheme = () => {
    document.documentElement.classList.add("theme-transition");
    const next =
      theme === "light" ? "dark" : theme === "dark" ? "system" : "light";
    setTheme(next);
    setTimeout(
      () => document.documentElement.classList.remove("theme-transition"),
      500,
    );
  };

  // Riley dog-foods memax while brainstorming memax itself — dumping scrappy
  // notes across Claude Code and Codex. Jordan (via Cursor) later asks "what's
  // riley cooking in our team hub?" and memax synthesizes the scattered notes
  // into the product's own elevator pitch. The test-test-test stray note stays
  // in sources but never pollutes the answer — the signal-from-noise claim is
  // demonstrated, not stated. The demo is the pitch.
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

  return (
    <div className="min-h-dvh flex flex-col items-center sm:justify-center px-6 sm:px-8 py-12 sm:py-16 bg-background">
      {/* Theme + language. Sign in is now a first-class CTA in the hero
          (HeroWaitlist secondary button) — not duplicated here. */}
      <div className="fixed top-0 right-0 z-10 flex items-center gap-0.5 p-3 sm:p-5">
        <button
          onClick={cycleTheme}
          className="p-2 rounded-chrome text-fg-4 hover:text-fg-2 transition-colors"
        >
          {theme === "dark" ? (
            <Moon className="w-4 h-4" />
          ) : (
            <Sun className="w-4 h-4" />
          )}
        </button>
        <button
          onClick={() => setLocale(locale === "en" ? "zh" : "en")}
          className="flex items-center gap-1 px-2 py-1.5 rounded-chrome text-fg-4 hover:text-fg-2 transition-colors text-[12px] font-medium"
        >
          <Globe className="w-3.5 h-3.5" />
          {locale === "en" ? "中文" : "EN"}
        </button>
      </div>

      {/* max-w-170 = Linear/Vercel standard hero width */}
      <div className="w-full max-w-170 flex flex-col items-center gap-8 sm:gap-10 lg:gap-12">
        {/* Logo + early-access badge row — brand-status framing (Linear /
            Arc / Framer pattern: product mark carries its rollout phase as
            a sibling pill). Previously the badge sat below the CTA as fine
            print; surfacing it at the brand level reads as "real product in
            controlled rollout" rather than "heads up, this is gated". */}
        <div className="flex items-center gap-2.5 sm:gap-3">
          <MemaxWordmark height={24} className="text-fg-1 sm:h-7" />
          <span className="inline-flex items-center gap-1.5 rounded-full border border-border/60 bg-surface-1 px-2.5 py-0.5 text-[11px] font-medium text-fg-3">
            <span
              aria-hidden
              className="h-1.5 w-1.5 rounded-full"
              style={{ background: "var(--signature)" }}
            />
            {t.landing.ctaMicrocopy}
          </span>
        </div>

        {/* Headline — display-sized, 48/60/80px. Mobile scale bumped so the
            hero keeps 视觉冲击力 on phone widths where the desktop scale
            alone wasn't carrying; tighter tracking + bold weight match the
            2026 devtool display hero pattern (Linear/Vercel/Framer). */}
        <div className="text-center space-y-3 sm:space-y-4">
          <h1
            className="font-bold text-fg-1 text-[3rem] sm:text-[3.75rem] lg:text-[5rem] leading-[1.02] whitespace-pre-line"
            style={{ letterSpacing: "-0.045em" }}
          >
            {t.landing.headline}
          </h1>
          {/* Subtitle — 16/18/20px */}
          <p className="text-fg-3 text-base sm:text-lg lg:text-xl max-w-120 mx-auto">
            {t.landing.sublineFull}
          </p>
        </div>

        {/* High-level overview — surfaces (MCP/CLI/Web) + agent brand marks.
            Answers "what does this work with?" above the fold, before the
            CTA. See overview-strip.tsx for the rationale on placement. */}
        <OverviewStrip />

        {/* Primary CTA — hero-anchored waitlist (morphs inline) */}
        <HeroWaitlist />

        {/* Demo card */}
        <div className="w-full rounded-surface overflow-hidden border border-[oklch(from_var(--foreground)_l_c_h/0.08)] bg-[oklch(from_var(--foreground)_l_c_h/0.02)]">
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
                    {/* Attribution row — "Jordan · saved via Claude Code" on
                        the left, timestamp right-aligned. Matches production
                        MemoryRow provenance layout. */}
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

          {/* Recall — Jordan (via OpenClaw) asks a teammate question; memax
              synthesizes Riley's scattered notes into the product pitch.
              Asker row mirrors the save-row two-line provenance pattern:
              attribution on line 1, ✦ query on line 2 under the same icon.
              The synthesis answer and source chips live in a separate block
              below so the "ask" and the "response" read as distinct beats.
              Source chips match production SourceRefItem. */}
          <div className="border-t-2 border-[oklch(from_var(--foreground)_l_c_h/0.08)] px-4 py-3.5 sm:px-5 sm:py-4 bg-[oklch(from_var(--foreground)_l_c_h/0.03)]">
            {(() => {
              const askerAgent =
                AGENT_IDENTITIES[t.landing.demoAskerAgent] ?? null;
              const AskerIcon = askerAgent?.icon ?? Sparkles;
              const askerColor = askerAgent?.color ?? "oklch(0.65 0.12 220)";
              return (
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
                    {/* Query sits on line 2 under the same icon — mirrors the
                        save rows' "attribution + content" stack. */}
                    <p className="mt-1 text-fg-2 text-sm sm:text-[15px] leading-snug">
                      {t.landing.recallQuery}
                    </p>
                  </div>
                </div>
              );
            })()}
            {/* Memax's synthesized answer. The breathing ✦ is the memax voice
                marker — horizontally aligned inline with the first word so the
                star belongs to the response, not the question. Matches the
                production AI-answer pattern from the recall flow. */}
            <p className="text-fg-1 text-sm sm:text-[15px] leading-relaxed">
              <span
                className="state-slow-breathe mr-1.5 inline-block leading-none"
                style={{
                  color: "var(--signature)",
                  verticalAlign: "0.08em",
                }}
              >
                {"\u2726"}
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
      </div>

      {/* Footer — 12px */}
      <div className="mt-auto pt-8 sm:pt-10 pb-5 flex flex-wrap items-center justify-center gap-x-5 gap-y-1 text-xs text-fg-4">
        <span>{t.landing.copyright}</span>
        <a href={DOCS_URL} className="hover:text-fg-3 transition-colors">
          {t.landing.docs}
        </a>
        <a
          href="https://github.com/MemaxLabs/memax"
          className="hover:text-fg-3 transition-colors"
        >
          {t.landing.github}
        </a>
        <a href="/privacy" className="hover:text-fg-3 transition-colors">
          {t.landing.privacy}
        </a>
        <a href="/terms" className="hover:text-fg-3 transition-colors">
          {t.landing.terms}
        </a>
      </div>
    </div>
  );
}
