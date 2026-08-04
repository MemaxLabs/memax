"use client";

import { AGENT_IDENTITIES } from "@memaxlabs/ui/tokens/agents";
import { AGENT_BRAND_MARKS } from "@memaxlabs/ui/tokens/agent-brand-marks";
import { useLocale } from "@/i18n";

/**
 * Top-of-hero compatibility band — no chip chrome. Two typographic rows
 * instead:
 *
 *   Row 1 — MCP · CLI · SDK · Web                 (tiny metadata line)
 *   Row 2 — [mark] Claude Code  [mark] Cursor …   (mono wordmarks, no pill)
 *
 * Why no chips: chips are a functional UI pattern (filter, select, tag).
 * Using them for "what we support" makes a landing read as a dashboard.
 * 2026 devtool marketing (Mem0, Letta, Continue.dev, Sourcegraph Cody,
 * Clerk, Resend, Stripe) uses mono wordmark rows — transparent bg, no
 * border, brand mark + name in muted text. Lets the row group as a
 * single visual unit.
 *
 * Surfaces enumerated from docs.memax.app (REST + Hooks intentionally
 * omitted — REST is wrapped by SDK, Hooks are an MCP impl detail).
 * You/Team audience markers were dropped — the demo already proves the
 * audience claim, and the compatibility strip should stay about tooling.
 */

// Same source of truth as AGENT_IDENTITIES — adding an agent to tokens
// auto-flows here. "generic" and internal "memax" slugs are excluded from
// marketing surfaces intentionally.
const SUPPORTED_AGENT_SLUGS = [
  "claude-code",
  "claude-ai",
  "cursor",
  "codex",
  "copilot",
  "windsurf",
  "gemini",
  "openclaw",
  "hermes",
  "opencode",
] as const;

export function OverviewStrip() {
  const { t } = useLocale();

  const surfaces = [
    t.landing.surfaceMcpLabel,
    t.landing.surfaceCliLabel,
    t.landing.surfaceSdkLabel,
    t.landing.surfaceWebLabel,
  ];

  return (
    <div className="w-full flex flex-col items-center gap-3 sm:gap-3.5">
      {/* Surfaces — tiny metadata line with middle-dot separators */}
      <div className="flex flex-wrap items-center justify-center gap-x-2 gap-y-1 text-fg-4 text-[11px] tracking-[0.08em] uppercase font-medium">
        {surfaces.map((label, i) => (
          <span key={label} className="inline-flex items-center gap-2">
            <span>{label}</span>
            {i < surfaces.length - 1 && (
              <span aria-hidden className="text-fg-4/40">
                ·
              </span>
            )}
          </span>
        ))}
      </div>

      {/* Agents — mono wordmark row, no chip chrome */}
      <div className="flex flex-wrap items-center justify-center gap-x-4 sm:gap-x-5 gap-y-1.5 text-fg-3 text-[13px]">
        {SUPPORTED_AGENT_SLUGS.map((slug) => {
          const Mark = AGENT_BRAND_MARKS[slug];
          const name = AGENT_IDENTITIES[slug]?.displayName ?? slug;
          return (
            <span key={slug} className="inline-flex items-center gap-1.5">
              {Mark ? <Mark className="w-4 h-4" aria-hidden /> : null}
              <span>{name}</span>
            </span>
          );
        })}
      </div>
    </div>
  );
}
