import type { V2 } from "memax-sdk";
import type { V2Client } from "./sdk-records";
import type {
  NotificationChange,
  NotificationSettingsView,
  ProcessorView,
  SecurityView,
  SettingsSource,
} from "./settings";

/**
 * Settings through memax.v2.settings: `GET`/`PATCH /v2/me/notifications`
 * (If-Match is the settings' version) and `GET /v2/security`. A space's
 * seals come from its checkpoints (activity-sdk.ts) and the agents from
 * the agents list (agents-sdk.ts), so this module maps only what
 * /v2/security and the notification settings serve.
 */

export function notificationsOf(
  s: V2.NotificationSettings,
): NotificationSettingsView {
  return {
    version: s.version,
    timeZone: s.time_zone,
    timeZoneSource: s.time_zone_source,
    events: s.events.map((e) => ({
      event: e.event,
      inApp: e.in_app,
      email: e.email,
      emailSent: e.email_sent,
    })),
    quietHours: {
      on: s.quiet_hours.on,
      from: s.quiet_hours.from,
      until: s.quiet_hours.until,
      gatesThrough: s.quiet_hours.gates_through,
    },
    reviewAfterDays: s.review_after_days,
  };
}

/** The request body for a change: only what it names. */
export function notificationsInput(
  change: NotificationChange,
): V2.NotificationSettingsInput {
  const input: V2.NotificationSettingsInput = {};
  const events = Object.entries(change.email ?? {}).filter(
    ([, v]) => v !== undefined,
  );
  if (events.length > 0) {
    input.events = Object.fromEntries(
      events.map(([event, email]) => [event, { email: email! }]),
    );
  }
  const q = change.quietHours;
  if (q && Object.keys(q).length > 0) {
    input.quiet_hours = {
      ...(q.on !== undefined ? { on: q.on } : {}),
      ...(q.from !== undefined ? { from: q.from } : {}),
      ...(q.until !== undefined ? { until: q.until } : {}),
      ...(q.gatesThrough !== undefined
        ? { gates_through: q.gatesThrough }
        : {}),
    };
  }
  if (change.reviewAfterDays !== undefined) {
    input.review_after_days = change.reviewAfterDays;
  }
  return input;
}

export function securityOf(s: V2.Security): SecurityView {
  return {
    assurance: s.assurance,
    residency: s.residency.map((p) => ({
      holds: p.holds,
      provider: p.provider,
      region: p.region ?? null,
    })),
    processors: s.processors.map(
      (p): ProcessorView => ({
        name: p.name,
        retention: p.retention,
        uses: p.uses.map((u) => ({
          use: u.use,
          model: u.model ?? null,
          hosts: u.hosts ?? [],
          minPrecision: u.min_precision ?? null,
          zeroRetention: u.zero_retention,
        })),
      }),
    ),
    backupDays: s.backup_days,
  };
}

export function createSdkSettings(client: V2Client): SettingsSource {
  return {
    async notifications({ signal } = {}) {
      return notificationsOf(
        await client.v2.settings.notifications({ signal }),
      );
    },
    async updateNotifications({ change, version, idempotencyKey }) {
      return notificationsOf(
        await client.v2.settings.updateNotifications(
          notificationsInput(change),
          { idempotencyKey, ifMatch: version },
        ),
      );
    },
    async security({ signal } = {}) {
      return securityOf(await client.v2.settings.security({ signal }));
    },
  };
}
