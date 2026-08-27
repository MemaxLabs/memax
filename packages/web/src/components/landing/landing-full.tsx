"use client";

import { useState } from "react";
import { MemaxWordmark } from "@memaxlabs/ui";
import { Globe, Moon, Sun } from "lucide-react";
import { useLocale } from "@/i18n";
import { useTheme } from "next-themes";
import { DOCS_URL } from "@/lib/urls";
import { BenchmarkStrip } from "./benchmark-strip";
import { HeroWaitlist } from "./hero-waitlist";
import { OverviewStrip } from "./overview-strip";
import { PivotToggle, type LandingPivot } from "./pivot-toggle";
import { RotatingHeadline } from "./rotating-headline";
import { ScenarioShowcase } from "./scenario-showcase";
import { UseCaseShowcase } from "./use-case-showcase";

// Canonical outward contact — same address privacy/terms already publish.
const TEAM_CONTACT_EMAIL = "team@memaxlabs.com";

export function LandingFull() {
  const { t, locale, setLocale } = useLocale();
  const { theme, setTheme } = useTheme();
  // Audience pivot: personal = self-serve waitlist story, team = shared-brain
  // story plus a direct contact channel. Hero copy and CTA extras switch;
  // the overview strip and scenario showcase are audience-agnostic proof.
  const [pivot, setPivot] = useState<LandingPivot>("personal");
  const isTeam = pivot === "team";

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
            a sibling pill). */}
        <div className="flex items-center gap-2.5 sm:gap-3 animate-fade-up">
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

        {/* Hero — audience pivot above the rotating headline. Headline and
            subline switch instantly with the pivot; the rotating word cycle
            restarts via the key remount. */}
        <div className="flex flex-col items-center gap-6 sm:gap-7 text-center animate-fade-up stagger-1">
          <PivotToggle pivot={pivot} onChange={setPivot} />
          <div className="space-y-3 sm:space-y-4">
            <RotatingHeadline
              key={`${pivot}-${locale}`}
              prefix={
                isTeam ? t.landing.heroTeamPrefix : t.landing.heroPersonalPrefix
              }
              words={
                isTeam ? t.landing.heroTeamWords : t.landing.heroPersonalWords
              }
              wordLine={t.landing.heroWordLine}
            />
            {/* Subtitle — 16/18/20px */}
            <p className="text-fg-3 text-base sm:text-lg lg:text-xl max-w-120 mx-auto">
              {isTeam ? t.landing.sublineTeam : t.landing.sublineFull}
            </p>
          </div>
        </div>

        {/* High-level overview — surfaces (MCP/CLI/Web) + agent brand marks.
            Audience-agnostic, so it doesn't switch with the pivot. */}
        <div className="w-full animate-fade-up stagger-2">
          <OverviewStrip />
        </div>

        {/* Primary CTA — hero-anchored waitlist (morphs inline). A
            time-to-value promise sits under it (competitive table stakes:
            the ask is followed immediately by how cheap saying yes is).
            Team pivot adds a direct sales-touch line below that. */}
        <div className="w-full flex flex-col items-center gap-3.5 animate-fade-up stagger-3">
          <HeroWaitlist />
          <p className="text-[13px] text-fg-4 text-center">
            {t.landing.timePromise}
          </p>
          {isTeam && (
            <p className="text-[13px] text-fg-4 text-center">
              {t.landing.teamContactPrompt}{" "}
              <a
                href={`mailto:${TEAM_CONTACT_EMAIL}`}
                className="text-fg-2 underline underline-offset-2 hover:text-fg-1 transition-colors"
              >
                {t.landing.teamContactCta} {"→"}
              </a>
            </p>
          )}
        </div>

        {/* Benchmark proof — directly after the ask. This is evidence no
            competitor in the category shows (LongMemEval), so it outranks
            the showcase: numbers first, then what using it looks like. */}
        <div className="w-full animate-fade-up stagger-4">
          <BenchmarkStrip />
        </div>

        {/* Scenario showcase — coded recreations of the four real surfaces
            (Claude Code, terminal, memax.app, third-party agent). */}
        <div className="w-full animate-fade-up stagger-5">
          <ScenarioShowcase />
        </div>

        {/* Use-case showcase (G2) — four acts of what you actually do
            with it, each a demo-kit composition. Surfaces above showed
            WHERE memax lives; these show WHY. */}
        <div className="w-full animate-fade-up stagger-5">
          <UseCaseShowcase />
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
