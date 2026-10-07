/**
 * Sentences Settings › Account builds: how a session is named, when it
 * was last used, what a passkey's row says, why a provider wasn't linked.
 * Pure (catalogue in, string out), so the rules are unit-tested in both
 * locales.
 */
import { interpolate } from "@/i18n/interpolate";
import type { Locale } from "@/i18n";
import type { Translations } from "@/i18n/locales/en";
import { formatShortDate } from "./copy";
import type { PasskeyView, SessionView } from "./data/account";

export type AccountCopy = Translations["ledger"]["account"];

const CLI = /^memax CLI (\S+)(?: on (.+?))?(?: \(([^)]*)\))?$/;

/**
 * A session's name and its quiet second part, as Account.png writes
 * them: "This browser"; "memax CLI on ziyang-mbp" · "2.0.0"; "Chrome on
 * macOS"; "Claude over MCP".
 */
export function sessionLabel(
  copy: AccountCopy,
  s: SessionView,
): { label: string; meta: string | null } {
  if (s.current && s.surface === "web") {
    return { label: copy.sessions.thisBrowser, meta: null };
  }
  const cli = CLI.exec(s.client);
  if (cli) {
    return {
      label: cli[2]
        ? interpolate(copy.sessions.cliOn, { host: cli[2] })
        : "memax CLI",
      meta: cli[1] ?? null,
    };
  }
  if (s.surface === "mcp") {
    return {
      label: interpolate(copy.sessions.mcp, { client: s.client }),
      meta: null,
    };
  }
  return { label: s.client, meta: null };
}

/**
 * The icon a session's row shows: the web, a phone, a terminal or an
 * agent. Ledger's icon names, spelled here: lib/ stays free of the Ledger.
 */
export type SessionIcon = "globe" | "today" | "terminal" | "agents";

export function sessionIcon(s: SessionView): SessionIcon {
  if (s.surface === "cli" || s.surface === "device") return "terminal";
  if (s.surface === "mcp") return "agents";
  return /\b(iOS|iPadOS|Android)\b/.test(s.client) ? "today" : "globe";
}

/** When a session was last used: "now", "2 min ago", "3 h ago", "yesterday", "4 days ago", then the date. */
export function lastUsedText(
  copy: AccountCopy,
  iso: string,
  now: Date,
  timeZone: string,
  locale: Locale,
): string {
  const minutes = Math.floor((now.getTime() - new Date(iso).getTime()) / 60000);
  const c = copy.sessions;
  if (minutes < 1) return c.now;
  if (minutes < 60) return interpolate(c.minutesAgo, { n: minutes });
  if (minutes < 18 * 60) {
    return interpolate(c.hoursAgo, { n: Math.round(minutes / 60) });
  }
  const days = Math.round(minutes / (24 * 60));
  if (days <= 1) return c.yesterday;
  if (days < 7) return interpolate(c.daysAgo, { n: days });
  return formatShortDate(new Date(iso), timeZone, locale);
}

/** A passkey's line: "iCloud Keychain · Added Sep 2 · last used Oct 4 · Synced across your devices". */
export function passkeyMeta(
  copy: AccountCopy,
  p: PasskeyView,
  timeZone: string,
  locale: Locale,
): string {
  const date = (iso: string) =>
    formatShortDate(new Date(iso), timeZone, locale);
  const c = copy.passkeys;
  return [
    p.provider && p.provider !== p.name ? p.provider : null,
    interpolate(c.addedOn, { date: date(p.createdAt) }),
    p.lastUsedAt
      ? interpolate(c.lastUsed, { date: date(p.lastUsedAt) })
      : c.neverUsed,
    p.synced || p.backupEligible ? c.synced : c.oneDevice,
  ]
    .filter(Boolean)
    .join(" · ");
}

/** Why a provider wasn't linked (?account_link_error=). */
export function linkErrorText(copy: AccountCopy, code: string): string {
  const errors = copy.signIn.linkErrors as Record<string, string>;
  return errors[code] ?? errors.other!;
}
