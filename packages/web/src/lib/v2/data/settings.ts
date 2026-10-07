/**
 * A person's settings beyond the record (epic 2.6): Settings ›
 * Notifications (Notifications.png) and Settings › Security. Part of
 * LedgerDataSource (source.ts) as `source.settings`; settings-sdk.ts
 * (memax.v2.settings) and settings-demo.ts implement it.
 *
 * Names follow the /v2 contract (NotificationSettings, Security). No
 * words live here: the pages word everything from the catalogue
 * (settings-en.ts, settings-zh.ts).
 */

/** Something Memax can tell a person about, in the page's order (spec NotificationEvent). */
export type NotificationEvent =
  | "decision_gate"
  | "morning_edition"
  | "review_waiting"
  | "drift"
  | "stale"
  | "write_held"
  | "weekly_summary"
  | "forget_done"
  | "agent_changed";

export const NOTIFICATION_EVENTS: readonly NotificationEvent[] = [
  "decision_gate",
  "morning_edition",
  "review_waiting",
  "drift",
  "stale",
  "write_held",
  "weekly_summary",
  "forget_done",
  "agent_changed",
];

/** How one event reaches the person. */
export interface NotificationChoiceView {
  event: NotificationEvent;
  /** It always shows in the app; not a choice. */
  inApp: boolean;
  /** Their choice, or the default. */
  email: boolean;
  /** Memax sends this email today; otherwise the choice waits for it. */
  emailSent: boolean;
}

/** When email waits, in the person's time zone; across midnight when `from` is later. */
export interface QuietHoursView {
  on: boolean;
  from: string;
  until: string;
  /** Decision gates still come through. */
  gatesThrough: boolean;
}

export interface NotificationSettingsView {
  /** The settings' version: what an edit sends as If-Match. */
  version: number;
  /** Dream's zone (Settings › Account); the quiet hours are read in it. */
  timeZone: string;
  timeZoneSource: "default" | "observed" | "set";
  events: NotificationChoiceView[];
  quietHours: QuietHoursView;
  /** Days a proposal waits before the daily Review reminder. */
  reviewAfterDays: number;
}

/** An edit: only what it names changes. */
export interface NotificationChange {
  email?: Partial<Record<NotificationEvent, boolean>>;
  quietHours?: Partial<QuietHoursView>;
  reviewAfterDays?: number;
}

/** What a place holds (spec DataHolds). */
export type DataHolds = "database" | "compute" | "objects" | "edge";

export interface DataPlaceView {
  holds: DataHolds;
  /** A short provider name ("neon", "fly", "r2", "cloudflare", or another). */
  provider: string;
  /** The provider's region name; null when the provider places it. */
  region: string | null;
}

/** What a processor keeps (spec Retention). */
export type Retention = "zero" | "unconfirmed" | "provider_terms";

/** What Memax sends a processor (spec SubprocessorUseKind). */
export type ProcessorUseKind =
  | "judge"
  | "judge_fallback"
  | "judge_strong"
  | "ask"
  | "dream"
  | "dream_fallback"
  | "dream_strong"
  | "embeddings"
  | "queries"
  | "rerank"
  | "email";

export interface ProcessorUseView {
  use: ProcessorUseKind;
  model: string | null;
  /** The zero-retention hosts the call is pinned to, in order. */
  hosts: string[];
  minPrecision: string | null;
  zeroRetention: boolean;
}

export interface ProcessorView {
  name: "openrouter" | "anthropic" | "voyage" | "resend";
  retention: Retention;
  uses: ProcessorUseView[];
}

/** Settings › Security's data from the server; seals and agents come from their own reads. */
export interface SecurityView {
  /** What a Keep from this session counts as without a passkey check. */
  assurance: "human_web" | "human_web_verified" | "client_attested";
  /**
   * The person has a passkey, so keeps that need them ask for it and
   * count as human_web_verified.
   */
  passkeyCheck: boolean;
  residency: DataPlaceView[];
  processors: ProcessorView[];
  /** How long backups keep what Forget removed. */
  backupDays: number;
}

export interface SettingsSource {
  /** The demo answers at once, so screenshots never catch a loading frame. */
  peekNotifications?(): NotificationSettingsView | undefined;
  peekSecurity?(): SecurityView | undefined;
  notifications(input?: {
    signal?: AbortSignal;
  }): Promise<NotificationSettingsView>;
  /** Apply a change made from `version`; a newer version throws edit_clash. */
  updateNotifications(input: {
    change: NotificationChange;
    version: number;
    idempotencyKey: string;
  }): Promise<NotificationSettingsView>;
  security(input?: { signal?: AbortSignal }): Promise<SecurityView>;
}

/** A 24-hour time of day, "08:00". */
const CLOCK = /^([01][0-9]|2[0-3]):[0-5][0-9]$/;

export function isClock(value: string): boolean {
  return CLOCK.test(value);
}

function minutes(clock: string): number {
  return Number(clock.slice(0, 2)) * 60 + Number(clock.slice(3, 5));
}

/** Whether a time of day falls inside the quiet hours (always false when they're off). */
export function inQuietHours(quiet: QuietHoursView, clock: string): boolean {
  if (!quiet.on || !isClock(quiet.from) || !isClock(quiet.until)) {
    return false;
  }
  const from = minutes(quiet.from);
  const until = minutes(quiet.until);
  const at = minutes(clock);
  if (from === until) return false;
  return from < until ? at >= from && at < until : at >= from || at < until;
}

/** Dream runs at 03:00 in the owner's night; the morning email waits for quiet hours to end. */
export const DREAM_HOUR = "03:00";

/** The events Memax emails today. */
export function emailedNow(
  settings: NotificationSettingsView,
): NotificationEvent[] {
  return settings.events.filter((e) => e.emailSent).map((e) => e.event);
}

/** The settings with a change applied, as the server would (for an optimistic update). */
export function applyChange(
  settings: NotificationSettingsView,
  change: NotificationChange,
): NotificationSettingsView {
  return {
    ...settings,
    events: settings.events.map((e) =>
      change.email?.[e.event] === undefined
        ? e
        : { ...e, email: change.email[e.event]! },
    ),
    quietHours: { ...settings.quietHours, ...change.quietHours },
    reviewAfterDays: change.reviewAfterDays ?? settings.reviewAfterDays,
  };
}
