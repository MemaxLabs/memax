/**
 * Shell-v2 chrome geometry — single source of truth.
 *
 * Pre-Phase-1 these values lived duplicated in three places (left-rail,
 * pinned-secondary-panel, shell-layout-desktop). Extracted here so any
 * future tweak (rail bump from 56→60, panel width tune) updates all
 * fixed-position siblings together — including page-level surfaces like
 * the memory-detail sticky header that need to clear the chrome.
 *
 * Numeric (not CSS string) so callers can do arithmetic for `left:`
 * computations without parsing.
 */

/**
 * Left-rail width. The rail is always expanded (2026-08) — it used to
 * shrink to an icon column whenever a secondary panel opened, which
 * meant primary navigation rearranged itself as a side effect of
 * looking at something else. One width now, and it is a real layout
 * footprint rather than an overlay.
 */
export const RAIL_WIDTH = 196;

/** Inset between the rail and the pinned secondary panel (and page edges). */
export const PANEL_INSET = 12;

/**
 * Pinned secondary panel width (memories tab tree explorer). Narrowed
 * from 296 with the always-expanded rail: the rail now claims a real
 * 196px of layout, so the pair has to stay within a sane share of the
 * viewport, and a topic tree reads fine at this width.
 */
export const PANEL_WIDTH = 260;

/**
 * Compute the leftward chrome footprint that fixed-position content must
 * clear.
 *
 * The rail is a real layout footprint now, not an overlay: it is always
 * RAIL_WIDTH wide, so content clears it instead of being covered by it.
 *
 * Secondary panel sits FLUSH against the rail's right edge (no insets
 * between rail and panel). Top/bottom + outer-right page insets stay,
 * because they're page-edge insets, not rail-relative.
 */
export function shellLeftOffsetPx({
  secondaryExpanded,
}: {
  secondaryExpanded: boolean;
}): number {
  const railFootprint = PANEL_INSET + RAIL_WIDTH;
  if (secondaryExpanded) {
    return railFootprint + PANEL_WIDTH + PANEL_INSET;
  }
  return railFootprint + PANEL_INSET;
}
