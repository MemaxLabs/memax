import { createHmac } from "node:crypto";

/**
 * Where the person's browser is, for the sessions list (Settings ›
 * Account). The API sees the web app's server, not the browser, so when
 * the BFF signs someone in or refreshes their session it says where the
 * browser is, signed with WEB_SURFACE_SECRET (websurface.ClientInfo in the
 * API, which ignores these headers unsigned). They are informational and
 * never decide what a request may do.
 *
 * The address and city are the edge's: Cloudflare's CF-Connecting-IP and
 * CF-IPCity on Workers, Vercel's X-Real-IP and X-Vercel-IP-City. Behind
 * neither (self-hosted `next start`), the first X-Forwarded-For hop.
 */
export function clientInfoHeaders(
  req: Request,
  secret: string,
  now = Date.now(),
): Record<string, string> {
  if (!secret) return {};
  const h = req.headers;
  const ip = (
    h.get("cf-connecting-ip") ??
    h.get("x-real-ip") ??
    h.get("x-forwarded-for")?.split(",")[0] ??
    ""
  )
    .trim()
    .slice(0, 64);
  const rawCity = h.get("cf-ipcity") ?? h.get("x-vercel-ip-city") ?? "";
  let city = rawCity;
  try {
    city = decodeURIComponent(rawCity);
  } catch {
    // Already plain.
  }
  const cityHeader = encodeURIComponent(city.slice(0, 100));
  // Printable ASCII only: a header value can't carry the rest.
  const ua = (h.get("user-agent") ?? "")
    .replace(/[^\x20-\x7e]/g, "")
    .slice(0, 512);
  const ts = Math.floor(now / 1000).toString();
  const signature = createHmac("sha256", secret)
    .update(["memax-web-client/v1", ts, ip, cityHeader, ua].join("\n"), "utf8")
    .digest("base64url");
  return {
    "x-memax-client-ip": ip,
    "x-memax-client-city": cityHeader,
    "x-memax-client-user-agent": ua,
    "x-memax-client-timestamp": ts,
    "x-memax-client-signature": `v1=${signature}`,
  };
}
