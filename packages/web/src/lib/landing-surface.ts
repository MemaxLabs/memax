/**
 * Landing-surface hint — remembers which surface the signed-in user
 * should land on (`/brain` for activated users, the personal memories
 * overview for everyone else) so the `/home` entry resolver can route
 * in a single hop on cold entry.
 *
 * Why this exists: activation state lives server-side (onboarding
 * checklist materializer) and takes a network round-trip to resolve.
 * Without a local hint, every cold entry would either (a) wait on the
 * query before showing anything, or (b) guess a surface and visibly
 * flick to the other one — the "bar renders then flicks into memory
 * view" defect. The hint makes the common case (returning user)
 * instant; `useOnboardingActivation` refreshes it whenever the real
 * signal resolves, so a stale hint self-corrects on the next entry.
 *
 * This is a routing hint, not an auth artifact — it never gates
 * access, it only picks between two authed surfaces. It is cleared in
 * `clearSession` alongside the token keys so a different account on
 * the same browser starts neutral.
 */

export type LandingSurface = "brain" | "memories";

const LANDING_SURFACE_KEY = "memax_landing_surface";

export function getLandingSurfaceHint(): LandingSurface | null {
  if (typeof window === "undefined") return null;
  try {
    const value = localStorage.getItem(LANDING_SURFACE_KEY);
    return value === "brain" || value === "memories" ? value : null;
  } catch {
    return null;
  }
}

export function setLandingSurfaceHint(surface: LandingSurface) {
  if (typeof window === "undefined") return;
  try {
    localStorage.setItem(LANDING_SURFACE_KEY, surface);
  } catch {}
}

export function clearLandingSurfaceHint() {
  if (typeof window === "undefined") return;
  try {
    localStorage.removeItem(LANDING_SURFACE_KEY);
  } catch {}
}

/**
 * Path for a landing surface. Memories lands on the ACTIVE hub when
 * the caller can resolve its slug — hard-coding personal here would
 * silently revert a team-hub selection on every /home entry (brand
 * link, error-page home buttons), the same revert-to-personal bug the
 * hub switcher fixes elsewhere. `null`/absent slug falls back to
 * personal (every user has one).
 */
export function landingPathFor(
  surface: LandingSurface,
  hubSlug?: string | null,
): string {
  return surface === "brain"
    ? "/brain"
    : `/h/${hubSlug || "personal"}/memories`;
}
