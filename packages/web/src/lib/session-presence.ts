/**
 * Session-presence cookie — a readable "this browser has a session"
 * marker beside the session itself, which lives in HttpOnly cookies only
 * the web app's server reads (lib/bff/cookies.ts).
 *
 * This cookie carries no secret (literal value "1"). Its only job is to
 * let the middleware and the server-rendered layouts route a signed-in
 * user straight into the app from `/`, `/login`, and `/register` —
 * silent session restore with zero marketing-page or login-form flash.
 * It is SameSite=Lax, so a link into the app from another site still
 * routes; the session cookies are SameSite=Strict and travel only with
 * the app's own requests.
 *
 * Lifecycle: the web app's server sets it with the session (sign-in,
 * refresh, a verified /api/auth/me) and clears it with the session
 * (sign-out, a refused refresh). The page clears it too when it learns
 * the session is gone or the API can't be reached, so a stale marker
 * costs one extra hop, never a loop.
 */

export const SESSION_PRESENCE_COOKIE = "memax_session_presence";

export function clearSessionPresence() {
  if (typeof document === "undefined") return;
  const secure = window.location.protocol === "https:" ? "; secure" : "";
  document.cookie = `${SESSION_PRESENCE_COOKIE}=; path=/; max-age=0; samesite=lax${secure}`;
}
