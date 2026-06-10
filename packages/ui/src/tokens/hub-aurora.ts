/**
 * Hub aurora gradient tokens — design tokens, not UI components.
 *
 * Living here (`tokens/`, no `"use client"`) instead of in
 * `components/hub-header-banner.tsx` so server components, route layouts,
 * and the shell-v2 page background can import them without crossing the
 * Next.js server/client component graph through a client module.
 *
 * Single source of truth for both the hub header banner and any other
 * surface that wants to mirror the same material — e.g., shell-v2 page
 * background, settings preview tiles.
 */

/**
 * Time-of-day buckets driving the gradient when a hub uses
 * `header_aurora_mode === "time"`. Bucket boundaries match human
 * day-segment intuition (`getHubHeaderTimeBucket(date)` below).
 */
export type HubHeaderTimeBucket =
  | "deep-night"
  | "morning"
  | "noon"
  | "afternoon"
  | "evening"
  | "late-night";

/**
 * Per-hub aurora preference. `signature` renders the brand-color variant;
 * `time` rotates through `AURORA_GRADIENTS` based on local time;
 * `none` disables the gradient entirely.
 */
export type HubHeaderAuroraMode = "none" | "signature" | "time";

/**
 * Brand-color aurora — used when a hub opts into the signature variant.
 * One radial in violet (memax brand) + a faint magenta highlight. Both
 * stops are low alpha so the gradient reads as atmosphere, not chrome.
 *
 * Alpha tuned 2026-05-18: halved from the original 0.22/0.14 to
 * 0.11/0.07 — the louder values read as a "wash" over content rather
 * than atmosphere, especially on the Ask surface where the gradient
 * competed with chat readability. Lower alphas keep the brand cue
 * (hub-identity recognition) without crowding text.
 */
export const SIGNATURE_AURORA =
  "radial-gradient(80% 140% at 25% 20%, oklch(0.55 0.15 290 / 0.11), transparent 65%), radial-gradient(60% 100% at 80% 0%, oklch(0.65 0.14 320 / 0.07), transparent 70%)";

/**
 * Time-of-day aurora gradients keyed by `HubHeaderTimeBucket`. Each entry
 * is a two-stop radial gradient tuned for the time segment it represents.
 * Alphas halved 2026-05-18 (≤ 0.11) so the gradient sits behind text
 * without competing — see SIGNATURE_AURORA note for rationale.
 */
export const AURORA_GRADIENTS: Record<HubHeaderTimeBucket, string> = {
  "deep-night":
    "radial-gradient(80% 140% at 25% 20%, oklch(0.55 0.15 260 / 0.11), transparent 65%), radial-gradient(60% 100% at 80% 0%, oklch(0.55 0.15 290 / 0.07), transparent 70%)",
  morning:
    "radial-gradient(80% 140% at 25% 20%, oklch(0.82 0.14 75 / 0.11), transparent 65%), radial-gradient(60% 100% at 80% 0%, oklch(0.78 0.14 35 / 0.08), transparent 70%)",
  noon: "radial-gradient(80% 140% at 25% 20%, oklch(0.84 0.12 75 / 0.10), transparent 65%), radial-gradient(60% 100% at 80% 0%, oklch(0.86 0.1 45 / 0.07), transparent 70%)",
  afternoon:
    "radial-gradient(80% 140% at 25% 20%, oklch(0.78 0.14 55 / 0.10), transparent 65%), radial-gradient(60% 100% at 80% 0%, oklch(0.72 0.13 25 / 0.07), transparent 70%)",
  evening:
    "radial-gradient(80% 140% at 25% 20%, oklch(0.68 0.16 25 / 0.11), transparent 65%), radial-gradient(60% 100% at 80% 0%, oklch(0.6 0.16 340 / 0.08), transparent 70%)",
  "late-night":
    "radial-gradient(80% 140% at 25% 20%, oklch(0.5 0.14 285 / 0.11), transparent 65%), radial-gradient(60% 100% at 80% 0%, oklch(0.55 0.14 265 / 0.08), transparent 70%)",
};

/**
 * Resolve the current time bucket for an aurora gradient lookup. Pure —
 * inputs are read-only, no side effects. Bucket boundaries: deep-night
 * before 5, morning before 12, noon before 14, afternoon before 18,
 * evening before 22, late-night otherwise.
 */
export function getHubHeaderTimeBucket(date: Date): HubHeaderTimeBucket {
  const hour = date.getHours();
  if (hour < 5) return "deep-night";
  if (hour < 12) return "morning";
  if (hour < 14) return "noon";
  if (hour < 18) return "afternoon";
  if (hour < 22) return "evening";
  return "late-night";
}
