// Control tokens — active/inactive state colors for toggles, radios, and
// other selection controls used across Settings, Teams, and every surface
// that isn't the Intelligence tab.
//
// Design rule:
//   - Purple (var(--signature)) is RESERVED for the Intelligence tab and
//     AI-adjacent features (dreams, merge, organize, archive toggles).
//     Signature purple signals "this is AI behavior."
//   - Everywhere else uses these neutral tokens. There is ONE active
//     "ink" color — NEUTRAL_INK — that rules all neutral selection
//     states (toggle-on track, toggle-on border, radio ring, radio dot).
//     It resolves to var(--fg-1) = foreground at 0.9 opacity = Memax's
//     body-text ink weight. Visible, authoritative, but not pure black.
//
// Usage:
//   import {
//     NEUTRAL_INK, NEUTRAL_INK_INVERSE,
//     NEUTRAL_TRACK_OFF, NEUTRAL_BORDER_OFF, NEUTRAL_THUMB_OFF,
//   } from "@memaxlabs/ui/tokens/controls";
//
// For the Intelligence variant, use var(--signature) directly.

/** Active ink — the one-and-only color for neutral selection states.
 *  Toggle track when on, toggle border when on, radio ring when on,
 *  radio dot, any "the user picked this" indicator. Body-text weight. */
export const NEUTRAL_INK = "var(--fg-1)";

/** Contrast for elements sitting on top of NEUTRAL_INK fill — e.g. the
 *  toggle thumb when the track is filled with ink. */
export const NEUTRAL_INK_INVERSE = "var(--background)";

/** Toggle track background when OFF — barely visible idle presence. */
export const NEUTRAL_TRACK_OFF = "oklch(from var(--foreground) l c h / 0.08)";

/** Border for OFF-state controls — radio ring when unselected, toggle
 *  border when off. */
export const NEUTRAL_BORDER_OFF = "oklch(from var(--foreground) l c h / 0.18)";

/** Toggle thumb when OFF — muted so it reads as "idle knob." */
export const NEUTRAL_THUMB_OFF = "oklch(from var(--foreground) l c h / 0.40)";
