"use client";

import { Memax } from "memax-sdk";
import { API_URL } from "@/lib/urls";

// The browser talks to the API only through the web app's server
// (/api/proxy), which holds the session in HttpOnly cookies, attaches its
// token and refreshes it (lib/bff). The browser never has a token: the
// SDK here sends no Authorization header, and same-origin requests carry
// the cookies.
const BROWSER_API_PROXY_URL = "/api/proxy";

let authedClient: Memax | null = null;
let publicClient: Memax | null = null;

function notifyAuthExpired() {
  if (typeof window === "undefined") return;
  window.dispatchEvent(new CustomEvent("memax:auth-expired"));
}

function getBrowserSafeAPIURL(): string {
  return typeof window === "undefined" ? API_URL : BROWSER_API_PROXY_URL;
}

/**
 * A 401 through the proxy means the session is over: the proxy already
 * refreshed it if it could. The auth provider signs the page out.
 */
async function authFetch(
  input: RequestInfo | URL,
  init?: RequestInit,
): Promise<Response> {
  const res = await fetch(input, { credentials: "same-origin", ...init });
  if (res.status === 401) notifyAuthExpired();
  return res;
}

function createAuthedClient(): Memax {
  return new Memax({ apiUrl: getBrowserSafeAPIURL(), fetch: authFetch });
}

function createPublicClient(): Memax {
  return new Memax({ apiUrl: getBrowserSafeAPIURL() });
}

export function getMemaxClient(): Memax {
  if (!authedClient) {
    authedClient = createAuthedClient();
  }
  return authedClient;
}

export function getPublicMemaxClient(): Memax {
  if (!publicClient) {
    publicClient = createPublicClient();
  }
  return publicClient;
}
