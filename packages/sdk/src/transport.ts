import type { MemaxConfig } from "./types.js";
import { MemaxError, parseRetryAfter } from "./errors.js";

/**
 * Fallback prod URL used when the SDK constructor receives no `apiUrl`.
 *
 * Consumers almost always override this via `new Memax({ apiUrl })`.
 * Web threads `NEXT_PUBLIC_API_URL` in; CLI threads `MEMAX_API_URL` /
 * config file; third-party consumers pass their own.
 */
const DEFAULT_API_URL = "https://api.memax.app";
const DEFAULT_MAX_RETRIES = 2;
const DEFAULT_RETRY_DELAY_MS = 1500;

/**
 * A value that can be serialized into a query string. Arrays produce
 * repeated `?key=a&key=b` pairs — required by the /v1/notifications
 * list endpoint for `kind` and `resolution` filters.
 */
export type QueryValue =
  | string
  | number
  | boolean
  | null
  | undefined
  | ReadonlyArray<string | number | boolean>;

export interface RequestOptions {
  body?: unknown;
  hubId?: string;
  query?: Record<string, QueryValue>;
  /**
   * Forwarded to `fetch` so that React Query's `cancelQueries` (or any
   * other AbortController) actually aborts the underlying HTTP request,
   * not just client-side query state. Aborts are re-thrown as the
   * fetch's native `AbortError` so React Query recognizes the
   * cancellation; we never wrap them as `network_error`.
   */
  signal?: AbortSignal;
}

export interface StreamOptions extends RequestOptions {
  onEvent: (event: string, data: unknown) => void;
  onClose?: () => void;
}

export interface DownloadOptions {
  hubId?: string;
  signal?: AbortSignal;
}

export type RequestFn = <T>(
  method: string,
  path: string,
  options?: RequestOptions,
) => Promise<T>;

export type StreamFn = (
  method: string,
  path: string,
  options: StreamOptions,
) => AbortController;

export type DownloadFn = (
  path: string,
  options?: DownloadOptions,
) => Promise<Response>;

function resolveFetch(config: MemaxConfig): typeof globalThis.fetch {
  if (config.fetch) {
    return config.fetch;
  }
  if (typeof globalThis.fetch !== "function") {
    throw new Error(
      "Fetch is not available in this runtime. Pass `fetch` in MemaxConfig.",
    );
  }
  return globalThis.fetch.bind(globalThis);
}

function createAuthProvider(
  config: MemaxConfig,
): () => Promise<Record<string, string>> {
  if (config.auth) {
    return config.auth;
  }
  if (config.apiKey) {
    const key = config.apiKey;
    return async () => ({ Authorization: `Bearer ${key}` });
  }
  return async () => ({});
}

function summarizeResponseText(text: string): string {
  const normalized = text.replace(/\s+/g, " ").trim();
  if (!normalized) {
    return "";
  }
  return normalized.length > 120
    ? `${normalized.slice(0, 117)}...`
    : normalized;
}

function formatRequestLabel(method: string, url: string): string {
  return `${method.toUpperCase()} ${url}`;
}

function isAbortShaped(value: unknown): value is Error {
  if (typeof DOMException !== "undefined" && value instanceof DOMException) {
    return value.name === "AbortError";
  }
  return value instanceof Error && value.name === "AbortError";
}

function makeAbortError(message = "Aborted"): Error {
  // DOMException is the spec-correct shape, but it isn't on every JS
  // runtime (older Node, some edge workers). Fall back to a plain Error
  // with the right name so React Query's name-based detection still works.
  if (typeof DOMException !== "undefined") {
    return new DOMException(message, "AbortError");
  }
  const err = new Error(message);
  err.name = "AbortError";
  return err;
}

function signalAbortError(signal: AbortSignal): Error {
  // AbortController.abort(reason) accepts arbitrary values (strings,
  // POJOs, custom Errors). Only return reason verbatim when it's already
  // abort-shaped — otherwise wrap in a normalized AbortError so
  // React Query's `error.name === "AbortError"` cancellation check fires.
  // The original reason is preserved as `cause` for debuggers.
  const reason = (signal as { reason?: unknown }).reason;
  if (isAbortShaped(reason)) {
    return reason;
  }
  const err = makeAbortError();
  if (reason !== undefined) {
    (err as { cause?: unknown }).cause = reason;
  }
  return err;
}

function sleepUnlessAborted(ms: number, signal?: AbortSignal): Promise<void> {
  return new Promise((resolve, reject) => {
    if (signal?.aborted) {
      reject(signalAbortError(signal));
      return;
    }
    const timer = setTimeout(() => {
      signal?.removeEventListener("abort", onAbort);
      resolve();
    }, ms);
    const onAbort = () => {
      clearTimeout(timer);
      reject(signalAbortError(signal!));
    };
    signal?.addEventListener("abort", onAbort, { once: true });
  });
}

function buildQueryString(query?: RequestOptions["query"]): string {
  if (!query) {
    return "";
  }

  const params = new URLSearchParams();
  for (const [key, value] of Object.entries(query)) {
    if (value == null) {
      continue;
    }
    if (Array.isArray(value)) {
      // Repeatable params (e.g. ?kind=a&kind=b) — required by the
      // /v1/notifications list endpoint. Skip empty entries rather
      // than emitting `?kind=`.
      for (const item of value) {
        if (item == null || item === "") continue;
        params.append(key, String(item));
      }
      continue;
    }
    params.set(key, String(value));
  }

  const encoded = params.toString();
  return encoded ? `?${encoded}` : "";
}

export class ApiTransport {
  // Public so `Memax` (client.ts) can pass the resolved apiUrl to
  // AuthResource without duplicating the DEFAULT_API_URL fallback —
  // drift between the two would let the drift-guard test pass while
  // `auth.githubURL(...)` still used the old host.
  readonly apiUrl: string;
  private readonly getAuth: () => Promise<Record<string, string>>;
  private readonly fetchImpl: typeof globalThis.fetch;
  private readonly headers: Record<string, string>;
  private readonly maxRetries: number;
  private readonly retryDelayMs: number;
  private readonly onWarning?: (warning: string) => void;

  constructor(config: MemaxConfig) {
    this.apiUrl = (config.apiUrl ?? DEFAULT_API_URL).replace(/\/$/, "");
    this.getAuth = createAuthProvider(config);
    this.fetchImpl = resolveFetch(config);
    this.headers = config.headers ?? {};
    this.maxRetries = config.maxRetries ?? DEFAULT_MAX_RETRIES;
    this.retryDelayMs = config.retryDelayMs ?? DEFAULT_RETRY_DELAY_MS;
    this.onWarning = config.onWarning;
  }

  async request<T>(
    method: string,
    path: string,
    options?: RequestOptions,
  ): Promise<T> {
    const url = `${this.apiUrl}${path}${buildQueryString(options?.query)}`;

    let lastErr: unknown;
    for (let attempt = 0; attempt <= this.maxRetries; attempt++) {
      // Bail before doing any work if the caller already cancelled.
      // Matters between not_ready retries — the inter-retry sleep can
      // outlast the user's interest.
      if (options?.signal?.aborted) {
        throw signalAbortError(options.signal);
      }

      const authHeaders = await this.getAuth();
      const headers: Record<string, string> = {
        "Content-Type": "application/json",
        ...this.headers,
        ...authHeaders,
      };
      if (options?.hubId) {
        headers["X-Hub-ID"] = options.hubId;
      }
      // Auto-detect and send client timezone for TZ-aware date handling
      try {
        const tz = Intl.DateTimeFormat().resolvedOptions().timeZone;
        if (tz) headers["X-Timezone"] = tz;
      } catch {
        // Intl not available in some runtimes — skip silently
      }

      let res: Response;
      try {
        res = await this.fetchImpl(url, {
          method,
          headers,
          body:
            options?.body !== undefined
              ? JSON.stringify(options.body)
              : undefined,
          signal: options?.signal,
        });
      } catch (error) {
        // When the signal was the cause, always throw a normalized
        // AbortError — a custom abort reason (non-Error or misnamed
        // Error) would otherwise bypass React Query's name-based
        // cancellation detection and surface as a real failure.
        if (options?.signal?.aborted) {
          throw signalAbortError(options.signal);
        }
        // Fetch can also abort internally (e.g. request body stream
        // error) — propagate those unwrapped so callers can distinguish
        // cancellation from a real "Cannot reach API" network failure.
        if (isAbortShaped(error)) {
          throw error;
        }
        throw new MemaxError(
          `Cannot reach API at ${url} — is the server running?`,
          "network_error",
          0,
        );
      }

      const text = await res.text();
      const warning = res.headers.get("X-Memax-Warning");
      if (warning) {
        this.onWarning?.(warning);
      }
      if (!text && res.ok) {
        return undefined as T;
      }

      let json: {
        data?: T;
        error?: {
          code: string;
          message: string;
          details?: Record<string, unknown>;
        };
      };
      try {
        json = JSON.parse(text);
      } catch {
        const requestLabel = formatRequestLabel(method, url);
        if (res.status === 401) {
          throw new MemaxError(
            `${requestLabel} returned 401 Unauthorized`,
            "unauthorized",
            401,
          );
        }
        const preview = summarizeResponseText(text);
        // Preserve Retry-After on non-JSON 429s too — a CDN or WAF can
        // surface a rate-limit to the browser without the server's
        // error envelope. The classifier still needs the seconds.
        const retryAfter =
          res.status === 429 || res.status === 503
            ? parseRetryAfter(res.headers.get("Retry-After"))
            : undefined;
        throw new MemaxError(
          preview
            ? `${requestLabel} returned ${res.status} with non-JSON response: ${preview}`
            : `${requestLabel} returned ${res.status} with non-JSON response`,
          "invalid_response",
          res.status,
          undefined,
          retryAfter,
        );
      }

      if (json.error) {
        // Retry-After is only meaningful on 429 in practice, but the
        // spec allows it on 503 too. Parse eagerly and attach — the
        // MemaxError getters let callers decide how to surface it.
        const retryAfter =
          res.status === 429 || res.status === 503
            ? parseRetryAfter(res.headers.get("Retry-After"))
            : undefined;
        const err = new MemaxError(
          json.error.message,
          json.error.code,
          res.status,
          json.error.details,
          retryAfter,
        );
        if (json.error.code === "not_ready" && attempt < this.maxRetries) {
          lastErr = err;
          await sleepUnlessAborted(this.retryDelayMs, options?.signal);
          continue;
        }
        throw err;
      }

      return json.data as T;
    }

    throw lastErr;
  }

  async download(path: string, options?: DownloadOptions): Promise<Response> {
    const url = `${this.apiUrl}${path}`;
    const authHeaders = await this.getAuth();
    const headers: Record<string, string> = {
      ...this.headers,
      ...authHeaders,
    };
    if (options?.hubId) {
      headers["X-Hub-ID"] = options.hubId;
    }
    try {
      const tz = Intl.DateTimeFormat().resolvedOptions().timeZone;
      if (tz) headers["X-Timezone"] = tz;
    } catch {
      // Intl not available — skip
    }

    let res: Response;
    try {
      res = await this.fetchImpl(url, {
        method: "GET",
        cache: "no-store",
        headers,
        signal: options?.signal,
      });
    } catch (error) {
      if (options?.signal?.aborted) {
        throw signalAbortError(options.signal);
      }
      if (isAbortShaped(error)) {
        throw error;
      }
      throw new MemaxError(
        `Cannot reach API at ${url} — is the server running?`,
        "network_error",
        0,
      );
    }

    if (res.ok) {
      return res;
    }

    const text = await res.text();
    if (!text) {
      throw new MemaxError(
        `${formatRequestLabel("GET", url)} returned ${res.status} with an empty error response`,
        "invalid_response",
        res.status,
      );
    }

    try {
      const json = JSON.parse(text) as {
        error?: {
          code: string;
          message: string;
          details?: Record<string, unknown>;
        };
      };
      if (json.error) {
        throw new MemaxError(
          json.error.message,
          json.error.code,
          res.status,
          json.error.details,
        );
      }
    } catch (error) {
      if (error instanceof MemaxError) {
        throw error;
      }
    }

    const preview = summarizeResponseText(text);
    throw new MemaxError(
      preview
        ? `${formatRequestLabel("GET", url)} returned ${res.status} with non-JSON response: ${preview}`
        : `${formatRequestLabel("GET", url)} returned ${res.status} with non-JSON response`,
      "invalid_response",
      res.status,
    );
  }

  stream(
    method: string,
    path: string,
    options: StreamOptions,
  ): AbortController {
    const controller = new AbortController();
    const url = `${this.apiUrl}${path}${buildQueryString(options.query)}`;

    void (async () => {
      const authHeaders = await this.getAuth();
      const headers: Record<string, string> = {
        "Content-Type": "application/json",
        Accept: "text/event-stream",
        ...this.headers,
        ...authHeaders,
      };
      if (options.hubId) {
        headers["X-Hub-ID"] = options.hubId;
      }
      try {
        const tz = Intl.DateTimeFormat().resolvedOptions().timeZone;
        if (tz) headers["X-Timezone"] = tz;
      } catch {
        // Intl not available — skip
      }

      let res: Response;
      try {
        res = await this.fetchImpl(url, {
          method,
          headers,
          body:
            options.body !== undefined
              ? JSON.stringify(options.body)
              : undefined,
          signal: controller.signal,
        });
      } catch (error) {
        if (!controller.signal.aborted) {
          options.onEvent("error", {
            code: "network_error",
            message:
              error instanceof Error ? error.message : "Cannot reach Memax API",
          });
        }
        return;
      }

      if (!res.ok || !res.body) {
        let payload:
          | {
              error?: {
                code?: string;
                message?: string;
                details?: Record<string, unknown>;
              };
            }
          | undefined;
        try {
          payload = (await res.json()) as {
            error?: {
              code?: string;
              message?: string;
              details?: Record<string, unknown>;
            };
          };
        } catch {
          // ignore
        }
        options.onEvent("error", {
          code: payload?.error?.code ?? "stream_error",
          message:
            payload?.error?.message ??
            `Stream request failed with ${res.status}`,
          status: res.status,
          details: payload?.error?.details,
        });
        return;
      }

      const reader = res.body.getReader();
      const decoder = new TextDecoder();
      let buffer = "";
      let currentEvent = "";

      try {
        while (true) {
          const { done, value } = await reader.read();
          if (done) break;

          buffer += decoder.decode(value, { stream: true });
          const lines = buffer.split("\n");
          buffer = lines.pop() ?? "";

          for (const rawLine of lines) {
            const line = rawLine.trimEnd();
            if (!line) {
              currentEvent = "";
              continue;
            }
            if (line.startsWith("event: ")) {
              currentEvent = line.slice(7).trim();
              continue;
            }
            if (!line.startsWith("data: ")) continue;

            const eventName = currentEvent || "message";
            const rawData = line.slice(6);
            let data: unknown = rawData;
            try {
              data = JSON.parse(rawData);
            } catch {
              // keep raw string
            }
            options.onEvent(eventName, data);
          }
        }
      } catch (error) {
        if (!controller.signal.aborted) {
          options.onEvent("error", {
            code: "stream_error",
            message:
              error instanceof Error ? error.message : "Stream read failed",
          });
        }
      } finally {
        if (!controller.signal.aborted) {
          options.onClose?.();
        }
      }
    })();

    return controller;
  }
}
