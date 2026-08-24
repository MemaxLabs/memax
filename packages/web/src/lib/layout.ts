/**
 * Shared layout constants — single source of truth for positioning.
 *
 * Design system reference: docs/design/memax-design-system.md §13
 *
 * Do NOT hardcode these values in page files. Import from here.
 */

// ─── Vertical positioning ───

/** Desktop: header elements (logo, avatar, tabs) top offset */
export const HEADER_TOP = "72px";

/**
 * Desktop: page content top padding (below header + tabs).
 *
 * 48px — landed after 80→64→48→32→48 iteration. 32 was too tight (titles
 * crashed into the viewport edge); 80 was too generous. 48 leaves visible
 * breathing room above the title without the "lots of empty sky" feel
 * the original 80 had. Inbox + agents + topics all consume this.
 */
export const CONTENT_TOP = "48px";

/**
 * Numeric counterpart to CONTENT_TOP — used by IntersectionObserver
 * `rootMargin` and other JS that needs the value as a number.
 * Keep in lockstep with CONTENT_TOP above.
 */
export const CONTENT_TOP_PX = 48;

/** Mobile: header elements top offset (respects safe area) */
export const MOBILE_HEADER_TOP = "max(60px, calc(48px + var(--safe-top, 0px)))";

/**
 * Mobile: fixed app chrome top (logo + hub chip row).
 *
 * Tightened to `max(8px, 4+safe-top)` (was `max(20px, 8+safe-top)`).
 * On no-notch phones the chrome now sits at y=8 instead of y=20,
 * which pulls the page title up by 12px. Notched phones still respect
 * the safe-area inset (max() picks the larger value).
 */
export const MOBILE_CHROME_TOP = "max(8px, calc(4px + var(--safe-top, 0px)))";

/** Mobile: fixed app chrome row height (logo/hub/avatar bar) */
export const MOBILE_CHROME_ROW_HEIGHT = "48px";

/**
 * Mobile: page-content interior gap below the fixed chrome row.
 *
 * IMPORTANT — this is the ONLY top padding `<PageShell>` adds on mobile.
 * The mobile shell's `<main>` already pads itself by chrome height +
 * safe-area inset (see shell-layout-mobile.tsx), so PageShell adding the
 * full chrome offset on top would double-count and shove content far
 * below the chrome. PageShell uses this constant alone on mobile.
 *
 * 16px — landed after 16→12→8→4→16 iteration. 4 was too tight (titles
 * felt glued to the chrome's border); 16 gives clean visual separation
 * without the original 16's compound-with-chrome bloat (now that the
 * doubling is fixed, this is the gap, not the offset).
 */
export const MOBILE_CHROME_CONTENT_GAP = "16px";

/**
 * Mobile: full chrome offset.
 *
 * NOT for content rendered inside `<ShellLayoutMobile>` `<main>` — that
 * `<main>` already pads itself to clear the chrome (chrome row height +
 * safe-area inset), so adding this on top double-counts. Pages inside
 * the mobile shell should use `<PageShell>` (which adds only
 * `MOBILE_CHROME_CONTENT_GAP`).
 *
 * Use this only for fixed full-viewport surfaces that render OUTSIDE the
 * shell main and therefore have to clear the chrome themselves — today
 * that's just the mobile bar surface + compose overlay (both takeover-
 * style).
 */
export const MOBILE_CONTENT_TOP = `calc(${MOBILE_CHROME_TOP} + ${MOBILE_CHROME_ROW_HEIGHT} + ${MOBILE_CHROME_CONTENT_GAP})`;

/**
 * Mobile compose/mirror overlay content inset.
 * Keeps overlay headings comfortably below fixed logo + hub chip chrome.
 */
export const MOBILE_OVERLAY_CONTENT_TOP = `calc(${MOBILE_CONTENT_TOP} + 12px)`;

// ─── Horizontal sizing ───

/** Tabs/header container max-width: max-w-4xl (56rem) + px-8 padding (64px) */
export const TABS_MAX_WIDTH = "calc(56rem + 64px)";

// Desktop tree/panel geometry now lives in `src/lib/shell-geometry.ts`
// (RAIL_WIDTH / PANEL_WIDTH / PANEL_INSET / shellLeftOffsetPx). The v1
// constants that used to sit here (TREE_PANEL_INSET/WIDTH, TREE_SIDEBAR_W,
// TREE_PANEL_CONTENT_OFFSET, desktopLeftWidth) had no remaining code
// consumers — two sources of truth for the same 296px was exactly how the
// rail and panel drifted apart — so they were deleted rather than kept in
// sync (2026-08).

// ─── Bar height ───

/** Bar total height including padding (for centering calculations) */
export const BAR_HEIGHT = "80px";

/** Mobile bar height */
export const MOBILE_BAR_HEIGHT = "72px";

/**
 * Mobile dock height — bottom tab bar height including padding.
 * Icon (~22px) + label (~11px) + vertical rhythm (pt-1.5 + pb-1.5)
 * plus safe-area-inset-bottom applied inline. Used by surfaces that
 * stack above the dock (batch toolbar) to reserve space so they
 * don't overlap it on mobile non-brain routes where the bar is
 * hidden but the dock is always present.
 */
export const MOBILE_DOCK_HEIGHT = "56px";

// ─── Bar dock anchor gaps ───

/**
 * Desktop dock gap — bar's bottom edge sits this far above the
 * viewport bottom in its resting docked state (`/memories`, empty
 * view). Used both to compute the `top` anchor and to compose the
 * scroll-hide translate so the bar slides fully off-screen.
 */
export const DESKTOP_DOCK_BOTTOM_GAP_PX = 32;

/**
 * Mobile dock gap — bar's bottom edge sits this far above the
 * viewport bottom (before safe-area inset). Clears the 56px mobile
 * dock and leaves a 12px breathing gap, matching iOS bottom-docked
 * compose bars (Comet, ChatGPT, Perplexity). The actual `top`
 * expression also subtracts `--safe-bottom` on top of this gap.
 */
export const MOBILE_DOCK_BOTTOM_GAP_PX = 68;

// ─── Bar anchor positions ───

/** Desktop brain-view resting anchor for the global bar */
export const BRAIN_BAR_REST_TOP = "42vh";

/** Desktop engaged anchor for the global bar across routes */
export const BAR_ENGAGED_TOP = "24vh";

/** Vertical gap from brain resting anchor to the below-bar memory badge */
export const BRAIN_BAR_BADGE_OFFSET = "44px";

/**
 * Mobile brain-view resting anchor for the "✦ N memories ›" badge.
 * Sits in the upper-middle third so the badge reads as content above
 * the bottom-docked bar + dock without fighting either. Desktop math
 * (`BRAIN_BAR_REST_TOP + BADGE_OFFSET`) does not apply because the
 * mobile bar lives at the bottom, not at 42vh.
 */
export const MOBILE_BRAIN_BADGE_TOP = "38%";

// ─── Empty state centering ───

/**
 * Height for centering content in the available viewport space.
 * Subtracts header top offset + bar height so content is visually centered
 * between the header and the bottom bar. Use as min-height on a flex container.
 *
 * Industry pattern: Notion, Linear — empty states fill available space.
 */
export const CENTERED_HEIGHT = `calc(100dvh - ${CONTENT_TOP} - ${BAR_HEIGHT})`;
export const MOBILE_CENTERED_HEIGHT = `calc(100dvh - ${MOBILE_CONTENT_TOP} - ${MOBILE_BAR_HEIGHT})`;

// ─── Tailwind class patterns (documented, not extracted as JS) ───
// Content width:    max-w-4xl (896px)
// Outer padding:    px-5 sm:px-8
// Bottom clearance: pb-36 md:pb-32
// Inner padding:    px-5 sm:px-8 md:px-10
