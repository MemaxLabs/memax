import { CommandFailedError } from "./command-error";
import {
  briefRefs,
  type BriefSource,
  type BriefStructure,
  type BriefVersionView,
  type BriefView,
} from "./brief";
import {
  buildBriefView,
  proseAuthorsOf,
  type BriefInputMemory,
} from "./brief-view";
import {
  DEMO_BRIEF_MEMORIES,
  DEMO_READS,
  DEMO_TITLES,
  DEMO_VERSIONS,
  demoBriefOf,
} from "./demo-brief-data";
import type { Decided } from "./demo-records";
import type { ReviewItem } from "./review";
import type { Section } from "./types";

/**
 * The demo's Brief on the same session as its Review and Memories:
 * what was kept in Review compiles (at the end of its section), what was
 * rejected is gone, an edit's words show, a fact remembered while
 * editing the Brief is kept, and Done writes a new version (B-0044 …).
 * Commands settle after a short delay and replay by idempotency key.
 */

const YOU = {
  kind: "person" as const,
  self: true,
  initials: "ZZ",
  name: "Ziyang",
};

export interface DemoBriefSession {
  edited: (
    slug: string,
    ref: string,
  ) => { statement: string; version: number } | undefined;
  decided: (slug: string, ref: string) => Decided | undefined;
  arrived: (slug: string) => ReviewItem[];
}

function sleep(ms: number) {
  return new Promise<void>((resolve) => setTimeout(resolve, ms));
}

export function createDemoBrief({
  now,
  session,
  commandDelayMs = 240,
}: {
  now: () => Date;
  session: DemoBriefSession;
  commandDelayMs?: number;
}): BriefSource & {
  /** A fact kept from ⌘K Remember or the Brief editor, so the Brief can place it. */
  remembered(
    slug: string,
    memory: { ref: string; statement: string; section: Section },
  ): void;
} {
  const versions = new Map<string, BriefVersionView[]>();
  const extra = new Map<string, BriefInputMemory[]>();
  const replays = new Map<string, { ref: string; version: number }>();

  function versionsOf(slug: string): BriefVersionView[] {
    let list = versions.get(slug);
    if (!list) {
      list =
        DEMO_VERSIONS[slug] ??
        (() => {
          const generic = demoBriefOf(slug);
          return generic ? [generic.version] : [];
        })();
      versions.set(slug, list);
    }
    return list;
  }

  function memoriesOf(slug: string): BriefInputMemory[] {
    const base = DEMO_BRIEF_MEMORIES[slug] ?? demoBriefOf(slug)?.memories ?? [];
    const stamp = now().toISOString();
    const live = base.flatMap((m): BriefInputMemory[] => {
      const done = session.decided(slug, m.ref);
      if (done?.outcome === "rejected") return [];
      const edit = session.edited(slug, m.ref);
      let next = edit
        ? { ...m, text: edit.statement, version: edit.version, changed: null }
        : m;
      if (done?.outcome === "kept") {
        next = {
          ...next,
          state: "kept",
          lifecycle: "kept",
          receipt: { by: YOU, action: "kept", at: stamp },
        };
      }
      return [next];
    });
    const arrived = session.arrived(slug).map(
      (item): BriefInputMemory => ({
        ref: item.ref,
        text: item.statement,
        section: item.section,
        state: "proposed",
        lifecycle: "proposed",
        version: item.version,
        receipt: { by: item.by, action: "proposed", at: item.at },
        source: null,
        scope: [],
      }),
    );
    return [...live, ...(extra.get(slug) ?? []), ...arrived];
  }

  function view(slug: string): BriefView | null {
    const list = versionsOf(slug);
    const current = list[0];
    if (!current) return null;
    return buildBriefView({
      id: `brief-${slug}`,
      ref: current.ref,
      version: current.version,
      structure: current.structure,
      by: current.by,
      at: current.at,
      reason: current.reason,
      memories: memoriesOf(slug),
      proseAuthors: proseAuthorsOf(current.structure, list),
      titles: DEMO_TITLES[slug] ?? {},
      reads: DEMO_READS[slug] ?? null,
    });
  }

  return {
    peek: (slug) => view(slug),
    peekVersions: (slug) => ({ items: versionsOf(slug), nextCursor: null }),
    async get({ space }) {
      return view(space.slug);
    },
    async versions({ space }) {
      return { items: versionsOf(space.slug), nextCursor: null };
    },
    async revise({ space, base, structure, reason, idempotencyKey }) {
      await sleep(commandDelayMs);
      const replay = replays.get(idempotencyKey);
      if (replay) return replay;
      const list = versionsOf(space.slug);
      const current = list[0];
      if ((current?.version ?? null) !== base) {
        throw new CommandFailedError({
          kind: "clash",
          currentVersion: current?.version ?? null,
        });
      }
      const kept = new Map(
        memoriesOf(space.slug)
          .filter((m) => m.lifecycle === "kept")
          .map((m) => [m.ref, m]),
      );
      // The server's rules: memory items are kept memories of the space,
      // and every line of prose cites one.
      for (const section of structure.sections) {
        for (const item of section.items) {
          const ok =
            "ref" in item
              ? kept.has(item.ref)
              : item.cites.some((ref) => kept.has(ref));
          if (!ok) {
            throw new CommandFailedError({
              kind: "unknown",
              message:
                "ref" in item
                  ? `${item.ref} isn't a kept memory of this space.`
                  : "A line of prose must cite a kept memory.",
            });
          }
        }
      }
      const version = (current?.version ?? 0) + 1;
      const n = current ? Number(current.ref.slice(2)) + 1 : 1;
      const next: BriefVersionView = {
        ref: `B-${String(n).padStart(4, "0")}`,
        version,
        parent: current?.version ?? null,
        current: true,
        by: YOU,
        at: now().toISOString(),
        reason: reason ?? null,
        facts: briefRefs(structure).length,
        structure: structure satisfies BriefStructure,
      };
      versions.set(space.slug, [
        next,
        ...list.map((v) => ({ ...v, current: false })),
      ]);
      const result = { ref: next.ref, version };
      replays.set(idempotencyKey, result);
      return result;
    },
    remembered(slug, { ref, statement, section }) {
      extra.set(slug, [
        ...(extra.get(slug) ?? []),
        {
          ref,
          text: statement,
          section,
          state: "kept",
          lifecycle: "kept",
          version: 1,
          receipt: { by: YOU, action: "kept", at: now().toISOString() },
          source: null,
          scope: [],
        },
      ]);
    },
  };
}
