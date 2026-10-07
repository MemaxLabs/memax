import { describe, expect, it, vi } from "vitest";
import type { V2 } from "memax-sdk";
import { CommandFailedError, toFailure } from "./command-error";
import { createDemoDream } from "./dream-demo";
import {
  applyChange,
  emailedNow,
  inQuietHours,
  isClock,
  NOTIFICATION_EVENTS,
} from "./settings";
import { createDemoSettings, DEMO_SECURITY } from "./settings-demo";
import {
  createSdkSettings,
  notificationsInput,
  notificationsOf,
  securityOf,
} from "./settings-sdk";

const wire: V2.NotificationSettings = {
  version: 4,
  time_zone: "America/Vancouver",
  time_zone_source: "observed",
  events: [
    { event: "decision_gate", in_app: true, email: true, email_sent: false },
    { event: "morning_edition", in_app: true, email: false, email_sent: true },
    { event: "weekly_summary", in_app: false, email: true, email_sent: false },
  ],
  quiet_hours: {
    on: true,
    from: "21:30",
    until: "07:00",
    gates_through: false,
  },
  review_after_days: 2,
  updated_at: "2026-10-05T10:00:00Z",
};

describe("notificationsOf", () => {
  it("maps the wire settings", () => {
    expect(notificationsOf(wire)).toEqual({
      version: 4,
      timeZone: "America/Vancouver",
      timeZoneSource: "observed",
      events: [
        {
          event: "decision_gate",
          inApp: true,
          email: true,
          emailSent: false,
        },
        {
          event: "morning_edition",
          inApp: true,
          email: false,
          emailSent: true,
        },
        {
          event: "weekly_summary",
          inApp: false,
          email: true,
          emailSent: false,
        },
      ],
      quietHours: {
        on: true,
        from: "21:30",
        until: "07:00",
        gatesThrough: false,
      },
      reviewAfterDays: 2,
    });
  });
});

describe("notificationsInput", () => {
  it("sends only what the change names", () => {
    expect(notificationsInput({ email: { drift: true } })).toEqual({
      events: { drift: { email: true } },
    });
    expect(
      notificationsInput({
        quietHours: { gatesThrough: false, from: "22:00" },
      }),
    ).toEqual({ quiet_hours: { from: "22:00", gates_through: false } });
    expect(notificationsInput({ reviewAfterDays: 3, quietHours: {} })).toEqual({
      review_after_days: 3,
    });
  });

  it("goes out with the version as If-Match and a key", async () => {
    const updateNotifications = vi.fn().mockResolvedValue(wire);
    const source = createSdkSettings({
      v2: { settings: { updateNotifications } },
    } as never);
    const got = await source.updateNotifications({
      change: { email: { stale: true } },
      version: 3,
      idempotencyKey: "k",
    });
    expect(updateNotifications).toHaveBeenCalledWith(
      { events: { stale: { email: true } } },
      { idempotencyKey: "k", ifMatch: 3 },
    );
    expect(got.version).toBe(4);
  });
});

describe("securityOf", () => {
  it("maps the posture, absent fields as null or empty", () => {
    const s = securityOf({
      assurance: "client_attested",
      passkey_check: false,
      residency: [
        { holds: "database", provider: "neon", region: "us-west-2" },
        { holds: "objects", provider: "r2" },
      ],
      processors: [
        {
          name: "openrouter",
          retention: "zero",
          uses: [
            {
              use: "judge",
              model: "deepseek/deepseek-v4.1-flash",
              hosts: ["together"],
              min_precision: "fp8",
              zero_retention: true,
            },
          ],
        },
        {
          name: "resend",
          retention: "provider_terms",
          uses: [{ use: "email", zero_retention: false }],
        },
      ],
      backup_days: 7,
    });
    expect(s.residency[1]).toEqual({
      holds: "objects",
      provider: "r2",
      region: null,
    });
    expect(s.processors[1]!.uses[0]).toEqual({
      use: "email",
      model: null,
      hosts: [],
      minPrecision: null,
      zeroRetention: false,
    });
    expect(s.processors[0]!.uses[0]!.hosts).toEqual(["together"]);
  });
});

describe("quiet hours and changes", () => {
  it("reads 24-hour times and windows across midnight", () => {
    expect(isClock("08:00")).toBe(true);
    expect(isClock("8:00")).toBe(false);
    expect(isClock("24:00")).toBe(false);
    const night = {
      on: true,
      from: "20:00",
      until: "08:00",
      gatesThrough: true,
    };
    expect(inQuietHours(night, "03:00")).toBe(true);
    expect(inQuietHours(night, "08:00")).toBe(false);
    expect(inQuietHours(night, "20:00")).toBe(true);
    expect(inQuietHours({ ...night, on: false }, "03:00")).toBe(false);
    const lunch = {
      on: true,
      from: "12:00",
      until: "13:00",
      gatesThrough: true,
    };
    expect(inQuietHours(lunch, "03:00")).toBe(false);
    expect(inQuietHours(lunch, "12:30")).toBe(true);
  });

  it("applies a change as the server would, and says what's emailed now", () => {
    const s = notificationsOf(wire);
    const next = applyChange(s, {
      email: { decision_gate: false },
      quietHours: { on: false },
      reviewAfterDays: 5,
    });
    expect(next.events[0]!.email).toBe(false);
    expect(next.quietHours).toEqual({ ...s.quietHours, on: false });
    expect(next.reviewAfterDays).toBe(5);
    expect(next.version).toBe(4);
    expect(emailedNow(s)).toEqual(["morning_edition"]);
  });
});

describe("the demo's settings", () => {
  it("draws the board's email column, and the morning email is Dream's setting", async () => {
    const dream = createDemoDream();
    const demo = createDemoSettings({ dream });
    const s = demo.peekNotifications!()!;
    expect(s.events.map((e) => e.event)).toEqual(NOTIFICATION_EVENTS);
    expect(s.events.filter((e) => e.email).map((e) => e.event)).toEqual([
      "decision_gate",
      "morning_edition",
      "review_waiting",
      "write_held",
      "weekly_summary",
      "agent_changed",
    ]);
    const off = await demo.updateNotifications({
      change: { email: { morning_edition: false } },
      version: s.version,
      idempotencyKey: "k",
    });
    expect((await dream.settings()).morningEmail).toBe(false);
    expect(off.version).toBe(s.version + 1);
    // Turning it on in Dream's settings moves the version: an edit from
    // the old one clashes.
    await dream.updateSettings({ morningEmail: true, idempotencyKey: "d" });
    const now = demo.peekNotifications!()!;
    expect(now.version).toBe(off.version + 1);
    expect(now.events[1]!.email).toBe(true);
    const clash = await demo
      .updateNotifications({
        change: { email: { drift: true } },
        version: off.version,
        idempotencyKey: "k2",
      })
      .catch((e: unknown) => e);
    expect(clash).toBeInstanceOf(CommandFailedError);
    expect(toFailure(clash)).toEqual({
      kind: "clash",
      currentVersion: now.version,
    });
    expect(await demo.security()).toBe(DEMO_SECURITY);
  });
});
