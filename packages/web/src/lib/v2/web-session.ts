import type { WebSession } from "./data/gates";

/**
 * What the frame knows about a session's assurance before it sends
 * anything (D15, internal/websurface): whether the session was issued to
 * the web app. The page never holds the token; the web app's server reads
 * its `surface` claim and says so in /api/auth/me (`session.surface`). The
 * API records a person on the web (human_web) only when that claim says
 * "web" and /api/proxy signed the request, so a session without it can't
 * answer a decision that needs the web. The proxy's secret is the
 * server's to know; a refusal still says so when it's missing.
 *
 * This only lets a screen say so before the person tries.
 */
export function webSessionOf(
  session: { surface: string | null } | null | undefined,
): WebSession {
  if (!session) return null;
  // No claim: a session from before migration 030, or a CLI login.
  return session.surface === "web";
}
