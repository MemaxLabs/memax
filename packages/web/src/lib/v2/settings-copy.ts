/**
 * Settings › Notifications and Settings › Security in words: pure,
 * catalogue in and sentences out, so both locales are unit-tested. The
 * Security page says only what the server's configuration says (the
 * /v2/security posture), with names from the catalogue and a raw slug
 * for anything it doesn't know, so a self-hosted server's own choices
 * still read correctly.
 */
import { interpolate } from "@/i18n/interpolate";
import type { Locale } from "@/i18n";
import type { Translations } from "@/i18n/locales/en";
import { count, joinList, joinSentences } from "./copy";
import type { AgentConnectionView, Autonomy } from "./data/agents";
import { AUTONOMY_LEVELS } from "./data/agents";
import {
  DREAM_HOUR,
  emailedNow,
  inQuietHours,
  type NotificationChoiceView,
  type NotificationSettingsView,
  type ProcessorView,
  type SecurityView,
} from "./data/settings";

export type SettingsCopy = Translations["ledger"]["settings"];
type NotificationsCopy = SettingsCopy["notifications"];
type SecurityCopy = SettingsCopy["security"];

/** A notification row's title and the line under it. */
export function eventText(
  copy: NotificationsCopy,
  choice: NotificationChoiceView,
  settings: NotificationSettingsView,
): { title: string; meta: string } {
  const e = copy.events[choice.event];
  switch (choice.event) {
    case "morning_edition": {
      // The email waits for the end of quiet hours that cover Dream's night.
      const q = settings.quietHours;
      const held = choice.email && inQuietHours(q, DREAM_HOUR);
      const morning = copy.events.morning_edition;
      return {
        title: e.title,
        meta: held
          ? interpolate(morning.meta, { time: q.until })
          : morning.metaReady,
      };
    }
    case "review_waiting": {
      const r = copy.events.review_waiting;
      const n = settings.reviewAfterDays;
      return {
        title: n === 1 ? r.title : interpolate(r.titleDays, { n }),
        meta: r.meta,
      };
    }
    default:
      return { title: e.title, meta: e.meta };
  }
}

/** The line under the table: what Memax emails today, and the channels that wait. */
export function deliveryNote(
  copy: NotificationsCopy,
  settings: NotificationSettingsView,
  locale: Locale,
): string {
  const sent = emailedNow(settings).map((e) => copy.events[e].short);
  return joinSentences(
    [
      sent.length > 0
        ? interpolate(copy.sentNow, { list: joinList(sent, locale) })
        : copy.sentNone,
      copy.channelsLater,
    ],
    locale,
  );
}

/** The quiet hours' zone line. */
export function quietZoneText(
  copy: NotificationsCopy,
  settings: NotificationSettingsView,
): string {
  return settings.timeZoneSource === "default"
    ? copy.quiet.zoneDefault
    : interpolate(copy.quiet.zone, { zone: settings.timeZone });
}

/**
 * Names of companies, services and models, the same in every language.
 * They're names, not copy, so they live here like the agents' registry
 * (and the voice lint, which bans "AI" in copy, never reads them). An
 * unknown slug reads as itself.
 */
export const NAMES = {
  processors: {
    openrouter: "OpenRouter",
    anthropic: "Anthropic",
    voyage: "Voyage AI",
    resend: "Resend",
  },
  providers: {
    neon: "Neon Postgres",
    fly: "Fly.io",
    r2: "Cloudflare R2",
    cloudflare: "Cloudflare Workers",
    vercel: "Vercel",
    upstash: "Upstash",
  },
  hosts: {
    together: "Together AI",
    baseten: "Baseten",
    coreweave: "CoreWeave",
    deepinfra: "DeepInfra",
    "amazon-bedrock": "Amazon Bedrock",
    "google-vertex": "Google Vertex AI",
  },
  models: {
    "deepseek/deepseek-v4.1-flash": "DeepSeek V4.1 Flash",
    "anthropic/claude-haiku-4.5": "Claude Haiku 4.5",
    "anthropic/claude-sonnet-5.5": "Claude Sonnet 5.5",
  },
} as const satisfies Record<string, Record<string, string>>;

function named(names: Readonly<Record<string, string>>, key: string): string {
  return names[key] ?? key;
}

/** "Neon Postgres, us-west-2", or the provider alone when it places the data. */
export function placeText(
  copy: SecurityCopy,
  place: SecurityView["residency"][number],
): string {
  const provider = named(NAMES.providers, place.provider);
  return place.region
    ? interpolate(copy.residency.at, { provider, region: place.region })
    : provider;
}

/** One processor as the table reads it: its name, what it gets, what it keeps. */
export interface ProcessorRow {
  name: string;
  gets: string[];
  keeps: string;
  /** The retention isn't confirmed: the row is said, not hidden. */
  unconfirmed: boolean;
}

export function processorRows(
  copy: SecurityCopy,
  processors: ProcessorView[],
  locale: Locale,
): ProcessorRow[] {
  const p = copy.processors;
  return processors.map((proc) => {
    const name = named(NAMES.processors, proc.name);
    const gets = proc.uses.map((u) => {
      const use = p.uses[u.use];
      if (!u.model) return use;
      const parts = [
        interpolate(p.model, {
          use,
          model: named(NAMES.models, u.model),
        }),
      ];
      if (u.hosts.length > 0) {
        parts.push(
          interpolate(p.hosts, {
            hosts: joinList(
              u.hosts.map((h) => named(NAMES.hosts, h)),
              locale,
            ),
          }),
        );
      }
      if (u.minPrecision) {
        parts.push(interpolate(p.precision, { precision: u.minPrecision }));
      }
      return parts.join(locale === "zh" ? "，" : ", ");
    });
    return {
      name,
      gets,
      keeps: interpolate(p.retention[proc.retention], { name }),
      unconfirmed: proc.retention === "unconfirmed",
    };
  });
}

/** What Forget can't reach, the providers that may keep words among them. */
export function unreachableLines(
  copy: SecurityCopy,
  security: SecurityView,
  locale: Locale,
): string[] {
  const u = copy.forget.unreachable;
  const lines = [
    count(u.backupsOne, u.backups, security.backupDays, {
      days: security.backupDays,
    }),
    u.git,
    u.agentMemory,
    u.handEdits,
    u.copies,
  ];
  const keeping = security.processors
    .filter((p) => p.retention !== "zero")
    .map((p) => named(NAMES.processors, p.name));
  if (keeping.length > 0) {
    lines.push(interpolate(u.providers, { names: joinList(keeping, locale) }));
  }
  return lines;
}

/** The most an agent may do in any of its spaces. */
function highest(agent: AgentConnectionView): Autonomy {
  let top: Autonomy = "read";
  for (const s of agent.spaces) {
    if (AUTONOMY_LEVELS.indexOf(s.autonomy) > AUTONOMY_LEVELS.indexOf(top)) {
      top = s.autonomy;
    }
  }
  return top;
}

/** "3 agents connected across 4 spaces. At most: 1 read, 2 propose. 1 paused." */
export function agentSummary(
  copy: SecurityCopy,
  agents: AgentConnectionView[],
  locale: Locale,
): string {
  const a = copy.agents;
  const live = agents.filter((x) => x.state !== "disconnected");
  if (live.length === 0) return a.none;
  const spaces = new Set(live.flatMap((x) => x.spaces.map((s) => s.spaceId)));
  const levels = AUTONOMY_LEVELS.map((level) => ({
    level,
    n: live.filter((x) => highest(x) === level).length,
  }))
    .filter((l) => l.n > 0)
    .map((l) => interpolate(a.level[l.level], { n: l.n }));
  const paused = live.filter((x) => x.state === "paused").length;
  return joinSentences(
    [
      interpolate(a.summary, {
        agents: count(a.agentsOne, a.agents, live.length),
        spaces: count(a.spacesOne, a.spaces, spaces.size),
      }),
      interpolate(a.levels, { list: joinList(levels, locale) }),
      paused > 0 ? interpolate(a.paused, { n: paused }) : null,
    ],
    locale,
  );
}
