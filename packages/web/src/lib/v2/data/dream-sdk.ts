import { MemaxError, type V2 } from "memax-sdk";
import {
  NO_COUNTS,
  type DreamActionView,
  type DreamEditionSummary,
  type DreamEditionView,
  type DreamEditionsPage,
  type DreamScheduleView,
  type DreamSettingsView,
  type DreamSource,
  type NoteAuthors,
} from "./dream";
import type { MemoryListItem } from "./memories";
import type { Actor } from "./records";
import {
  agentKey,
  listItemOf,
  receiptsFor,
  type V2Client,
} from "./sdk-records";

/**
 * Dream through memax.v2.dream: an edition with its actions (each with
 * its memory as it is now), the space's editions and schedule, undo,
 * restore, run now and the person's settings. The rails' receipts come
 * from the same join Memories uses (receiptsFor).
 */

function summaryOf(e: V2.DreamEdition): DreamEditionSummary {
  return {
    ref: e.ref,
    n: e.n,
    slot: e.slot,
    finishedAt: e.finished_at,
    notes: e.notes_read,
    facts: e.fact_refs.length,
  };
}

function scheduleOf(s: V2.DreamSchedule | undefined): DreamScheduleView | null {
  return s
    ? { cadence: s.cadence, timeZone: s.time_zone, nextAt: s.next_at }
    : null;
}

function settingsOf(s: V2.DreamSettings): DreamSettingsView {
  return {
    timeZone: s.time_zone,
    timeZoneSource: s.time_zone_source,
    morningEmail: s.morning_email,
  };
}

function authorsOf(by: readonly V2.NoteAuthorCount[]): NoteAuthors[] {
  return by.map((a) => ({
    kind: a.kind,
    agent: a.kind === "agent" ? (agentKey(a.agent) ?? null) : null,
    count: a.count,
  }));
}

/** Who kept a memory, from its rail, when its latest word is a keep. */
function keptByOf(item: MemoryListItem | null): Actor | null {
  return item?.receipt?.action === "kept" ? item.receipt.by : null;
}

/** An edition as its page reads it, the rails joined from the receipts. */
export function editionViewOf(
  e: V2.DreamEdition,
  receipts: ReadonlyMap<string, V2.Receipt>,
  viewerId: string | undefined,
): DreamEditionView {
  const item = (m: V2.Memory | undefined) =>
    m ? listItemOf(m, receipts, viewerId) : null;
  const actions: DreamActionView[] = (e.actions ?? []).map((a) => {
    const related = item(a.related);
    return {
      id: a.id,
      n: a.n,
      kind: a.kind,
      memory: item(a.memory),
      version: a.memory?.version ?? null,
      related: related ? { ref: related.ref, keptBy: keptByOf(related) } : null,
      notes: a.note_refs,
      noteAuthors: null,
      brief: a.brief ? { ref: a.brief.ref ?? null, ops: a.brief.ops } : null,
      undone: a.undone ? { at: a.undone.at } : null,
      undoable: a.undoable && !a.undone,
    };
  });
  return {
    ref: e.ref,
    n: e.n,
    slot: e.slot,
    trigger: e.trigger,
    since: e.since ?? null,
    until: e.until,
    finishedAt: e.finished_at,
    seconds: e.seconds,
    notes: e.notes_read,
    notesBy: authorsOf(e.notes_by),
    noteIds: e.note_refs,
    factIds: e.fact_refs,
    counts: { ...NO_COUNTS, ...e.counts },
    undone: e.undone,
    needsYou: e.needs_you,
    actions,
    surfaced: (e.surfaced ?? []).flatMap((s) => {
      const memory = item(s.memory);
      const other = item(s.with);
      return memory
        ? [
            {
              memory,
              with: other ? { ref: other.ref, keptBy: keptByOf(other) } : null,
            },
          ]
        : [];
    }),
  };
}

function isNotFound(err: unknown): boolean {
  return (
    err instanceof MemaxError &&
    (err.code === "not_found" || err.status === 404)
  );
}

export function createSdkDream(
  client: V2Client,
  viewerId: () => string | undefined,
): DreamSource {
  return {
    async editions({ space, limit = 4, signal }): Promise<DreamEditionsPage> {
      const page = await client.v2.dream.editions(space.slug, {
        limit,
        signal,
      });
      return {
        items: page.items.map(summaryOf),
        hasMore: page.has_more,
        schedule: scheduleOf(page.schedule),
      };
    },
    async edition({ space, ref, signal }) {
      let e: V2.DreamEdition;
      try {
        e = await client.v2.dream.edition(space.slug, ref, { signal });
      } catch (err) {
        if (isNotFound(err)) return null;
        throw err;
      }
      const memories: V2.Memory[] = [];
      for (const a of e.actions ?? []) {
        if (a.memory) memories.push(a.memory);
        if (a.related) memories.push(a.related);
      }
      for (const s of e.surfaced ?? []) {
        memories.push(s.memory);
        if (s.with) memories.push(s.with);
      }
      const receipts = await receiptsFor(client, space.slug, memories, signal);
      return editionViewOf(e, receipts, viewerId());
    },
    async undo({ action, idempotencyKey }) {
      await client.v2.dream.undo(action.id, {}, { idempotencyKey });
    },
    async undoAll({ space, edition, kind, idempotencyKey }) {
      const result = await client.v2.dream.undoAll(
        space.slug,
        edition,
        { kind },
        { idempotencyKey },
      );
      return {
        undone: result.undone.length,
        refused: result.refused.map((r) => ({
          ref: r.ref ?? null,
          reason: r.reason,
        })),
      };
    },
    async restore({ space, ref, version, idempotencyKey }) {
      await client.v2.memories.restore(
        ref,
        {},
        {
          space: space.slug,
          idempotencyKey,
          ...(version !== null ? { ifMatch: version } : {}),
        },
      );
    },
    async run({ space, idempotencyKey }) {
      await client.v2.dream.run(space.slug, { idempotencyKey });
    },
    async settings({ signal } = {}) {
      return settingsOf(await client.v2.dream.settings({ signal }));
    },
    async updateSettings({ timeZone, morningEmail, idempotencyKey }) {
      return settingsOf(
        await client.v2.dream.updateSettings(
          {
            ...(timeZone !== undefined ? { time_zone: timeZone } : {}),
            ...(morningEmail !== undefined
              ? { morning_email: morningEmail }
              : {}),
          },
          { idempotencyKey },
        ),
      );
    },
    async unsubscribe({ token }) {
      await client.v2.dream.unsubscribe(token);
    },
  };
}
