// Minimal transport for the internal admin client. Mirrors the
// request/response shape of lib/api.ts (ApiError envelope, retry-after
// behavior) so admin calls and user-facing calls fail the same way in
// the UI. Kept in-package so the admin client never imports from the
// public SDK transport.
//
// Like every browser call, admin calls go through the web app's server
// (/api/proxy), which holds the session in HttpOnly cookies and attaches
// its token (lib/bff); the page has no token to send.

import { ApiError } from "@/lib/api";
import { API_URL } from "@/lib/urls";

/** Where admin calls go: the proxy in the browser, the API on the server. */
export const ADMIN_API_BASE =
  typeof window === "undefined" ? API_URL : "/api/proxy";

type Method = "GET" | "POST" | "PUT" | "PATCH" | "DELETE";

interface Envelope {
  data?: unknown;
  error?: { code: string; message: string; details?: Record<string, unknown> };
}

/**
 * adminReq is the single request helper for every admin endpoint.
 * Returns parsed `data` on 2xx; throws ApiError on everything else.
 *
 * Why not reuse apiGet/apiPost/apiPatch from lib/api.ts? Two reasons:
 *   1. apiDelete returns void, but some admin DELETEs (e.g. revoking
 *      a plan override) return a {status} body. We need a uniform
 *      parsed-body contract across all verbs.
 *   2. Admin requests never want the X-Hub-ID write-context header
 *      injected automatically. They're global-scope operations, and
 *      the server's admin middleware ignores the header anyway.
 *      Having a dedicated helper avoids accidental hub scoping.
 */
export async function adminReq<T>(
  method: Method,
  path: string,
  body?: unknown,
): Promise<T> {
  const headers: Record<string, string> = {};
  const init: RequestInit = { method, headers };
  if (body !== undefined) {
    headers["Content-Type"] = "application/json";
    init.body = JSON.stringify(body);
  }
  if (method === "GET") {
    init.cache = "no-store";
  }

  const res = await fetch(`${ADMIN_API_BASE}${path}`, init);
  const text = res.status === 204 ? "" : await res.text();
  const parsed: Envelope = text ? safeParse(text) : {};

  if (!res.ok) {
    const err = parsed.error ?? {
      code: `http_${res.status}`,
      message: `Request failed with status ${res.status}`,
    };
    throw new ApiError(err.message, err.code, res.status, err.details);
  }
  return (parsed.data as T) ?? (undefined as T);
}

function safeParse(text: string): Envelope {
  try {
    return JSON.parse(text) as Envelope;
  } catch {
    return {};
  }
}
