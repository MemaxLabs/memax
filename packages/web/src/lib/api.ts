import { getAccessToken } from "./auth";
import { API_URL } from "./urls";

const BROWSER_API_PROXY_URL = "/api/proxy";
const BROWSER_SAFE_API_URL =
  typeof window === "undefined" ? API_URL : BROWSER_API_PROXY_URL;
const MAX_RETRIES = 2;
const RETRY_DELAY_MS = 1500;

interface ApiEnvelope {
  data?: unknown;
  error?: { code: string; message: string; details?: Record<string, unknown> };
}

export class ApiError extends Error {
  constructor(
    message: string,
    public code: string,
    public status: number,
    public details?: Record<string, unknown>,
  ) {
    super(message);
    this.name = "ApiError";
  }

  /** True when the error is a quota limit (402). */
  get isQuotaExceeded(): boolean {
    return this.code === "quota_exceeded";
  }
}

function authHeaders(): Record<string, string> {
  const token = getAccessToken();
  return token ? { Authorization: `Bearer ${token}` } : {};
}

/**
 * Write-context hub header. Automatically reads activeHubId from localStorage
 * so all write operations (POST/PATCH/DELETE) target the correct hub without
 * each callsite passing hubId explicitly. Explicit options.hubId overrides.
 */
function writeHubHeaders(explicitHubId?: string): Record<string, string> {
  const hubId =
    explicitHubId ??
    (typeof window !== "undefined"
      ? localStorage.getItem("memax_active_hub_id")
      : null);
  return hubId ? { "X-Hub-ID": hubId } : {};
}

export function apiAuthHeaders(): Record<string, string> {
  return authHeaders();
}

interface ApiRequestOptions {
  hubId?: string;
}

async function parseResponse<T>(res: Response): Promise<T> {
  // 204 No Content has no body — return early
  if (res.status === 204) return undefined as T;
  const text = await res.text();
  let json: ApiEnvelope;
  try {
    json = JSON.parse(text) as ApiEnvelope;
  } catch {
    if (res.status === 401) {
      throw new ApiError(
        "Session expired. Please log in again.",
        "unauthorized",
        401,
      );
    }
    throw new ApiError(
      `Server returned an unexpected response (${res.status})`,
      "invalid_response",
      res.status,
    );
  }
  if (json.error) {
    throw new ApiError(
      json.error.message,
      json.error.code,
      res.status,
      json.error.details,
    );
  }
  return json.data as T;
}

function isRetryable(err: unknown): boolean {
  return err instanceof ApiError && err.code === "not_ready";
}

function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

async function requestWithRetry<T>(
  input: RequestInfo,
  init?: RequestInit,
): Promise<T> {
  let lastErr: unknown;
  for (let attempt = 0; attempt <= MAX_RETRIES; attempt++) {
    try {
      const res = await fetch(input, init);
      return await parseResponse<T>(res);
    } catch (err) {
      lastErr = err;
      if (isRetryable(err) && attempt < MAX_RETRIES) {
        await sleep(RETRY_DELAY_MS);
        continue;
      }
      throw err;
    }
  }
  throw lastErr;
}

export async function apiPost<T>(
  path: string,
  body: unknown,
  options?: ApiRequestOptions,
): Promise<T> {
  return requestWithRetry<T>(`${BROWSER_SAFE_API_URL}${path}`, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      ...authHeaders(),
      ...writeHubHeaders(options?.hubId),
    },
    body: JSON.stringify(body),
  });
}

export async function apiGet<T>(
  path: string,
  options?: ApiRequestOptions,
): Promise<T> {
  return requestWithRetry<T>(`${BROWSER_SAFE_API_URL}${path}`, {
    cache: "no-store",
    headers: {
      ...authHeaders(),
      // GET reads use query params for hub filtering, not X-Hub-ID.
      // Explicit hubId override kept for edge cases (e.g. recall).
      ...(options?.hubId ? { "X-Hub-ID": options.hubId } : {}),
    },
  });
}

export async function apiPatch<T>(
  path: string,
  body: unknown,
  options?: ApiRequestOptions,
): Promise<T> {
  return requestWithRetry<T>(`${BROWSER_SAFE_API_URL}${path}`, {
    method: "PATCH",
    headers: {
      "Content-Type": "application/json",
      ...authHeaders(),
      ...writeHubHeaders(options?.hubId),
    },
    body: JSON.stringify(body),
  });
}

export async function apiDelete(
  path: string,
  options?: ApiRequestOptions,
): Promise<void> {
  await requestWithRetry<void>(`${BROWSER_SAFE_API_URL}${path}`, {
    method: "DELETE",
    headers: {
      ...authHeaders(),
      ...writeHubHeaders(options?.hubId),
    },
  });
}

export async function apiDownload(path: string): Promise<Response> {
  const res = await fetch(`${BROWSER_SAFE_API_URL}${path}`, {
    cache: "no-store",
    headers: { ...authHeaders() },
  });
  if (!res.ok) {
    await parseResponse<never>(res);
  }
  return res;
}

/**
 * SSE streaming POST. Calls onEvent for each server-sent event.
 * Returns an AbortController to cancel the stream.
 */
export function apiStream(
  path: string,
  body: unknown,
  onEvent: (event: string, data: unknown) => void,
  options?: ApiRequestOptions,
): AbortController {
  const controller = new AbortController();

  (async () => {
    const res = await fetch(`${BROWSER_SAFE_API_URL}${path}`, {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        Accept: "text/event-stream",
        ...authHeaders(),
        ...writeHubHeaders(options?.hubId),
      },
      body: JSON.stringify(body),
      signal: controller.signal,
    });

    if (!res.ok || !res.body) return;

    const reader = res.body.getReader();
    const decoder = new TextDecoder();
    let buffer = "";

    // eslint-disable-next-line no-constant-condition
    while (true) {
      const { done, value } = await reader.read();
      if (done) break;

      buffer += decoder.decode(value, { stream: true });
      const lines = buffer.split("\n");
      buffer = lines.pop() ?? "";

      let currentEvent = "";
      for (const line of lines) {
        if (line.startsWith("event: ")) {
          currentEvent = line.slice(7);
        } else if (line.startsWith("data: ") && currentEvent) {
          try {
            onEvent(currentEvent, JSON.parse(line.slice(6)));
          } catch {
            // ignore malformed JSON
          }
          currentEvent = "";
        }
      }
    }
  })().catch(() => {
    // Aborted or network error — silently ignore
  });

  return controller;
}
