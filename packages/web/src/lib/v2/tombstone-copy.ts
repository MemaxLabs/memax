import { interpolate } from "@/i18n/interpolate";
import type { Locale } from "@/i18n";
import type { Translations } from "@/i18n/locales/en";
import {
  count,
  formatClock,
  formatShortDate,
  joinList,
  joinSentences,
  spell,
  type AppCopy,
} from "./copy";
import type {
  TombstoneStepLine,
  TombstoneView,
  UnreachableLine,
} from "./data/memories";
import { actorName, dateTime, stampOf, type RecordsCopy } from "./records-copy";

/**
 * Tombstone.png in words: the lineage of how a memory was forgotten,
 * what is gone, what is kept, and the copies Memax can't reach. Pure,
 * so the rules are unit-tested; the page draws it.
 */

export type TombstoneCopy = Translations["ledger"]["memory"]["tombstone"];

export interface TombstoneLine {
  key: string;
  /** The line's mark (a Ledger MarkState the page draws); none is a dot. */
  state?: "forgotten" | "proposed" | "conflict" | "working" | "off";
  agent?: string;
  person?: string;
  title: string;
  time: string;
  dateTime?: string;
  detail?: string;
}

export interface TombstoneContext {
  rc: RecordsCopy;
  app: AppCopy;
  timeZone: string;
  locale: Locale;
  /** The viewer's initials, for their own stamp. */
  you?: { initials: string };
  agentName: (key: string) => string;
}

/** "Forgotten Oct 3, 10:12, at your request". */
export function forgottenLabel(
  copy: TombstoneCopy,
  t: TombstoneView,
  ctx: TombstoneContext,
): string {
  const when = dateTime(ctx.rc, t.at, ctx.timeZone, ctx.locale);
  if (t.by?.kind === "memax") return interpolate(copy.forgotAgain, { when });
  if (t.by?.kind === "person" && !t.by.self) {
    return interpolate(copy.forgotBy, {
      when,
      name: actorName(t.by, ctx.rc, ctx.agentName),
    });
  }
  return interpolate(copy.forgotYou, { when });
}

/** "Kept Sep 21 · read 23 times before it was forgotten". */
export function tombstoneMeta(
  copy: TombstoneCopy,
  t: TombstoneView,
  ctx: TombstoneContext,
): string {
  const kept = t.keptAt
    ? interpolate(copy.kept, {
        date: formatShortDate(new Date(t.keptAt), ctx.timeZone, ctx.locale),
      })
    : null;
  const reads =
    t.readsBefore === 0
      ? copy.readsBeforeNone
      : count(copy.readsBeforeOne, copy.readsBefore, t.readsBefore);
  return [kept, reads].filter(Boolean).join(" · ");
}

/** A later step's time: the clock on the day it was forgotten, else the date too. */
function stepTime(at: string, t: TombstoneView, ctx: TombstoneContext): string {
  const day = (iso: string) =>
    formatShortDate(new Date(iso), ctx.timeZone, ctx.locale);
  return day(at) === day(t.at)
    ? formatClock(at, ctx.timeZone, ctx.locale)
    : dateTime(ctx.rc, at, ctx.timeZone, ctx.locale);
}

const WRITES_FILES = new Set(["local", "pr", "mcp"]);

function asked(
  copy: TombstoneCopy,
  t: TombstoneView,
  ctx: TombstoneContext,
): TombstoneLine {
  const s = copy.steps;
  const stamp = stampOf(t.by, ctx.rc, ctx.you);
  let title: string;
  let detail: string | undefined;
  if (t.carried) {
    title = interpolate(s.carried, { ref: t.carried.primary });
    detail = interpolate(s.carriedWhy[t.carried.reason], {
      ref: t.carried.primary,
    });
  } else if (t.by?.kind === "memax") {
    title = s.askedAgain;
  } else if (t.requestedBy) {
    title = interpolate(s.askedAgent, {
      agent: ctx.agentName(t.requestedBy),
    });
  } else if (t.by?.kind === "person" && !t.by.self) {
    title = interpolate(s.askedBy, {
      name: actorName(t.by, ctx.rc, ctx.agentName),
    });
  } else {
    title = s.askedYou;
  }
  if (!t.carried) {
    const via = t.via in s.via ? s.via[t.via as keyof typeof s.via] : undefined;
    detail = joinSentences(
      [
        via,
        t.note
          ? interpolate(
              t.by?.kind === "person" && t.by.self ? s.note : s.noteTheirs,
              { note: t.note },
            )
          : null,
      ],
      ctx.locale,
    );
  }
  return {
    key: "asked",
    ...(stamp?.person ? { person: stamp.person } : {}),
    ...(stamp?.agent && !stamp.person ? { agent: stamp.agent } : {}),
    title,
    time: dateTime(ctx.rc, t.at, ctx.timeZone, ctx.locale),
    dateTime: t.at,
    ...(detail ? { detail } : {}),
  };
}

function removed(
  copy: TombstoneCopy,
  t: TombstoneView,
  step: TombstoneStepLine | undefined,
  ctx: TombstoneContext,
): TombstoneLine {
  const s = copy.steps;
  const what = [
    t.gone.sources > 0 ? s.sources : null,
    s.search,
    t.gone.embeddings > 0 ? s.embedding : null,
  ].filter((w): w is string => Boolean(w));
  const at = step?.at ?? t.at;
  return {
    key: "removed",
    state: "forgotten",
    title: s.removed,
    time: stepTime(at, t, ctx),
    dateTime: at,
    detail: interpolate(s.removedDetail, { what: joinList(what, ctx.locale) }),
  };
}

function targetLines(
  copy: TombstoneCopy,
  t: TombstoneView,
  steps: TombstoneStepLine[],
  ctx: TombstoneContext,
): TombstoneLine[] {
  const s = copy.steps;
  const out: TombstoneLine[] = [];
  // Files rewritten together read as one line, as the board draws them.
  const written = steps.filter(
    (x) =>
      x.status === "done" && x.target && WRITES_FILES.has(x.target.delivery),
  );
  if (written.length) {
    const at = written
      .map((x) => x.at)
      .filter((a): a is string => Boolean(a))
      .sort()
      .at(-1);
    out.push({
      key: "rewritten",
      title: interpolate(s.rewritten, {
        files: joinList(
          written.map((x) => x.target!.label),
          ctx.locale,
        ),
      }),
      time: at ? stepTime(at, t, ctx) : "",
      ...(at ? { dateTime: at } : {}),
    });
  }
  for (const x of steps) {
    if (!x.target || written.includes(x)) continue;
    const label = x.target.label;
    const at = x.at ? stepTime(x.at, t, ctx) : s.waiting;
    if (x.status === "done") {
      out.push({
        key: x.key,
        title: interpolate(s.copyUpdated, { label }),
        time: at,
        ...(x.at ? { dateTime: x.at } : {}),
      });
    } else if (x.status === "held") {
      out.push({
        key: x.key,
        state: "proposed",
        title: interpolate(s.held, { label }),
        time: s.waiting,
        detail: s.heldDetail,
      });
    } else if (x.status === "unreachable") {
      out.push({
        key: x.key,
        state: "off",
        title: interpolate(s.stopped, { label }),
        time: "",
        detail: s.stoppedDetail,
      });
    } else if (x.status === "failed") {
      out.push({
        key: x.key,
        state: "conflict",
        title: interpolate(s.failed, { label }),
        time: s.waiting,
        detail: s.failedDetail,
      });
    } else if (x.reason === "delivery") {
      out.push({
        key: x.key,
        state: "working",
        title: interpolate(s.delivery, { label }),
        time: s.waiting,
        detail: s.deliveryDetail,
      });
    } else {
      out.push({
        key: x.key,
        state: "working",
        title: interpolate(s.compiling, { label }),
        time: s.waiting,
      });
    }
  }
  return out;
}

/** Memax's own copies: shown only while they aren't done (or failed). */
function internalLine(
  copy: TombstoneCopy,
  x: TombstoneStepLine,
): TombstoneLine | null {
  if (x.status === "done") return null;
  const s = copy.steps;
  const title =
    x.kind === "artifacts"
      ? s.artifacts
      : x.kind === "caches"
        ? s.caches
        : s.ledger;
  return {
    key: x.key,
    state: x.status === "failed" ? "conflict" : "working",
    title,
    time: s.waiting,
    ...(x.kind === "ledger" ? { detail: s.ledgerDetail } : {}),
  };
}

function agentLine(
  copy: TombstoneCopy,
  t: TombstoneView,
  x: TombstoneStepLine,
  ctx: TombstoneContext,
): TombstoneLine {
  const s = copy.steps;
  const key = x.agent ?? "";
  const agent = ctx.agentName(key);
  const stamp = key ? { agent: key } : {};
  if (x.status === "done") {
    return {
      key: x.key,
      ...stamp,
      title: interpolate(s.told, { agent }),
      time: x.at ? stepTime(x.at, t, ctx) : "",
      ...(x.at ? { dateTime: x.at } : {}),
    };
  }
  if (x.reason === "paused") {
    return {
      key: x.key,
      ...stamp,
      state: "off",
      title: interpolate(s.paused, { agent }),
      time: s.waiting,
      detail: s.pausedDetail,
    };
  }
  if (x.reason === "disconnected" || x.status === "unreachable") {
    return {
      key: x.key,
      ...stamp,
      state: "off",
      title: interpolate(s.disconnected, { agent }),
      time: "",
      detail: s.disconnectedDetail,
    };
  }
  return {
    key: x.key,
    ...stamp,
    title: interpolate(s.toldWaiting, { agent }),
    time: s.waiting,
  };
}

/** How it was forgotten, oldest first: Tombstone.png's lineage. */
export function tombstoneLines(
  copy: TombstoneCopy,
  t: TombstoneView,
  ctx: TombstoneContext,
): TombstoneLine[] {
  const lines: TombstoneLine[] = [asked(copy, t, ctx)];
  lines.push(
    removed(
      copy,
      t,
      t.steps.find((x) => x.kind === "removed"),
      ctx,
    ),
  );
  lines.push(
    ...targetLines(
      copy,
      t,
      t.steps.filter((x) => x.kind === "target"),
      ctx,
    ),
  );
  for (const x of t.steps) {
    if (x.kind === "artifacts" || x.kind === "caches" || x.kind === "ledger") {
      const line = internalLine(copy, x);
      if (line) lines.push(line);
    }
  }
  // Agents told first, then those still to be told.
  const agents = t.steps.filter((x) => x.kind === "agent");
  const told = agents
    .filter((x) => x.status === "done")
    .sort((a, b) => (a.at ?? "").localeCompare(b.at ?? ""));
  for (const x of [...told, ...agents.filter((x) => x.status !== "done")]) {
    lines.push(agentLine(copy, t, x, ctx));
  }
  return lines;
}

/** The Gone panel: "The statement and its two sources", … */
export function goneLines(
  copy: TombstoneCopy,
  t: TombstoneView,
  ctx: TombstoneContext,
): string[] {
  const g = copy.gone;
  const lines = [
    t.gone.sources === 0
      ? g.statement
      : t.gone.sources === 1
        ? g.statementSource
        : interpolate(g.statementSources, {
            n: spell(ctx.app, t.gone.sources),
          }),
    t.gone.embeddings > 0 ? g.index : g.indexOnly,
  ];
  if (t.gone.files > 0) lines.push(count(g.filesOne, g.files, t.gone.files));
  if (t.agents > 0) lines.push(count(g.agentsOne, g.agents, t.agents));
  if (t.with.length) {
    lines.push(interpolate(g.with, { list: joinList(t.with, ctx.locale) }));
  }
  return lines;
}

function reachLine(
  copy: TombstoneCopy,
  u: UnreachableLine,
  ctx: TombstoneContext,
): string[] {
  const r = copy.reach;
  const list = (items: string[]) => joinList(items, ctx.locale);
  switch (u.kind) {
    case "git_history":
      if (!u.files.length) return [];
      return [
        u.repositories.length
          ? interpolate(r.gitHistory, {
              files: list(u.files),
              repos: list(u.repositories),
            })
          : interpolate(r.gitHistoryBare, { files: list(u.files) }),
      ];
    case "agent_memory":
      return [
        u.agents.length
          ? interpolate(r.agentMemory, {
              agents: list(u.agents.map((a) => ctx.agentName(a))),
            })
          : r.agentMemoryBare,
      ];
    case "backups":
      return u.days ? [interpolate(r.backups, { n: u.days })] : [];
    case "llm":
      return u.processors.map((p) =>
        interpolate(p.zeroRetention ? r.llmZero : r.llm, {
          name: p.name,
          purpose: r.purposes[p.purpose],
        }),
      );
    case "hand_edits":
      return u.files.length
        ? [interpolate(r.handEdits, { files: list(u.files) })]
        : [];
    case "copies":
      return u.targets.length
        ? [interpolate(r.copies, { targets: list(u.targets) })]
        : [];
  }
}

/** The copies Memax can't reach, said plainly. */
export function reachLines(
  copy: TombstoneCopy,
  t: TombstoneView,
  ctx: TombstoneContext,
): string[] {
  return t.unreachable.flatMap((u) => reachLine(copy, u, ctx));
}
