import { describe, expect, it, vi } from "vitest";
import { Memax, MemaxError } from "../index.js";
import type { V2 } from "../index.js";

function jsonResponse(body: unknown, init?: ResponseInit): Response {
  return new Response(JSON.stringify(body), {
    status: 200,
    headers: { "Content-Type": "application/json" },
    ...init,
  });
}

function client(response: Response) {
  const fetchMock = vi.fn(async () => response.clone());
  const memax = new Memax({
    apiUrl: "https://api.memax.app",
    apiKey: "mxk_test",
    fetch: fetchMock,
    maxRetries: 0,
  });
  const call = (i = 0) => {
    const [url, init] = fetchMock.mock.calls[i] as unknown as [
      string,
      RequestInit & { headers: Record<string, string> },
    ];
    return {
      url,
      method: init.method,
      headers: init.headers,
      body: init.body === undefined ? undefined : JSON.parse(String(init.body)),
    };
  };
  return { memax, call };
}

const settings: V2.NotificationSettings = {
  version: 3,
  time_zone: "America/Vancouver",
  time_zone_source: "observed",
  events: [
    { event: "decision_gate", in_app: true, email: true, email_sent: false },
    { event: "morning_edition", in_app: true, email: true, email_sent: true },
    { event: "weekly_summary", in_app: false, email: true, email_sent: false },
  ],
  quiet_hours: {
    on: true,
    from: "20:00",
    until: "08:00",
    gates_through: true,
  },
  review_after_days: 1,
};

describe("memax.v2.settings", () => {
  it("reads the notification settings", async () => {
    const { memax, call } = client(jsonResponse({ data: settings }));
    const got = await memax.v2.settings.notifications();
    expect(got.version).toBe(3);
    expect(call().url).toBe("https://api.memax.app/v2/me/notifications");
    expect(call().method).toBe("GET");
  });

  it("changes them with an Idempotency-Key and the version as If-Match", async () => {
    const { memax, call } = client(
      jsonResponse({ data: { ...settings, version: 4 } }),
    );
    const got = await memax.v2.settings.updateNotifications(
      {
        events: { drift: { email: true } },
        quiet_hours: { from: "21:30" },
      },
      { idempotencyKey: "k-1", ifMatch: 3 },
    );
    expect(got.version).toBe(4);
    const c = call();
    expect(c.method).toBe("PATCH");
    expect(c.url).toBe("https://api.memax.app/v2/me/notifications");
    expect(c.headers["Idempotency-Key"]).toBe("k-1");
    expect(c.headers["If-Match"]).toBe('"3"');
    expect(c.body).toEqual({
      events: { drift: { email: true } },
      quiet_hours: { from: "21:30" },
    });
  });

  it("surfaces an edit clash, such as an unsubscribe since", async () => {
    const { memax } = client(
      jsonResponse(
        {
          error: {
            code: "edit_clash",
            message:
              "Your notification settings changed since you opened them.",
            details: { expected_version: 3, current_version: 5 },
          },
        },
        { status: 412 },
      ),
    );
    const err = await memax.v2.settings
      .updateNotifications(
        { events: { morning_edition: { email: true } } },
        { idempotencyKey: "k-2", ifMatch: 3 },
      )
      .catch((e: unknown) => e);
    expect(err).toBeInstanceOf(MemaxError);
    expect((err as MemaxError).code).toBe("edit_clash");
  });

  it("reads the security posture", async () => {
    const security: V2.Security = {
      assurance: "human_web",
      residency: [{ holds: "database", provider: "neon", region: "us-west-2" }],
      processors: [
        {
          name: "voyage",
          retention: "unconfirmed",
          uses: [
            { use: "embeddings", model: "voyage-4", zero_retention: false },
          ],
        },
      ],
      backup_days: 7,
    };
    const { memax, call } = client(jsonResponse({ data: security }));
    const got = await memax.v2.settings.security();
    expect(got.processors[0]?.retention).toBe("unconfirmed");
    expect(call().url).toBe("https://api.memax.app/v2/security");
  });
});
