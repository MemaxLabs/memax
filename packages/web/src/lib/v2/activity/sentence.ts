/**
 * The words of an Activity row, built from a receipt (catalogue in,
 * tokens out) so the sentences are unit-tested in both locales. The
 * React side sets {actor} in bold and {quote} in the serif; everything
 * else is plain text.
 */
import { interpolate } from "@/i18n/interpolate";
import type { Locale } from "@/i18n";
import type { Translations } from "@/i18n/locales/en";
import { joinList } from "../copy";
import type { ActivityEntry, ViaPart } from "../data/activity";
import type { Autonomy, AgentSurface } from "../data/agents";

export type ActivityCopy = Translations["ledger"]["activity"];

export type Token =
  | { kind: "actor"; text: string }
  | { kind: "quote"; text: string }
  | { kind: "text"; text: string };

export interface Sentence {
  template: string;
  values: Record<string, Token>;
}

/** Names the catalogue doesn't hold: agents (the Ledger registry), levels and surfaces. */
export interface Names {
  locale: Locale;
  agent: (key: string) => string;
  level: (autonomy: Autonomy) => string;
  surface: (surface: AgentSurface) => string;
}

const text = (value: string | number): Token => ({
  kind: "text",
  text: String(value),
});

/** Ends a sentence the person wrote, so the template never adds its own stop. */
function closed(value: string, locale: Locale): string {
  const trimmed = value.trim();
  if (/[.?!。？！]$/.test(trimmed)) return trimmed;
  return `${trimmed}${locale === "zh" ? "。" : "."}`;
}

/** Who did it, as the row names them. */
export function actorName(
  copy: ActivityCopy,
  entry: ActivityEntry,
  names: Names,
): string {
  const a = entry.actor;
  switch (a.kind) {
    case "you":
      return copy.actors.you;
    case "person":
      return a.name ?? copy.actors.person;
    case "agent":
      return names.agent(a.agent);
    case "dream":
      return copy.actors.dream;
    case "memax":
      return copy.actors.memax;
    case "repository":
      return entry.action === "drifted"
        ? copy.actors.handEdit
        : copy.actors.repository;
  }
}

/** One of the judge's folds: a `merged` receipt by Memax, pointing at the kept memory. */
export function isFold(entry: ActivityEntry): boolean {
  return (
    entry.action === "merged" &&
    entry.actor.kind === "memax" &&
    entry.source?.kind === "memory"
  );
}

/**
 * Whether a fold in the loaded log can still be undone: inside its 14
 * days, and no `undid` receipt cites it.
 */
export function foldUndoable(
  entry: ActivityEntry,
  entries: readonly ActivityEntry[],
  now: Date,
  windowMs: number,
): boolean {
  if (!isFold(entry)) return false;
  if (Date.parse(entry.at) + windowMs <= now.getTime()) return false;
  return !entries.some(
    (e) =>
      e.action === "undid" &&
      e.source?.kind === "receipt" &&
      e.source.ref === entry.id,
  );
}

/** An edition's number from its display ID: "D-0214" is 214. */
export function editionNumber(ref: string): number | string {
  const m = /^D-0*(\d+)$/.exec(ref);
  return m ? Number(m[1]) : ref;
}

function pick(one: string, other: string, n: number) {
  return interpolate(n === 1 ? one : other, { n });
}

/** The row's sentences: usually one, two when a compile skipped a file or a reject gave a reason. */
export function activitySentences(
  copy: ActivityCopy,
  entry: ActivityEntry,
  names: Names,
): Sentence[] {
  const s = copy.sentence;
  const name = actorName(copy, entry, names);
  // Chinese templates run straight on from {actor} ("你忘记了"); a Latin
  // name keeps the space mixed-script copy puts before CJK ("Codex 问了").
  const spaced =
    names.locale === "zh" && /[A-Za-z0-9.]$/.test(name) ? `${name} ` : name;
  const actor: Token = { kind: "actor", text: spaced };
  const d = entry.detail;
  const one = (template: string, values: Record<string, Token> = {}) => ({
    template,
    values: { actor, ...values },
  });
  const quote = d?.kind === "quote" ? d.text : null;
  const quoted = (withQuote: string, plain: string): Sentence =>
    quote
      ? one(withQuote, {
          quote: { kind: "quote", text: closed(quote, names.locale) },
        })
      : one(plain);
  const agentObject = text(names.agent(entry.object.ref));

  switch (entry.action) {
    case "asked":
      return [
        d?.kind === "question"
          ? one(s.asked, { quote: { kind: "quote", text: d.text } })
          : one(s.askedPlain),
      ];
    case "verified":
      return [one(d?.kind === "verified" ? s.verified : s.verifiedPlain)];
    case "compiled": {
      if (d?.kind !== "compiled") return [one(s.compiledPlain)];
      const targets = joinList(
        d.targets.map((t) => (t === "chatgpt" ? s.chatgptProject : t)),
        names.locale,
      );
      const out: Sentence[] = [one(s.compiled, { targets: text(targets) })];
      if (d.skipped) {
        out.push({
          template: s.compiledSkipped,
          values: { agent: text(names.agent(d.skipped.agent)) },
        });
      }
      return out;
    }
    case "handed_off":
      return [
        d?.kind === "handoff"
          ? one(d.carries === 1 ? s.handedOffOne : s.handedOff, {
              agent: text(names.agent(d.to)),
              n: text(d.carries),
            })
          : one(s.handedOffPlain),
      ];
    case "read": {
      if (d?.kind !== "read") {
        return [one(s.other, { ref: text(entry.object.ref) })];
      }
      // A compile read (a session-start digest or load) is the Brief.
      const template = d.brief
        ? d.memories === 0
          ? s.readBriefOnly
          : d.memories === 1
            ? s.readBriefOne
            : s.readBrief
        : d.memories === 1
          ? s.readOne
          : s.read;
      return [one(template, { n: text(d.memories) })];
    }
    case "kept":
      return [quoted(s.kept, s.keptPlain)];
    case "proposed":
      return [quoted(s.proposed, s.proposedPlain)];
    case "rejected": {
      const out = [quoted(s.rejected, s.rejectedPlain)];
      if (entry.reason) {
        out.push({
          template: s.reason,
          values: { reason: text(closed(entry.reason, names.locale)) },
        });
      }
      return out;
    }
    case "edited":
      if (d?.kind === "brief") {
        return [
          one(s.editedBrief, {
            facts: text(pick(s.briefFactsOne, s.briefFacts, d.facts)),
          }),
        ];
      }
      return [quoted(s.edited, s.editedPlain)];
    case "merged":
      if (d?.kind === "dream") {
        return [
          one(s.dream, {
            notes: text(pick(s.notesOne, s.notes, d.notes)),
            facts: text(pick(s.factsOne, s.facts, d.facts)),
            stale: text(d.stale),
            faded: text(d.faded),
          }),
        ];
      }
      // One of the judge's folds: Memax merged a proposal into a kept memory.
      if (isFold(entry)) {
        return [
          one(s.folded, {
            ref: text(entry.object.ref),
            into: text(entry.source!.ref),
          }),
        ];
      }
      // One of an edition's dedupes: a proposal folded into the one it repeats.
      if (entry.actor.kind === "dream" && entry.source?.kind === "dream") {
        return [one(s.dreamDuplicate, { ref: text(entry.object.ref) })];
      }
      return [one(entry.actor.kind === "dream" ? s.dreamPlain : s.merged)];
    case "flagged":
      return [one(s.flagged)];
    case "resolved":
      return [one(s.resolved)];
    case "faded":
      return [one(s.faded)];
    case "restored":
      return [one(s.restored)];
    case "forgot": {
      // Name only what held it: nothing compiled may have, or no agent.
      if (d?.kind !== "forgot" || d.files + d.agents === 0) {
        return [one(s.forgotPlain)];
      }
      const files = text(pick(s.filesOne, s.files, d.files));
      const agents = text(pick(s.agentsOne, s.agents, d.agents));
      if (d.files === 0) return [one(s.forgotAgents, { agents })];
      if (d.agents === 0) return [one(s.forgotFiles, { files })];
      return [one(s.forgot, { files, agents })];
    }
    case "moved":
      return [one(s.moved)];
    case "answered":
      return [one(s.answered)];
    case "withdrawn":
      return [one(s.withdrawn, { ref: text(entry.object.ref) })];
    case "undid":
      return [one(s.undid)];
    case "drifted":
      return [
        one(s.drifted, {
          path: text(d?.kind === "drift" ? d.path : entry.object.ref),
        }),
      ];
    case "connected":
      return [
        d?.kind === "autonomy"
          ? one(s.connected, {
              agent: agentObject,
              level: text(names.level(d.level)),
            })
          : one(s.connectedPlain, { agent: agentObject }),
      ];
    case "autonomy_changed":
      return [
        d?.kind === "autonomy"
          ? one(s.autonomy, {
              agent: agentObject,
              level: text(names.level(d.level)),
            })
          : one(s.autonomyPlain, { agent: agentObject }),
      ];
    case "paused":
      return [one(s.paused, { agent: agentObject })];
    case "resumed":
      return [one(s.resumed, { agent: agentObject })];
    case "disconnected":
      return [one(s.disconnected, { agent: agentObject })];
    case "revised":
      return [one(s.revised, { ref: text(entry.object.ref) })];
    case "configured":
      return [one(s.configured, { ref: text(entry.object.ref) })];
    case "requested":
      return [one(s.requested, { ref: text(entry.object.ref) })];
    case "delivered":
      return [one(s.delivered, { ref: text(entry.object.ref) })];
    case "observed":
      return [one(s.observed, { ref: text(entry.object.ref) })];
    case "pulled":
      return [one(s.pulled, { ref: text(entry.object.ref) })];
    case "overwritten":
      return [one(s.overwritten, { ref: text(entry.object.ref) })];
    case "stopped":
      return [one(s.stopped, { ref: text(entry.object.ref) })];
    case "judged":
      return [one(s.judged, { ref: text(entry.object.ref) })];
    case "linked":
      return [one(s.linked, { ref: text(entry.object.ref) })];
    case "superseded":
      return [one(s.superseded, { ref: text(entry.object.ref) })];
    // The judge put a Write agent's write back in Review; the receipt's
    // source is the decision in force it contradicts.
    case "returned":
      return [
        entry.source?.kind === "memory"
          ? one(s.returned, {
              ref: text(entry.object.ref),
              decision: text(entry.source.ref),
            })
          : one(s.returnedPlain, { ref: text(entry.object.ref) }),
      ];
    case "drafted":
      return [one(s.drafted, { ref: text(entry.object.ref) })];
    case "purged":
      return [one(s.purged, { ref: text(entry.object.ref) })];
    case "forget_requested":
      return [one(s.forgetRequested, { ref: text(entry.object.ref) })];
    case "forget_declined":
      return [one(s.forgetDeclined, { ref: text(entry.object.ref) })];
    case "exported":
      return [one(s.exported)];
    // Switch to V2 (on the space's own stream).
    case "noted":
      return [one(s.noted)];
    case "switched":
      return [one(s.switched)];
    case "switched_back":
      return [one(s.switchedBack)];
    // Dream's edition (D-), and notes it folded into a memory as lineage.
    case "published":
      return [one(s.published, { n: text(editionNumber(entry.object.ref)) })];
    case "folded":
      return [one(s.foldedNotes, { ref: text(entry.object.ref) })];
    default: {
      // Every verb the API can send has words above; this fails to compile
      // when the spec gains one. A newer server can still send a verb this
      // client predates, so the runtime fallback stays.
      const unhandled: never = entry.action;
      void unhandled;
      return [one(s.other, { ref: text(entry.object.ref) })];
    }
  }
}

/** The sentences as plain text, for a row's accessible description and tests. */
export function sentenceText(sentences: Sentence[], locale: Locale): string {
  return sentences
    .map(({ template, values }) =>
      template.replace(/\{(\w+)\}/g, (whole, key: string) => {
        const token = values[key];
        if (!token) return whole;
        return token.kind === "quote" ? `“${token.text}”` : token.text;
      }),
    )
    .join(locale === "zh" ? "" : " ");
}

/** The "via" column: "MCP · session 7c2f", "Review · from Cursor". */
export function viaText(
  copy: ActivityCopy,
  parts: readonly ViaPart[],
  names: Names,
): string {
  const v = copy.via;
  return parts
    .map((p) => {
      switch (p.kind) {
        case "via":
          return v[p.via];
        case "session":
          return interpolate(p.cloud ? v.cloud : v.session, { ref: p.ref });
        case "from":
          return interpolate(v.from, { agent: names.agent(p.agent) });
        case "surface":
          return names.surface(p.surface);
        case "targets":
          return interpolate(v.targets, { done: p.done, total: p.total });
        case "edition":
          return interpolate(v.edition, { n: p.n });
        case "repository":
          return v.repository;
        case "source":
          return p.ref;
        case "passkey":
          return v.passkey;
      }
    })
    .join(" · ");
}

// Days and times, in the viewer's zone.

const DATE_TAGS: Record<Locale, string> = { en: "en-US", zh: "zh-CN" };

/** "2026-10-05" in the zone: the key rows group under. */
export function dayKey(iso: string | Date, timeZone: string): string {
  return new Intl.DateTimeFormat("en-CA", {
    timeZone,
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
  }).format(typeof iso === "string" ? new Date(iso) : iso);
}

/** "Today", "Yesterday", then "Saturday, October 3". */
export function dayLabel(
  copy: ActivityCopy,
  key: string,
  now: Date,
  timeZone: string,
  locale: Locale,
): string {
  const today = dayKey(now, timeZone);
  const yesterday = dayKey(new Date(now.getTime() - 86_400_000), timeZone);
  if (key === today) return copy.days.today;
  if (key === yesterday) return copy.days.yesterday;
  // Noon UTC on that day, formatted in UTC, can't slip into a neighbour.
  return new Intl.DateTimeFormat(DATE_TAGS[locale], {
    timeZone: "UTC",
    weekday: "long",
    month: "long",
    day: "numeric",
  }).format(new Date(`${key}T12:00:00Z`));
}

/**
 * "Times in Vancouver (PT)"; in Chinese, the zone's own name ("北美太平洋时间").
 * Named as of `at`, the moment the times are read against: a zone's name
 * can change with the date. When the short name needs a place to be
 * unambiguous ("PT (Canada)": from November 2026 British Columbia stays
 * on UTC−7 while the rest of Pacific Time goes back to UTC−8), the
 * English label uses the offset instead ("GMT-7"), which is exact and
 * doesn't nest parentheses.
 */
export function zoneLabel(
  copy: ActivityCopy,
  timeZone: string,
  locale: Locale,
  at: Date = new Date(),
): string {
  const zoneName = (style: "shortGeneric" | "longGeneric" | "shortOffset") =>
    new Intl.DateTimeFormat(DATE_TAGS[locale], {
      timeZone,
      timeZoneName: style,
    })
      .formatToParts(at)
      .find((p) => p.type === "timeZoneName")?.value ?? timeZone;
  if (locale === "zh") {
    return interpolate(copy.timesIn, { zone: zoneName("longGeneric") });
  }
  const generic = zoneName("shortGeneric");
  const zone = generic.includes("(") ? zoneName("shortOffset") : generic;
  const city = timeZone.includes("/")
    ? timeZone.split("/").pop()!.replace(/_/g, " ")
    : null;
  return city
    ? interpolate(copy.timesIn, { city, zone })
    : interpolate(copy.timesInZone, { zone });
}
