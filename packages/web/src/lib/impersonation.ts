/**
 * Impersonation for dev users: acting as another person to debug as them.
 *
 * The web app's server holds every token (lib/bff): /api/auth/impersonate
 * swaps the impersonation token into the session cookie and keeps the
 * dev's own session aside, HttpOnly, until they stop. The page sees only
 * the readable `memax_impersonating` marker, which holds the dev's name
 * for the banner and no secret.
 */

const IMPERSONATING_COOKIE = "memax_impersonating";

function marker(): string | null {
  if (typeof document === "undefined") return null;
  for (const part of document.cookie.split(";")) {
    const [name, ...rest] = part.trim().split("=");
    if (name === IMPERSONATING_COOKIE) {
      try {
        return decodeURIComponent(rest.join("="));
      } catch {
        return rest.join("=");
      }
    }
  }
  return null;
}

export function isImpersonating(): boolean {
  return marker() !== null;
}

export function getOriginalUserName(): string | null {
  return marker();
}

/**
 * Start impersonating someone (by user id or email). Impersonation tokens
 * are access-only and expire in an hour. Nested impersonation is refused.
 * Reloads into the impersonated session; throws with the server's message
 * when it was refused.
 */
export async function startImpersonating(
  currentUserName: string,
  target: { user_id?: string; email?: string },
): Promise<void> {
  if (isImpersonating()) {
    throw new Error("Already impersonating. Stop the current session first.");
  }
  const res = await fetch("/api/auth/impersonate", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ ...target, original_name: currentUserName }),
  });
  if (!res.ok) {
    const json = (await res.json().catch(() => null)) as {
      error?: { message?: string };
    } | null;
    throw new Error(json?.error?.message ?? "Impersonation failed.");
  }
  // Full reload to re-initialize auth context with the new identity.
  window.location.reload();
}

/**
 * Stop impersonating: the dev's own session comes back. Resolves once the
 * server swapped it back; `reload` (default) reloads into it.
 */
export async function stopImpersonating(reload = true): Promise<void> {
  await fetch("/api/auth/impersonate", { method: "DELETE" }).catch(() => {});
  if (reload) window.location.reload();
}
