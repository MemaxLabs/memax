/**
 * What changed between two Brief versions (BriefHistory.png), pure so
 * the rules are unit-tested. A version holds display IDs and prose, so
 * the diff is structural: facts added, taken out or moved between
 * sections, prose reworded, sections renamed. What a Dream edition did
 * to memories themselves (a rewording, a stale flag) isn't in a version;
 * the source may say so (`edition`, the demo only until editions are
 * recorded) and it's listed with the rest.
 */
import type {
  BriefItem,
  BriefStructure,
  BriefVersionView,
  EditionChange,
} from "@/lib/v2/data/brief";

export type HistoryChange =
  | { kind: "added"; ref: string; note: string | null }
  | { kind: "removed"; ref: string; note: string | null }
  | { kind: "moved"; ref: string; from: string; note: string | null }
  | {
      kind: "reworded";
      /** The memory, or null for a line of prose. */
      ref: string | null;
      before: string;
      after: string;
      note: string | null;
    }
  | { kind: "flagged"; ref: string; note: string | null }
  | { kind: "proseAdded"; text: string; cites: string[] }
  | { kind: "proseRemoved"; text: string; cites: string[] }
  | { kind: "renamed"; from: string };

export interface HistorySection {
  key: string;
  heading: string;
  changes: HistoryChange[];
}

export interface HistoryDiff {
  sections: HistorySection[];
  /** Facts in both versions, where they were. */
  unchanged: string[];
  count: number;
}

interface Placed {
  section: string;
  heading: string;
}

function memoryPlaces(s: BriefStructure | null): Map<string, Placed> {
  const places = new Map<string, Placed>();
  for (const section of s?.sections ?? []) {
    for (const item of section.items) {
      if ("ref" in item) {
        places.set(item.ref, {
          section: section.key,
          heading: section.heading,
        });
      }
    }
  }
  return places;
}

type Prose = Extract<BriefItem, { text: string }>;

const sameCites = (a: Prose, b: Prose) =>
  a.cites.length === b.cites.length &&
  a.cites.every((r) => b.cites.includes(r));

/** The changes from `from` (null: the first version) to `to`. */
export function diffVersions(
  from: BriefStructure | null,
  to: BriefStructure,
  extra: { edition?: EditionChange[]; notes?: Record<string, string> } = {},
): HistoryDiff {
  const notes = extra.notes ?? {};
  const before = memoryPlaces(from);
  const after = memoryPlaces(to);
  const sections = new Map<string, HistorySection>();
  const sectionOf = (key: string, heading: string) => {
    let found = sections.get(key);
    if (!found) {
      found = { key, heading, changes: [] };
      sections.set(key, found);
    }
    return found;
  };
  const edited = new Set((extra.edition ?? []).map((c) => c.ref));
  const unchanged: string[] = [];

  for (const section of to.sections) {
    const old = from?.sections.find((s) => s.key === section.key);
    if (old && old.heading !== section.heading) {
      sectionOf(section.key, section.heading).changes.push({
        kind: "renamed",
        from: old.heading,
      });
    }
    const oldProse = (old?.items ?? []).filter((i): i is Prose => "text" in i);
    const used = new Set<Prose>();
    for (const item of section.items) {
      if ("ref" in item) {
        const was = before.get(item.ref);
        const note = notes[item.ref] ?? null;
        if (!from) continue;
        if (!was) {
          sectionOf(section.key, section.heading).changes.push({
            kind: "added",
            ref: item.ref,
            note,
          });
        } else if (was.section !== section.key) {
          sectionOf(section.key, section.heading).changes.push({
            kind: "moved",
            ref: item.ref,
            from: was.heading,
            note,
          });
        } else if (!edited.has(item.ref)) {
          unchanged.push(item.ref);
        }
        continue;
      }
      if (!from) continue;
      const same = oldProse.find((p) => !used.has(p) && p.text === item.text);
      if (same) {
        used.add(same);
        continue;
      }
      const reworded = oldProse.find((p) => !used.has(p) && sameCites(p, item));
      if (reworded) {
        used.add(reworded);
        sectionOf(section.key, section.heading).changes.push({
          kind: "reworded",
          ref: null,
          before: reworded.text,
          after: item.text,
          note: null,
        });
      } else {
        sectionOf(section.key, section.heading).changes.push({
          kind: "proseAdded",
          text: item.text,
          cites: item.cites,
        });
      }
    }
    for (const p of oldProse) {
      if (!used.has(p) && from) {
        sectionOf(section.key, section.heading).changes.push({
          kind: "proseRemoved",
          text: p.text,
          cites: p.cites,
        });
      }
    }
  }
  for (const [ref, was] of before) {
    if (!after.has(ref)) {
      sectionOf(was.section, was.heading).changes.push({
        kind: "removed",
        ref,
        note: notes[ref] ?? null,
      });
    }
  }
  for (const change of extra.edition ?? []) {
    const heading =
      to.sections.find((s) => s.key === change.section)?.heading ??
      change.section;
    sectionOf(change.section, heading).changes.push(
      change.kind === "reworded"
        ? {
            kind: "reworded",
            ref: change.ref,
            before: change.before,
            after: change.after,
            note: change.note,
          }
        : { kind: "flagged", ref: change.ref, note: change.note },
    );
  }

  // Sections in the newer version's order, then any that went away.
  const order = to.sections.map((s) => s.key);
  const list = [...sections.values()].sort(
    (a, b) =>
      (order.indexOf(a.key) + 1 || Infinity) -
      (order.indexOf(b.key) + 1 || Infinity),
  );
  // Within a section: what Dream did to memories first, as the board lists it.
  const rank = (c: HistoryChange) =>
    c.kind === "reworded" && c.ref ? 0 : c.kind === "flagged" ? 1 : 2;
  for (const s of list) s.changes.sort((a, b) => rank(a) - rank(b));
  return {
    sections: list,
    unchanged,
    count: list.reduce((sum, s) => sum + s.changes.length, 0),
  };
}

/** A version's diff from the one before it. */
export function diffFromParent(
  version: BriefVersionView,
  versions: readonly BriefVersionView[],
): HistoryDiff {
  const parent =
    version.parent === null
      ? null
      : (versions.find((v) => v.version === version.parent) ?? null);
  return diffVersions(parent?.structure ?? null, version.structure, {
    edition: version.edition,
    notes: version.notes,
  });
}

/** The single change of a one-change version, for its title. */
export function onlyChange(diff: HistoryDiff): HistoryChange | null {
  if (diff.count !== 1) return null;
  return diff.sections.flatMap((s) => s.changes)[0] ?? null;
}
