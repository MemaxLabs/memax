/**
 * What an access token says about itself, read without verifying it: the
 * BFF uses it to decide when to refresh and whether a session is the web
 * app's. The API verifies every token it is sent.
 */
export interface TokenClaims {
  sub?: string;
  exp?: number;
  surface?: string;
  sid?: string;
  impersonator_id?: string;
}

export function decodeClaims(token: string | undefined): TokenClaims | null {
  if (!token || token.startsWith("mxk_")) return null;
  const parts = token.split(".");
  if (parts.length !== 3 || !parts[1]) return null;
  try {
    const claims: unknown = JSON.parse(
      Buffer.from(parts[1], "base64url").toString("utf8"),
    );
    return claims && typeof claims === "object"
      ? (claims as TokenClaims)
      : null;
  } catch {
    return null;
  }
}

/** Whether the token is still good for at least `marginSeconds`. */
export function usable(
  token: string | undefined,
  now = Date.now(),
  marginSeconds = 30,
): boolean {
  const exp = decodeClaims(token)?.exp;
  return typeof exp === "number" && exp * 1000 > now + marginSeconds * 1000;
}

/**
 * The user id of a session issued to the web app, or null for anything
 * else (CLI sessions, sessions from before surfaces, impersonation).
 */
export function webSessionUser(token: string | undefined): string | null {
  const c = decodeClaims(token);
  if (!c || c.surface !== "web" || c.impersonator_id) return null;
  return typeof c.sub === "string" && c.sub !== "" ? c.sub : null;
}
