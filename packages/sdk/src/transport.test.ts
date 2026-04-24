import { describe, expect, it, vi } from "vitest";
import { MemaxError } from "./errors.js";
import { ApiTransport } from "./transport.js";

function jsonResponse(body: unknown, init?: ResponseInit): Response {
  return new Response(JSON.stringify(body), {
    status: 200,
    headers: { "Content-Type": "application/json" },
    ...init,
  });
}

describe("ApiTransport", () => {
  it("uses apiKey auth and merges query params", async () => {
    const fetchMock = vi.fn(async () => jsonResponse({ data: { ok: true } }));
    const transport = new ApiTransport({
      apiUrl: "https://api.memax.app/",
      apiKey: "test-key",
      fetch: fetchMock,
      headers: { "X-Test": "1" },
    });

    await transport.request("GET", "/v1/test", {
      query: { a: 1, b: "two", skip: undefined },
      hubId: "hub-1",
    });

    expect(fetchMock).toHaveBeenCalledWith(
      "https://api.memax.app/v1/test?a=1&b=two",
      expect.objectContaining({
        method: "GET",
        headers: expect.objectContaining({
          Authorization: "Bearer test-key",
          "X-Hub-ID": "hub-1",
          "X-Test": "1",
          "Content-Type": "application/json",
        }),
      }),
    );
  });

  it("returns undefined for empty successful responses", async () => {
    const fetchMock = vi.fn(async () => new Response(null, { status: 204 }));
    const transport = new ApiTransport({ fetch: fetchMock });

    await expect(
      transport.request<void>("DELETE", "/v1/test"),
    ).resolves.toBeUndefined();
  });

  it("retries not_ready responses", async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(
        jsonResponse(
          { error: { code: "not_ready", message: "warming up" } },
          { status: 503 },
        ),
      )
      .mockResolvedValueOnce(jsonResponse({ data: { ok: true } }));

    const transport = new ApiTransport({
      fetch: fetchMock,
      maxRetries: 1,
      retryDelayMs: 0,
    });

    await expect(
      transport.request<{ ok: boolean }>("GET", "/v1/test"),
    ).resolves.toEqual({ ok: true });
    expect(fetchMock).toHaveBeenCalledTimes(2);
  });

  it("propagates Retry-After on 429 JSON errors", async () => {
    const fetchMock = vi.fn(
      async () =>
        new Response(
          JSON.stringify({
            error: { code: "rate_limited", message: "slow down" },
          }),
          {
            status: 429,
            headers: {
              "Content-Type": "application/json",
              "Retry-After": "42",
            },
          },
        ),
    );
    const transport = new ApiTransport({ fetch: fetchMock });

    const err = (await transport
      .request("GET", "/v1/test")
      .catch((e: MemaxError) => e)) as MemaxError;
    expect(err).toBeInstanceOf(MemaxError);
    expect(err.status).toBe(429);
    expect(err.code).toBe("rate_limited");
    expect(err.retryAfterSeconds).toBe(42);
    expect(err.isRateLimited).toBe(true);
  });

  it("propagates Retry-After on 429 non-JSON responses (CDN/WAF path)", async () => {
    // Some edge proxies return plain text or HTML for 429. The SDK
    // must still surface the Retry-After so the classifier can
    // render a countdown.
    const fetchMock = vi.fn(
      async () =>
        new Response("<html>too many requests</html>", {
          status: 429,
          headers: { "Retry-After": "5" },
        }),
    );
    const transport = new ApiTransport({ fetch: fetchMock });
    const err = (await transport
      .request("GET", "/v1/test")
      .catch((e: MemaxError) => e)) as MemaxError;
    expect(err).toBeInstanceOf(MemaxError);
    expect(err.status).toBe(429);
    expect(err.retryAfterSeconds).toBe(5);
  });

  it("leaves retryAfterSeconds undefined for non-429/503 statuses", async () => {
    const fetchMock = vi.fn(
      async () =>
        new Response(
          JSON.stringify({ error: { code: "forbidden", message: "nope" } }),
          {
            status: 403,
            headers: {
              "Content-Type": "application/json",
              "Retry-After": "60", // ignored by spec on 403
            },
          },
        ),
    );
    const transport = new ApiTransport({ fetch: fetchMock });
    const err = (await transport
      .request("GET", "/v1/test")
      .catch((e: MemaxError) => e)) as MemaxError;
    expect(err.status).toBe(403);
    expect(err.retryAfterSeconds).toBeUndefined();
  });

  it("maps unauthorized non-json responses", async () => {
    const fetchMock = vi.fn(
      async () => new Response("unauthorized", { status: 401 }),
    );
    const transport = new ApiTransport({ fetch: fetchMock });

    await expect(transport.request("GET", "/v1/test")).rejects.toMatchObject({
      code: "unauthorized",
      status: 401,
    } satisfies Partial<MemaxError>);
  });

  it("throws network_error when fetch fails", async () => {
    const fetchMock = vi.fn(async () => {
      throw new Error("boom");
    });
    const transport = new ApiTransport({ fetch: fetchMock });

    await expect(transport.request("GET", "/v1/test")).rejects.toMatchObject({
      code: "network_error",
      status: 0,
    } satisfies Partial<MemaxError>);
  });

  it("forwards signal to fetch and rethrows AbortError without wrapping", async () => {
    const controller = new AbortController();
    // Mimic fetch's contract: handle both already-aborted (sync throw) and
    // mid-flight abort (listener-driven rejection) so the test isn't sensitive
    // to whether the abort wins the race against the internal auth await.
    const fetchMock = vi.fn(async (_url: string, init: RequestInit) => {
      if (init.signal?.aborted) {
        throw new DOMException("The operation was aborted.", "AbortError");
      }
      return new Promise<Response>((_resolve, reject) => {
        init.signal?.addEventListener("abort", () => {
          reject(new DOMException("The operation was aborted.", "AbortError"));
        });
      });
    });
    const transport = new ApiTransport({ fetch: fetchMock });

    const requestPromise = transport.request("GET", "/v1/test", {
      signal: controller.signal,
    });
    controller.abort();

    await expect(requestPromise).rejects.toMatchObject({ name: "AbortError" });
    // Critically: not wrapped as MemaxError network_error — React Query
    // recognizes the cancellation by the AbortError name.
    await expect(requestPromise).rejects.not.toBeInstanceOf(MemaxError);
    expect(fetchMock).toHaveBeenCalledWith(
      "https://api.memax.app/v1/test",
      expect.objectContaining({ signal: controller.signal }),
    );
  });

  it("bails out before fetch when signal is already aborted", async () => {
    const controller = new AbortController();
    controller.abort();
    const fetchMock = vi.fn(async () => jsonResponse({ data: { ok: true } }));
    const transport = new ApiTransport({ fetch: fetchMock });

    await expect(
      transport.request("GET", "/v1/test", { signal: controller.signal }),
    ).rejects.toMatchObject({ name: "AbortError" });
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("normalizes custom abort reasons to AbortError so React Query detects cancellation", async () => {
    // controller.abort(reason) accepts arbitrary values. React Query
    // detects cancelled queries by `error.name === "AbortError"`, so a
    // raw string or generic Error reason would otherwise be invisible
    // and leak through as a real failure toast.
    const controller = new AbortController();
    controller.abort("user navigated away");

    const fetchMock = vi.fn(async () => jsonResponse({ data: { ok: true } }));
    const transport = new ApiTransport({ fetch: fetchMock });

    const rejection = await transport
      .request("GET", "/v1/test", { signal: controller.signal })
      .catch((err: Error) => err);

    expect(rejection).toBeInstanceOf(Error);
    expect(rejection).toMatchObject({ name: "AbortError" });
    expect((rejection as { cause?: unknown }).cause).toBe(
      "user navigated away",
    );
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("aborts the not_ready retry sleep when signal fires mid-backoff", async () => {
    const controller = new AbortController();
    const fetchMock = vi.fn(async () =>
      jsonResponse(
        { error: { code: "not_ready", message: "warming up" } },
        { status: 503 },
      ),
    );
    const transport = new ApiTransport({
      fetch: fetchMock,
      maxRetries: 3,
      // Long enough that the test would be slow without the abort cutting it short.
      retryDelayMs: 1000,
    });

    const requestPromise = transport.request("GET", "/v1/test", {
      signal: controller.signal,
    });
    // Let the first attempt land and enter the inter-retry sleep.
    await Promise.resolve();
    await Promise.resolve();
    controller.abort();

    await expect(requestPromise).rejects.toMatchObject({ name: "AbortError" });
    // Only the first attempt fired — the abort cut the sleep before retry 2.
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });

  it("parses stream events", async () => {
    const payload = [
      "event: delta\n",
      'data: {"text":"Hello"}\n\n',
      "event: done\n",
      'data: {"ok":true}\n\n',
    ].join("");
    const fetchMock = vi.fn(
      async () =>
        new Response(payload, {
          status: 200,
          headers: { "Content-Type": "text/event-stream" },
        }),
    );
    const transport = new ApiTransport({ fetch: fetchMock });
    const events: Array<[string, unknown]> = [];

    transport.stream("POST", "/v1/ask", {
      body: { query: "hi" },
      onEvent: (event, data) => {
        events.push([event, data]);
      },
    });

    await new Promise((resolve) => setTimeout(resolve, 0));

    expect(events).toEqual([
      ["delta", { text: "Hello" }],
      ["done", { ok: true }],
    ]);
  });

  it("defaults apiUrl to the production API origin", async () => {
    const fetchMock = vi.fn<typeof fetch>(
      async () =>
        new Response(JSON.stringify({ data: {} }), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        }),
    );
    const transport = new ApiTransport({ fetch: fetchMock });
    await transport.request("GET", "/v1/anything");

    const calledUrl = String(fetchMock.mock.calls[0]?.[0] ?? "");
    expect(calledUrl.startsWith("https://api.memax.app")).toBe(true);
  });
});
