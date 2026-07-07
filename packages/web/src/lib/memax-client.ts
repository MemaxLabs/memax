"use client";

import { Memax } from "memax-sdk";
import { getAccessToken } from "@/lib/auth";
import {
  clearSessionPresence,
  markSessionPresence,
} from "@/lib/session-presence";
import { API_URL } from "@/lib/urls";

const BROWSER_API_PROXY_URL = "/api/proxy";
const TOKEN_KEY = "memax_access_token";
const REFRESH_KEY = "memax_refresh_token";

let authedClient: Memax | null = null;
let publicClient: Memax | null = null;
let refreshPromise: Promise<string | null> | null = null;

interface AuthTokens {
  access_token?: string;
  refresh_token?: string;
}

function notifyAuthExpired() {
  if (typeof window === "undefined") return;
  window.dispatchEvent(new CustomEvent("memax:auth-expired"));
}

function getBrowserSafeAPIURL(): string {
  return typeof window === "undefined" ? API_URL : BROWSER_API_PROXY_URL;
}

export function createMemaxClientWithToken(token: string): Memax {
  return new Memax({
    apiUrl: getBrowserSafeAPIURL(),
    auth: async (): Promise<Record<string, string>> => ({
      Authorization: `Bearer ${token}`,
    }),
  });
}

function getRefreshToken(): string | null {
  if (typeof window === "undefined") return null;
  return localStorage.getItem(REFRESH_KEY);
}

function setStoredTokens(accessToken: string, refreshToken: string) {
  localStorage.setItem(TOKEN_KEY, accessToken);
  localStorage.setItem(REFRESH_KEY, refreshToken);
  markSessionPresence();
}

function clearStoredTokens() {
  localStorage.removeItem(TOKEN_KEY);
  localStorage.removeItem(REFRESH_KEY);
  clearSessionPresence();
}

async function refreshAccessToken(): Promise<string | null> {
  if (typeof window === "undefined") return null;
  if (refreshPromise) return refreshPromise;

  const refreshToken = getRefreshToken();
  if (!refreshToken) {
    clearStoredTokens();
    notifyAuthExpired();
    return null;
  }

  refreshPromise = (async () => {
    try {
      const res = await fetch("/api/auth/refresh", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ refresh_token: refreshToken }),
      });
      const payload = (await res.json()) as {
        data?: AuthTokens;
        error?: { code?: string };
      };
      const tokens = payload.data;
      const nextAccessToken = tokens?.access_token;
      const nextRefreshToken = tokens?.refresh_token;
      if (!nextAccessToken || !nextRefreshToken) {
        if (
          res.status === 401 ||
          payload.error?.code === "invalid_token" ||
          payload.error?.code === "expired_token" ||
          payload.error?.code === "unauthorized"
        ) {
          clearStoredTokens();
          notifyAuthExpired();
        }
        return null;
      }

      setStoredTokens(nextAccessToken, nextRefreshToken);
      return nextAccessToken;
    } catch {
      return null;
    } finally {
      refreshPromise = null;
    }
  })();

  return refreshPromise;
}

function shouldAttemptRefresh(input: RequestInfo | URL): boolean {
  const url =
    typeof input === "string"
      ? input
      : input instanceof URL
        ? input.toString()
        : input.url;
  return !url.includes("auth/refresh") && !url.includes("auth/exchange");
}

async function authFetch(
  input: RequestInfo | URL,
  init?: RequestInit,
): Promise<Response> {
  const res = await fetch(input, init);
  if (res.status !== 401 || !shouldAttemptRefresh(input)) {
    return res;
  }

  const nextAccessToken = await refreshAccessToken();
  if (!nextAccessToken) {
    return res;
  }

  const headers = new Headers(init?.headers ?? undefined);
  headers.set("Authorization", `Bearer ${nextAccessToken}`);

  const retried = await fetch(input, {
    ...init,
    headers,
  });

  if (retried.status === 401) {
    clearStoredTokens();
    notifyAuthExpired();
  }

  return retried;
}

function createAuthedClient(): Memax {
  return new Memax({
    apiUrl: getBrowserSafeAPIURL(),
    fetch: authFetch,
    auth: async (): Promise<Record<string, string>> => {
      const headers: Record<string, string> = {};
      const token = getAccessToken();
      if (token) {
        headers.Authorization = `Bearer ${token}`;
      }
      return headers;
    },
  });
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
