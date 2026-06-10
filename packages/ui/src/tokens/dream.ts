/**
 * Dream palette — single source of truth for all dream-related colors,
 * gradients, and animations.
 *
 * Three primary hues:
 *   - Purple (290) — primary dream accent
 *   - Blue (260)   — secondary, used for "B" side, clean state
 *   - Pink (340)   — tertiary, aurora warmth
 *
 * Semantic colors:
 *   - Amber (#f59e0b) — conflict/contradiction (needs attention, not urgency)
 *   - Emerald — merged (auto-resolved)
 *   - Gray (foreground/30) — archived (stale)
 */

/* ── Core colors ── */

export const DREAM_PURPLE = "oklch(0.55 0.15 290)";
export const DREAM_BLUE = "oklch(0.60 0.12 260)";
export const DREAM_PINK = "oklch(0.65 0.14 340)";
export const DREAM_CONFLICT = "#f59e0b"; // amber — conflict icon, borders

/* ── Borders ── */

export const DREAM_BORDER = `1px solid oklch(0.55 0.15 290 / 0.25)`;
export const DREAM_BORDER_SUBTLE = `1px solid oklch(0.55 0.15 290 / 0.2)`;

/* ── Gradients ── */

/** Opaque dream bg for card surfaces (card stack shells, etc.) */
export const DREAM_CARD_BG =
  "linear-gradient(135deg, oklch(0.96 0.02 290), oklch(0.97 0.015 260))";

/** Translucent dream gradient for overlays, summary cards */
export const DREAM_GRADIENT_SUBTLE =
  "linear-gradient(135deg, oklch(0.55 0.15 290 / 0.12) 0%, oklch(0.60 0.12 260 / 0.15) 100%)";

/** Aurora gradient — animated background-position for dream feel */
export const DREAM_AURORA =
  "linear-gradient(135deg, oklch(0.55 0.15 290 / 0.12), oklch(0.60 0.12 260 / 0.15), oklch(0.65 0.14 340 / 0.10), oklch(0.55 0.15 290 / 0.12))";

/** Glow — radial gradient behind cards */
export const DREAM_GLOW =
  "radial-gradient(ellipse at center, oklch(0.55 0.15 290 / 0.15), oklch(0.60 0.12 260 / 0.08), transparent 70%)";

/** Progress bar / linear accent */
export const DREAM_PROGRESS = `linear-gradient(90deg, ${DREAM_PURPLE}, ${DREAM_BLUE}, ${DREAM_PINK})`;

/* ── Banner-specific (higher chroma for active state) ── */

export const DREAM_BANNER = {
  /** Completed dream — subtle aurora */
  dream: {
    accent: DREAM_PURPLE,
    bg: `linear-gradient(135deg, oklch(0.55 0.15 290 / 0.10), oklch(0.60 0.12 260 / 0.12), oklch(0.65 0.14 340 / 0.08), oklch(0.55 0.15 290 / 0.10))`,
  },
  /** Active dreaming — intensified chroma */
  dreaming: {
    accent: "oklch(0.60 0.18 290)",
    bg: `linear-gradient(135deg, oklch(0.55 0.18 290 / 0.18), oklch(0.60 0.15 260 / 0.20), oklch(0.65 0.16 340 / 0.14), oklch(0.55 0.18 290 / 0.18))`,
  },
};

/* ── A/B comparison tints ── */

export const DREAM_TINT_A = {
  bg: "oklch(0.55 0.15 290 / 0.05)",
  border: "1px solid oklch(0.55 0.15 290 / 0.1)",
};

export const DREAM_TINT_B = {
  bg: "oklch(0.60 0.12 260 / 0.05)",
  border: "1px solid oklch(0.60 0.12 260 / 0.1)",
};

/* ── CTA button styles ── */

export const DREAM_CTA = {
  primary: {
    border: "1px solid oklch(0.60 0.12 260 / 0.25)",
    background: "oklch(0.60 0.12 260 / 0.08)",
  },
  secondary: {
    border: "1px solid oklch(0.55 0.15 290 / 0.2)",
    background: "oklch(0.55 0.15 290 / 0.05)",
  },
};

/* ── Conflict card styling ── */

export const DREAM_CONFLICT_CARD = {
  border: "1px solid oklch(from #f59e0b l c h / 0.2)",
  background: "oklch(from #f59e0b l c h / 0.04)",
};

/* ── Ambient backgrounds (login, settings) ── */

/** Three-layer radial ambient glow for large surfaces */
export const DREAM_AMBIENT =
  "radial-gradient(ellipse 60% 50% at 50% 45%, oklch(0.62 0.16 290 / 0.06) 0%, transparent 70%), radial-gradient(ellipse 40% 40% at 35% 55%, oklch(0.60 0.12 260 / 0.04) 0%, transparent 60%), radial-gradient(ellipse 35% 35% at 65% 50%, oklch(0.65 0.14 340 / 0.03) 0%, transparent 60%)";

/** Settings section — very subtle dream tint */
export const DREAM_SETTINGS_BG =
  "linear-gradient(135deg, oklch(0.55 0.15 290 / 0.04) 0%, oklch(0.60 0.12 260 / 0.06) 50%, oklch(0.65 0.14 340 / 0.03) 100%)";

/* ── Keyframe animations (inject via <style>) ── */

export const DREAM_KEYFRAMES = `
@keyframes dream-aurora {
  0% { background-position: 0% 50%; }
  50% { background-position: 100% 50%; }
  100% { background-position: 0% 50%; }
}
@keyframes dream-glow-pulse {
  0%, 100% { opacity: 0.5; transform: scale(1); }
  50% { opacity: 0.8; transform: scale(1.05); }
}
`;

/** Banner-specific keyframes (slightly different timing names) */
export const DREAM_BANNER_KEYFRAMES = `
@keyframes bar-notif-aurora {
  0% { background-position: 0% 50%; }
  50% { background-position: 100% 50%; }
  100% { background-position: 0% 50%; }
}
@keyframes bar-dreaming-glow {
  0%, 100% { opacity: 0.6; }
  50% { opacity: 1; }
}
`;
