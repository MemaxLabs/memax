/**
 * Before the BFF, the web app kept its session tokens in localStorage,
 * where any script on the page, or a local agent reading the browser's
 * profile on disk, could take them. They now live only in HttpOnly
 * cookies the web app's server reads (lib/bff). This deletes what an
 * older version left behind, without ever reading it: a browser that had
 * a session from then signs in again once, and that old session expires
 * on the API within 30 days (or is signed out from Settings).
 *
 * The only place these keys appear (bundle-scan.test.ts holds it so).
 */
const LEGACY_TOKEN_KEYS = [
  "memax_access_token",
  "memax_refresh_token",
  "memax_original_access_token",
  "memax_original_refresh_token",
];

export function forgetLegacyTokens(): void {
  if (typeof window === "undefined") return;
  try {
    for (const key of LEGACY_TOKEN_KEYS) window.localStorage.removeItem(key);
  } catch {
    // Storage unavailable (private mode, blocked): nothing to forget.
  }
}
