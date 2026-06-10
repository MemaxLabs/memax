"use client";

/**
 * ChatEmptyState — the first-run surface when the user lands on
 * /brain with no active session.
 *
 * Design philosophy (2026 redesign):
 *
 * 1. Personalized greeting, not a generic title. ChatGPT, Claude,
 *    Gemini, Cursor, Linear AI all converged on personalization
 *    here — "Hey {name}, what's on your mind?" reads warmer than
 *    "Ask anything" and signals that this surface knows who you are.
 *    Fallback for unnamed users hits the same shape minus the name.
 *
 * 2. USP-forward subtitle. memax's unique value is its memory of
 *    your notes + your team's + your agents' work. Earlier copy
 *    listed features ("recalls, reasons, acts on your behalf") and
 *    read like a spec sheet. The new line names what memax HAS
 *    READ and invites a question — the user supplies the shape.
 *
 * 3. Two-column prompt grid on desktop, single column on mobile.
 *    Four suggestions instead of three so the grid feels balanced.
 *    Each prompt is action-typed (verb-first) so the user can pick
 *    one and immediately know what they're asking for.
 *
 * 4. Generous vertical spacing. Linear / Notion empty states breathe.
 *    The old layout crammed glyph + title + subtitle + chips into a
 *    small max-w-2xl block; the new one lets each section sit at its
 *    natural density.
 *
 * 5. ✦ glyph kept but moved to subtle accent above the greeting —
 *    a small brand presence, not the visual lead. The greeting
 *    text is the focal point.
 */

import { useMemo } from "react";
import type { ReactNode } from "react";
import { cn } from "@memaxlabs/ui";
import { useAuth } from "@/lib/auth";
import { useOnboardingActivation } from "@/hooks/use-onboarding-activation";
import { useLocale, useInterpolate } from "@/i18n";

export interface ChatEmptyStateProps {
  onPrompt: (prompt: string) => void;
}

interface SuggestionRecord {
  id: string;
  prompt: string;
  icon?: ReactNode;
}

export function ChatEmptyState({ onPrompt }: ChatEmptyStateProps) {
  const { t } = useLocale();
  const interpolate = useInterpolate();
  const { user } = useAuth();
  // Activation gate via shared hook — same checklist-derived SOT
  // the /brain redirect uses. See hooks/use-onboarding-activation.ts.
  const { isActivated } = useOnboardingActivation();

  // First-name only — display_name might be "Derek Ye" or a single
  // string. Take the first whitespace-delimited token so the
  // greeting reads casual ("Hey Derek, …") not formal ("Hey Derek
  // Ye, …").
  const firstName = useMemo(() => {
    const raw = user?.display_name ?? user?.name ?? "";
    const trimmed = raw.trim();
    if (!trimmed) return "";
    return trimmed.split(/\s+/)[0] ?? "";
  }, [user?.display_name, user?.name]);

  const greeting = firstName
    ? interpolate(t.chat.emptyGreeting, { name: firstName })
    : t.chat.emptyGreetingFallback;

  const suggestions: SuggestionRecord[] = isActivated
    ? [
        { id: "catch-up", prompt: t.chat.suggestions.catchUp },
        { id: "find", prompt: t.chat.suggestions.findConflicts },
        { id: "focus", prompt: t.chat.suggestions.focus },
        { id: "plan", prompt: t.chat.suggestions.planWeek },
      ]
    : [
        { id: "onboard-corpus", prompt: t.chat.suggestions.onboardCorpus },
        { id: "onboard-setup", prompt: t.chat.suggestions.onboardSetup },
        { id: "onboard-save", prompt: t.chat.suggestions.onboardSave },
        { id: "onboard-organize", prompt: t.chat.suggestions.onboardOrganize },
      ];

  return (
    <div className="flex w-full flex-col items-center text-center">
      <div className="w-full max-w-2xl">
        {/* ✦ brand accent. Smaller than the prior 32px hero so the
            greeting reads as the primary focus. */}
        <div
          className="mx-auto mb-4 flex items-center justify-center text-[16px]"
          style={{ color: "var(--signature)" }}
          aria-hidden
        >
          ✦
        </div>

        {/* Greeting — display-typography weight + tight tracking so
            the name reads with personality, not generic UI chrome. */}
        <h1
          className="text-display-2 text-fg-1 font-semibold sm:text-display-1"
          style={{ letterSpacing: "-0.02em" }}
        >
          {greeting}
        </h1>

        <p className="mx-auto mt-4 max-w-md text-[14px] leading-relaxed text-fg-3 sm:text-[15px]">
          {t.chat.emptySubtitle}
        </p>

        {/* Suggestions: 2-column grid on >=sm, single column on
            mobile so each prompt has room to breathe rather than
            wrapping awkwardly. Buttons are action-typed and the
            border is subtle — they should read as gentle invitations,
            not loud CTAs. */}
        <div className="mt-8 grid grid-cols-1 gap-2 sm:grid-cols-2">
          {suggestions.map((s) => (
            <button
              key={s.id}
              type="button"
              onClick={() => onPrompt(s.prompt)}
              className={cn(
                "group flex items-center gap-2 rounded-xl border border-border/40 bg-surface-1 px-4 py-3 text-left",
                "text-[14px] text-fg-2 transition-all duration-150",
                "hover:border-border/80 hover:bg-surface-2 hover:text-fg-1",
                "active:scale-[0.98]",
              )}
            >
              <span className="flex-1 truncate">{s.prompt}</span>
              <span
                aria-hidden
                className="text-fg-4 transition-colors group-hover:text-fg-2"
              >
                →
              </span>
            </button>
          ))}
        </div>
      </div>
    </div>
  );
}
