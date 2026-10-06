import type { WebSession } from "./data/gates";

/**
 * What the frame knows about a session's assurance before it sends
 * anything (D15, internal/websurface): whether the bearer token was
 * issued to the web app, from its `surface` claim. The API records a
 * person on the web (human_web) only when that claim says "web" and
 * /api/proxy signed the request, so a token without it can't answer a
 * decision that needs the web. The proxy's secret is the server's to
 * know; a refusal still says so when it's missing.
 *
 * The token isn't verified here (the API verifies it); this only lets a
 * screen say so before the person tries.
 */
export function webSessionOf(token: string | null | undefined): WebSession {
  if (!token) return null;
  // An API key is never a person on the web.
  if (token.startsWith("mxk_")) return false;
  const parts = token.split(".");
  if (parts.length !== 3 || !parts[1]) return null;
  try {
    const base64 = parts[1].replace(/-/g, "+").replace(/_/g, "/");
    const padded = base64.padEnd(Math.ceil(base64.length / 4) * 4, "=");
    const claims: unknown = JSON.parse(atob(padded));
    if (!claims || typeof claims !== "object") return null;
    // No claim: a session from before migration 030, or a CLI login.
    return (claims as { surface?: unknown }).surface === "web";
  } catch {
    return null;
  }
}
