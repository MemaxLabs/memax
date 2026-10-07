import { CommandFailedError } from "./command-error";
import type { DreamSource } from "./dream";
import {
  applyChange,
  NOTIFICATION_EVENTS,
  type NotificationEvent,
  type NotificationSettingsView,
  type SecurityView,
  type SettingsSource,
} from "./settings";

/**
 * The demo's settings: Notifications.png's choices (the email column as
 * drawn; phone and Slack aren't channels yet), Memax cloud's posture as
 * the server's defaults describe it, and a session copy that edits
 * change. The morning edition's email is the demo Dream's setting, one
 * copy, as on the server; a change there moves the version too.
 */

/** Who gets an email by default, and what shows in the app (the board's In app column). */
const DEFAULTS: Record<NotificationEvent, { inApp: boolean; email: boolean }> =
  {
    decision_gate: { inApp: true, email: true },
    morning_edition: { inApp: true, email: true },
    review_waiting: { inApp: true, email: true },
    drift: { inApp: true, email: false },
    stale: { inApp: true, email: false },
    write_held: { inApp: true, email: true },
    weekly_summary: { inApp: false, email: true },
    forget_done: { inApp: true, email: false },
    agent_changed: { inApp: true, email: true },
  };

/** Memax cloud (plan 25 §5.16, D14): the server's defaults with every key set. */
export const DEMO_SECURITY: SecurityView = {
  assurance: "human_web",
  residency: [
    { holds: "database", provider: "neon", region: "us-west-2" },
    { holds: "compute", provider: "fly", region: "sjc" },
    { holds: "objects", provider: "r2", region: null },
    { holds: "edge", provider: "cloudflare", region: null },
  ],
  processors: [
    {
      name: "openrouter",
      retention: "zero",
      uses: [
        ...(["judge", "ask", "dream"] as const).map((use) => ({
          use,
          model: "deepseek/deepseek-v4.1-flash",
          hosts: ["together", "baseten", "coreweave", "deepinfra"],
          minPrecision: "fp8",
          zeroRetention: true,
        })),
        ...(["judge_fallback", "dream_fallback"] as const).map((use) => ({
          use,
          model: "anthropic/claude-haiku-4.5",
          hosts: ["amazon-bedrock", "google-vertex"],
          minPrecision: "fp8",
          zeroRetention: true,
        })),
        ...(["judge_strong", "dream_strong"] as const).map((use) => ({
          use,
          model: "anthropic/claude-sonnet-5.5",
          hosts: ["google-vertex"],
          minPrecision: "fp8",
          zeroRetention: true,
        })),
      ],
    },
    {
      name: "voyage",
      retention: "unconfirmed",
      uses: [
        {
          use: "embeddings",
          model: "voyage-4",
          hosts: [],
          minPrecision: null,
          zeroRetention: false,
        },
        {
          use: "queries",
          model: "voyage-4-lite",
          hosts: [],
          minPrecision: null,
          zeroRetention: false,
        },
        {
          use: "rerank",
          model: "rerank-3-lite",
          hosts: [],
          minPrecision: null,
          zeroRetention: false,
        },
      ],
    },
    {
      name: "resend",
      retention: "provider_terms",
      uses: [
        {
          use: "email",
          model: null,
          hosts: [],
          minPrecision: null,
          zeroRetention: false,
        },
      ],
    },
  ],
  backupDays: 7,
};

export function createDemoSettings({
  dream,
  commandDelayMs = 0,
}: {
  dream: DreamSource;
  commandDelayMs?: number;
}): SettingsSource {
  const wait = () =>
    commandDelayMs > 0
      ? new Promise((r) => setTimeout(r, commandDelayMs))
      : Promise.resolve();
  const morning = () => dream.peekSettings?.()?.morningEmail ?? true;
  let stored: NotificationSettingsView = {
    version: 1,
    timeZone: dream.peekSettings?.()?.timeZone ?? "America/Vancouver",
    timeZoneSource: dream.peekSettings?.()?.timeZoneSource ?? "observed",
    events: NOTIFICATION_EVENTS.map((event) => ({
      event,
      ...DEFAULTS[event],
      emailSent: event === "morning_edition",
    })),
    quietHours: { on: true, from: "20:00", until: "08:00", gatesThrough: true },
    reviewAfterDays: 1,
  };
  let seenMorning = morning();

  /** The settings now, with Dream's copy of the morning email. */
  const current = (): NotificationSettingsView => {
    const m = morning();
    if (m !== seenMorning) {
      // Dream's settings changed it (Settings › Account): a new version.
      seenMorning = m;
      stored = { ...stored, version: stored.version + 1 };
    }
    const ds = dream.peekSettings?.();
    return {
      ...stored,
      timeZone: ds?.timeZone ?? stored.timeZone,
      timeZoneSource: ds?.timeZoneSource ?? stored.timeZoneSource,
      events: stored.events.map((e) =>
        e.event === "morning_edition" ? { ...e, email: m } : e,
      ),
    };
  };

  return {
    peekNotifications: current,
    peekSecurity: () => DEMO_SECURITY,
    async notifications() {
      return current();
    },
    async updateNotifications({ change, version }) {
      await wait();
      const now = current();
      if (version !== now.version) {
        throw new CommandFailedError({
          kind: "clash",
          currentVersion: now.version,
        });
      }
      const next = applyChange(now, change);
      const m = change.email?.morning_edition;
      if (m !== undefined && m !== morning()) {
        await dream.updateSettings({ morningEmail: m, idempotencyKey: "demo" });
        seenMorning = m;
      }
      stored = { ...next, version: now.version + 1 };
      return current();
    },
    async security() {
      return DEMO_SECURITY;
    },
  };
}
