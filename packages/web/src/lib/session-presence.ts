/**
 * Session-presence cookie — a server-visible "this browser has a
 * session" marker for the auth tokens that live in localStorage.
 *
 * The tokens themselves never leave localStorage; this cookie carries
 * no secret (literal value "1"). Its only job is to let the Edge
 * middleware route a previously-signed-in user straight into the app
 * from `/`, `/login`, and `/register` — silent session restore with
 * zero marketing-page or login-form flash. Without a server-visible
 * signal, those routes must always render the signed-out experience
 * first and correct themselves client-side after auth init (a full
 * reload + loader chain the user reads as "I had to sign in again").
 *
 * Lifecycle: set whenever tokens are persisted or a session is
 * verified (login, token refresh, successful /me), cleared whenever
 * the session is torn down. If the cookie is ever stale (present with
 * dead tokens), the app shell's auth init fails, clears it, and lands
 * on /login — one extra hop, no loop. If it is missing with live
 * tokens (pre-migration browsers), the client-side redirect fallback
 * still works and the next refresh/verify re-plants it.
 */

export const SESSION_PRESENCE_COOKIE = "memax_session_presence";

// ~400 days — matches the practical ceiling browsers apply to
// cookie lifetimes (Chrome caps at 400 days). The cookie re-plants on
// every token refresh, so real sessions never age it out.
const MAX_AGE_SECONDS = 400 * 24 * 60 * 60;

export function markSessionPresence() {
  if (typeof document === "undefined") return;
  const secure = window.location.protocol === "https:" ? "; secure" : "";
  document.cookie = `${SESSION_PRESENCE_COOKIE}=1; path=/; max-age=${MAX_AGE_SECONDS}; samesite=lax${secure}`;
}

export function clearSessionPresence() {
  if (typeof document === "undefined") return;
  document.cookie = `${SESSION_PRESENCE_COOKIE}=; path=/; max-age=0; samesite=lax`;
}
