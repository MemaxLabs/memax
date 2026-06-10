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

/** Collapsed left-rail width (default user preference). */
export const RAIL_WIDTH_COLLAPSED = 56;

/** Expanded left-rail width (after user click / hover-to-expand). */
export const RAIL_WIDTH_EXPANDED = 196;

/** Inset between the rail and the pinned secondary panel (and page edges). */
export const PANEL_INSET = 12;

/** Pinned secondary panel width (memories tab tree explorer). */
export const PANEL_WIDTH = 296;

/**
 * Compute the leftward chrome footprint that fixed-position content must
 * clear.
 *
 * The rail's LAYOUT width is always RAIL_WIDTH_COLLAPSED — hover-expansion
 * is a visual overlay that does NOT shove page content. Pinned mode was
 * removed (industry: VS Code / Slack workspace switcher pattern). The
 * `collapsed` arg is preserved on the type for back-compat but ignored
 * by the layout calc; callers should stop passing it.
 *
 * Secondary panel sits FLUSH against the rail's right edge (no insets
 * between rail and panel). Top/bottom + outer-right page insets stay,
 * because they're page-edge insets, not rail-relative.
 */
export function shellLeftOffsetPx({
  secondaryExpanded,
}: {
  collapsed?: boolean;
  secondaryExpanded: boolean;
}): number {
  const railFootprint = PANEL_INSET + RAIL_WIDTH_COLLAPSED;
  if (secondaryExpanded) {
    return railFootprint + PANEL_WIDTH + PANEL_INSET;
  }
  return railFootprint + PANEL_INSET;
}
