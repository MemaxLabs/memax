/**
 * Which OAuth redirect URIs Memax accepts (RFC 6749 §3.1.2, RFC 8252 for
 * native apps), read from the one description the server embeds
 * (packages/server/internal/oauthredirect/redirects.json): https anywhere;
 * http on a loopback host; a private-use scheme that is a reverse domain
 * name or a native agent's own (cursor://, vscode://); never a refused
 * scheme, userinfo or a fragment. The consent page follows the server's
 * answer only when it passes the same rules, so it never navigates to
 * javascript:, data: or the like, and a browser hands a native app's
 * scheme to the app.
 */
import rules from "../../../server/internal/oauthredirect/redirects.json";

const SCHEME = /^[a-z][a-z0-9+.-]*$/;
const LOOPBACK: ReadonlySet<string> = new Set(rules.loopback_hosts);
const REFUSED: ReadonlySet<string> = new Set(rules.refused_schemes);
const NATIVE: ReadonlySet<string> = new Set(Object.keys(rules.native_schemes));

/** Whether Memax accepts `uri` as a redirect, as the server decides it. */
export function acceptedRedirect(uri: string): boolean {
  if (/[#\s\\]/.test(uri)) return false;
  let url: URL;
  try {
    url = new URL(uri);
  } catch {
    return false;
  }
  if (url.username !== "" || url.password !== "") return false;
  const scheme = url.protocol.slice(0, -1).toLowerCase();
  if (!SCHEME.test(scheme) || REFUSED.has(scheme)) return false;
  if (scheme === "https") return url.hostname !== "";
  if (scheme === "http") {
    return LOOPBACK.has(url.hostname.replace(/^\[|\]$/g, "").toLowerCase());
  }
  if (NATIVE.has(scheme) || scheme.includes(".")) {
    // Something must follow "scheme:".
    return url.host !== "" || (url.pathname !== "" && url.pathname !== "/");
  }
  return false;
}

/** Whether a browser loads the page itself (https, loopback http) rather than handing it to an app. */
export function opensInBrowser(uri: string): boolean {
  return /^https?:/i.test(uri);
}
