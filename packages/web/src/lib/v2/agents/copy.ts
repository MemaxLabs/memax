/**
 * Sentences the agent screens build from data: why a command didn't go
 * through, what changed, when an agent was last seen. Pure (catalogue
 * in, string out) so both locales are unit-tested.
 */
import { interpolate } from "@/i18n/interpolate";
import type { Locale } from "@/i18n";
import type { Translations } from "@/i18n/locales/en";
import type { AgentRefusal, Autonomy } from "../data/agents";
import type { SpaceRole } from "../data/types";
import type { Unavailable } from "./autonomy";

export type AgentsCopy = Translations["ledger"]["agents"];

export interface RefusalText {
  text: string;
  /** Raising needs a web session Memax can verify: offer to sign in again. */
  signIn: boolean;
}

/**
 * Why a command on an agent didn't go through, and what to do. `level`
 * is where the agent stays; `dev` adds the operator's hint (no
 * WEB_SURFACE_SECRET in local development).
 */
export function refusalText(
  copy: AgentsCopy,
  refusal: AgentRefusal,
  {
    agent,
    level,
    space,
    role,
    command = "autonomy",
    dev = false,
  }: {
    agent: string;
    level: string;
    space: string;
    role: SpaceRole;
    command?: "autonomy" | "pause" | "resume" | "disconnect";
    dev?: boolean;
  },
): RefusalText {
  const r = copy.refused;
  const vars = { agent, level, space };
  const say = (template: string, signIn = false): RefusalText => ({
    text: interpolate(template, vars),
    signIn,
  });
  switch (refusal) {
    case "needs_web": {
      const base = interpolate(
        command === "resume" ? r.needsWebResume : r.needsWeb,
        vars,
      );
      return { text: dev ? `${base} ${r.needsWebDev}` : base, signIn: true };
    }
    case "key_max_propose":
      return say(r.keyMaxPropose);
    case "not_your_agent":
      return say(r.notYours);
    case "not_allowed":
      return say(role === "viewer" ? r.viewer : r.notAllowed);
    case "person_must_manage":
      return say(r.personMustManage);
    case "not_member":
      return say(r.notMember);
    case "surface_unverified":
      return say(r.surfaceUnverified);
    case "invalid_transition":
      return say(r.invalidTransition);
    case "not_found":
      return say(r.notFound);
    case "failed":
      return say(command === "autonomy" ? r.failed : r.failedCommand);
  }
}

/** The toast once a level is set: "Codex proposes in memax-v2 now. Its writes wait in Review." */
export function changedText(
  copy: AgentsCopy,
  autonomy: Autonomy,
  vars: { agent: string; space: string },
): string {
  return interpolate(copy.changed[autonomy], vars);
}

/** A greyed level's tooltip. */
export function unavailableText(
  copy: AgentsCopy,
  reason: Unavailable,
  agent: string,
): string {
  return interpolate(copy.unavailable[reason], { agent });
}

const DATE_TAGS: Record<Locale, string> = { en: "en-US", zh: "zh-CN" };

function dayKey(date: Date, timeZone: string): string {
  return new Intl.DateTimeFormat("en-CA", {
    timeZone,
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
  }).format(date);
}

/**
 * "2 min ago", "3 h ago", "Yesterday", then "Sep 28". Relative within a
 * day, absolute after. `lower` for mid-sentence and table use ("yesterday").
 */
export function agoText(
  copy: AgentsCopy,
  iso: string | null,
  now: Date,
  timeZone: string,
  locale: Locale,
  { lower = false }: { lower?: boolean } = {},
): string {
  const a = copy.ago;
  if (!iso) return a.never;
  const then = new Date(iso);
  const minutes = Math.floor((now.getTime() - then.getTime()) / 60000);
  if (minutes < 1) return a.justNow;
  if (minutes < 60) return interpolate(a.minutes, { n: minutes });
  const yesterday = dayKey(new Date(now.getTime() - 86_400_000), timeZone);
  if (minutes < 24 * 60 && dayKey(then, timeZone) !== yesterday) {
    return interpolate(a.hours, { n: Math.floor(minutes / 60) });
  }
  if (dayKey(then, timeZone) === yesterday) {
    return lower ? a.yesterdayLower : a.yesterday;
  }
  return new Intl.DateTimeFormat(DATE_TAGS[locale], {
    timeZone,
    month: "short",
    day: "numeric",
  }).format(then);
}
