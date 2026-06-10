// Motion tokens — single source of truth for all animation durations and easings.
// Use these instead of hardcoded values in Framer Motion transitions.

/** UI state changes: intent label, right slot, tooltip */
export const INSTANT = 0.1;

/** Modal open/close, expand slot, view switch, dropdown */
export const FAST = 0.15;

/** Dissolve, card appear/disappear, dropdown */
export const NORMAL = 0.2;

/** Loading pulse cycle, breathing dots */
export const LOADING = 0.35;

/** @deprecated Placeholder cycling removed — brain view uses stable text */
export const SIGNATURE_INTERVAL = 3500;

/** Remember result auto-clear (ms) */
export const REMEMBER_CLEAR = 2000;

/** Forget countdown (ms) */
export const FORGET_COUNTDOWN = 5000;

/** Dissolve wait before delete (ms) */
export const DISSOLVE_WAIT = 250;

/** Card highlight after modal close (ms) */
export const HIGHLIGHT_DURATION = 1000;

/** Content ready transition — loading→loaded (CSS class: animate-content-ready) */
export const CONTENT_READY = 0.15;

/** Spring easing — used for position changes */
export const EASE = [0.16, 1, 0.3, 1] as const;

// ─── Glass surface motion ───
// Used by the desktop floating tree panel and the mobile centered tree
// modal. Shared grammar keeps the two surfaces reading as one material
// family. Durations diverge slightly: desktop overshoots a hair to feel
// like an iOS 18 sheet presentation; mobile is tuned a touch faster so
// it reads as "direct" on touch.

/** Glass surface enter duration — desktop. */
export const GLASS_ENTER_DURATION = 0.38;
/** Glass surface enter duration — mobile centered modal. */
export const GLASS_ENTER_DURATION_MOBILE = 0.32;
/** Glass surface exit duration — both surfaces. */
export const GLASS_EXIT_DURATION = 0.22;
/** Backdrop fade — narrow-viewport desktop + mobile modal. */
export const GLASS_BACKDROP_DURATION = 0.2;

/** Decelerate-with-overshoot; matches iOS 18 sheet / macOS Tahoe sidebar. */
export const GLASS_ENTER_EASE = [0.22, 1, 0.36, 1] as const;
/** Deliberate exit, no overshoot. */
export const GLASS_EXIT_EASE = [0.4, 0, 0.2, 1] as const;
