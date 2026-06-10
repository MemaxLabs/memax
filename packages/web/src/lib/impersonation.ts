/**
 * Impersonation helpers for dev users.
 *
 * When a dev impersonates another user, the original tokens are stashed in
 * localStorage under `memax_original_*` keys. The impersonated user's tokens
 * replace the normal `memax_access_token` / `memax_refresh_token`.
 *
 * The app detects impersonation by checking for the presence of the stashed
 * original tokens and shows a persistent banner.
 */

const TOKEN_KEY = "memax_access_token";
const REFRESH_KEY = "memax_refresh_token";
const ORIGINAL_TOKEN_KEY = "memax_original_access_token";
const ORIGINAL_REFRESH_KEY = "memax_original_refresh_token";
const ORIGINAL_USER_NAME_KEY = "memax_original_user_name";

export function isImpersonating(): boolean {
  if (typeof window === "undefined") return false;
  return localStorage.getItem(ORIGINAL_TOKEN_KEY) !== null;
}

export function getOriginalUserName(): string | null {
  if (typeof window === "undefined") return null;
  return localStorage.getItem(ORIGINAL_USER_NAME_KEY);
}

/**
 * Start impersonating: stash current tokens, install impersonated access token.
 * Impersonation tokens are access-only (no refresh) and expire in 1 hour.
 * Blocks nested impersonation — must stop current session first.
 */
export function startImpersonating(
  currentUserName: string,
  targetAccessToken: string,
) {
  // Block nested impersonation — preserve the real dev's original tokens
  if (isImpersonating()) {
    throw new Error("Already impersonating. Stop the current session first.");
  }

  // Stash original tokens
  const currentAccess = localStorage.getItem(TOKEN_KEY);
  const currentRefresh = localStorage.getItem(REFRESH_KEY);
  if (currentAccess) localStorage.setItem(ORIGINAL_TOKEN_KEY, currentAccess);
  if (currentRefresh)
    localStorage.setItem(ORIGINAL_REFRESH_KEY, currentRefresh);
  localStorage.setItem(ORIGINAL_USER_NAME_KEY, currentUserName);

  // Install impersonated access token (no refresh — session expires in 1h)
  localStorage.setItem(TOKEN_KEY, targetAccessToken);
  localStorage.removeItem(REFRESH_KEY);

  // Full reload to re-initialize auth context with new identity
  window.location.reload();
}

/**
 * Stop impersonating: restore original tokens and reload.
 */
export function stopImpersonating() {
  const originalAccess = localStorage.getItem(ORIGINAL_TOKEN_KEY);
  const originalRefresh = localStorage.getItem(ORIGINAL_REFRESH_KEY);

  // Restore original tokens
  if (originalAccess) localStorage.setItem(TOKEN_KEY, originalAccess);
  if (originalRefresh) localStorage.setItem(REFRESH_KEY, originalRefresh);

  // Clean up stash
  clearImpersonationState();

  // Full reload to re-initialize auth context
  window.location.reload();
}

/**
 * Restore original dev tokens without reloading.
 * Used by auth init when the impersonation token expires — the auth context
 * will re-fetch with the restored tokens instead of logging the dev out.
 * Returns true if original tokens were restored, false if there was nothing to restore.
 */
export function restoreOriginalSession(): boolean {
  const originalAccess = localStorage.getItem(ORIGINAL_TOKEN_KEY);
  if (!originalAccess) return false;

  const originalRefresh = localStorage.getItem(ORIGINAL_REFRESH_KEY);
  localStorage.setItem(TOKEN_KEY, originalAccess);
  if (originalRefresh) localStorage.setItem(REFRESH_KEY, originalRefresh);

  clearImpersonationState();
  return true;
}

/**
 * Clear all impersonation localStorage keys.
 * Called by logout to prevent stale original tokens from surviving.
 */
export function clearImpersonationState() {
  localStorage.removeItem(ORIGINAL_TOKEN_KEY);
  localStorage.removeItem(ORIGINAL_REFRESH_KEY);
  localStorage.removeItem(ORIGINAL_USER_NAME_KEY);
}
