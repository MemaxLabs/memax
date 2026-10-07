"use client";

import { Memax } from "memax-sdk";
import { API_URL } from "@/lib/urls";
import {
  PASSKEY_SUGGESTED_EVENT,
  answerPasskeyCheck,
  suggestsPasskey,
} from "@/lib/v2/passkeys/check";

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

/**
 * A /v2 change that needed a person and went through without a passkey
 * says so in its policy (`suggest: "passkey"`); the frame offers to add
 * one. Read from a copy, after the answer is on its way to the caller.
 */
function watchForSuggestion(
  input: RequestInfo | URL,
  init: RequestInit | undefined,
  res: Response,
) {
  if (typeof window === "undefined" || !res.ok) return;
  const method = (init?.method ?? "GET").toUpperCase();
  if (method === "GET" || !String(input).includes("/v2/")) return;
  void res
    .clone()
    .json()
    .then((body: unknown) => {
      if (suggestsPasskey(body)) {
        window.dispatchEvent(new CustomEvent(PASSKEY_SUGGESTED_EVENT));
      }
    })
    .catch(() => {});
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
  watchForSuggestion(input, init, res);
  return res;
}

function createAuthedClient(): Memax {
  return new Memax({
    apiUrl: getBrowserSafeAPIURL(),
    fetch: authFetch,
    // The passkey re-check (lib/v2/passkeys/check.ts): a decision that
    // asks for the person's passkey is answered and sent again.
    passkeyCheck: answerPasskeyCheck,
  });
}

function getBrowserSafeAPIURL(): string {
  return typeof window === "undefined" ? API_URL : BROWSER_API_PROXY_URL;
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
