/**
 * The Brief editor's model (BriefEdit.png), pure so every rule is
 * unit-tested: a draft of the Brief's sections that collects edits,
 * moves, regroups, cites and new facts until Done, the list of changes
 * the panel shows, the checks Done runs, and the payload it sends.
 *
 * - Facts are kept memories of the space. A fact's new words become a
 *   memory edit (If-Match on its version); a new fact is remembered
 *   (kept) first, then placed. The server checks both.
 * - Prose lines must cite at least one kept memory.
 * - Moving a fact past the first or last fact of its section regroups
 *   it into the section before or after (⌥↑ / ⌥↓).
 */
import {
  memorySectionOf,
  type BriefItem,
  type BriefRowState,
  type BriefStructure,
  type BriefView,
} from "@/lib/v2/data/brief";
import type { Section } from "@/lib/v2/data/types";

export interface DraftItem {
  /** Stable while editing: the memory's ref, the prose's key, or "new-n". */
  key: string;
  kind: "memory" | "prose" | "new";
  ref: string | null;
  /** The memory's version: the If-Match of an edit. */
  version: number | null;
  text: string;
  /** The words when editing began; "" for a new fact. */
  original: string;
  cites: string[];
  originalCites: string[];
  /** Where a memory came from ("session 3e1a"): what it cites, shown while editing. */
  source: string | null;
  /** The section it started in; "" for a new fact. */
  from: string;
  state: BriefRowState;
}

export interface DraftSection {
  key: string;
  heading: string;
  /** The heading when editing began; null for a section added here. */
  originalHeading: string | null;
  items: DraftItem[];
}

export interface EditorState {
  title: string;
  summary: string | null;
  /** The version Done revises (If-Match). */
  base: number;
  sections: DraftSection[];
  /** Facts taken out of the Brief. */
  removed: DraftItem[];
  /** Each section's facts in their first order, for "Reordered". */
  order: Record<string, string[]>;
  /** The fact being edited, and its words before this edit. */
  editing: { key: string; before: string } | null;
  nextId: number;
}

export type EditorAction =
  | { type: "move"; key: string; delta: 1 | -1 }
  | { type: "moveTo"; key: string; section: string; index: number }
  | { type: "edit"; key: string }
  | { type: "text"; key: string; text: string }
  | { type: "keep" }
  | { type: "cancel" }
  | { type: "cite"; key: string; ref: string }
  | { type: "uncite"; key: string; ref: string }
  | { type: "remove"; key: string }
  | { type: "add"; section: string; after?: string }
  | { type: "heading"; section: string; heading: string }
  | { type: "addSection"; heading: string }
  | { type: "rebase"; base: number }
  | { type: "reset"; state: EditorState };

/** The editor's start: the Brief as the page shows it, without the proposals waiting in Review. */
export function draftOf(brief: BriefView): EditorState {
  const sections = brief.sections.map((section) => ({
    key: section.key,
    heading: section.heading,
    originalHeading: section.heading,
    items: section.rows
      .filter((row) => row.kind !== "waiting")
      .map(
        (row): DraftItem => ({
          key: row.key,
          kind: row.kind === "prose" ? "prose" : "memory",
          ref: row.ref,
          version: row.version,
          text: row.text,
          original: row.text,
          cites: [...row.cites],
          originalCites: [...row.cites],
          source: row.source,
          from: section.key,
          state: row.state,
        }),
      ),
  }));
  return {
    title: brief.title,
    summary: brief.summary,
    base: brief.version,
    sections,
    removed: [],
    order: Object.fromEntries(
      sections.map((s) => [s.key, s.items.map((i) => i.key)]),
    ),
    editing: null,
    nextId: 1,
  };
}

function locate(state: EditorState, key: string) {
  for (let s = 0; s < state.sections.length; s++) {
    const i = state.sections[s]!.items.findIndex((item) => item.key === key);
    if (i >= 0) return { s, i };
  }
  return null;
}

function updateItem(
  state: EditorState,
  key: string,
  update: (item: DraftItem) => DraftItem,
): EditorState {
  return {
    ...state,
    sections: state.sections.map((section) => ({
      ...section,
      items: section.items.map((item) =>
        item.key === key ? update(item) : item,
      ),
    })),
  };
}

/** A section key from a heading: the compiler's pattern, unique in the Brief. */
export function sectionKeyFor(
  heading: string,
  taken: readonly string[],
): string {
  const base =
    heading
      .normalize("NFKD")
      .toLowerCase()
      .replace(/[^a-z0-9]+/g, "-")
      .replace(/^-+|-+$/g, "")
      .replace(/^[^a-z]+/, "")
      .slice(0, 32) || "section";
  let key = base;
  for (let n = 2; taken.includes(key); n++) key = `${base}-${n}`;
  return key;
}

export function editorReducer(
  state: EditorState,
  action: EditorAction,
): EditorState {
  switch (action.type) {
    case "move": {
      const at = locate(state, action.key);
      if (!at) return state;
      const sections = state.sections.map((s) => ({
        ...s,
        items: [...s.items],
      }));
      const items = sections[at.s]!.items;
      const target = at.i + action.delta;
      if (target >= 0 && target < items.length) {
        const [item] = items.splice(at.i, 1);
        items.splice(target, 0, item!);
        return { ...state, sections };
      }
      // Past the edge of its section: into the next one up or down.
      const next = sections[at.s + action.delta];
      if (!next) return state;
      const [item] = items.splice(at.i, 1);
      if (action.delta === 1) next.items.unshift(item!);
      else next.items.push(item!);
      return { ...state, sections };
    }
    case "moveTo": {
      const at = locate(state, action.key);
      const to = state.sections.findIndex((s) => s.key === action.section);
      if (!at || to < 0) return state;
      const sections = state.sections.map((s) => ({
        ...s,
        items: [...s.items],
      }));
      const [item] = sections[at.s]!.items.splice(at.i, 1);
      let index = action.index;
      // Dropping below itself in the same section: the gap closed first.
      if (at.s === to && at.i < index) index -= 1;
      const dest = sections[to]!.items;
      dest.splice(Math.max(0, Math.min(index, dest.length)), 0, item!);
      return { ...state, sections };
    }
    case "edit": {
      const at = locate(state, action.key);
      if (!at) return state;
      const item = state.sections[at.s]!.items[at.i]!;
      return { ...state, editing: { key: item.key, before: item.text } };
    }
    case "text":
      return updateItem(state, action.key, (item) => ({
        ...item,
        text: action.text,
      }));
    case "keep":
      return { ...state, editing: null };
    case "cancel": {
      const editing = state.editing;
      if (!editing) return state;
      return {
        ...updateItem(state, editing.key, (item) => ({
          ...item,
          text: editing.before,
        })),
        editing: null,
      };
    }
    case "cite":
      return updateItem(state, action.key, (item) =>
        item.kind !== "prose" || item.cites.includes(action.ref)
          ? item
          : { ...item, cites: [...item.cites, action.ref] },
      );
    case "uncite":
      return updateItem(state, action.key, (item) => ({
        ...item,
        cites: item.cites.filter((ref) => ref !== action.ref),
      }));
    case "remove": {
      const at = locate(state, action.key);
      if (!at) return state;
      const item = state.sections[at.s]!.items[at.i]!;
      return {
        ...state,
        sections: state.sections.map((s, n) =>
          n === at.s
            ? { ...s, items: s.items.filter((i) => i.key !== action.key) }
            : s,
        ),
        // A new fact taken out was never there.
        removed: item.kind === "new" ? state.removed : [...state.removed, item],
        editing: state.editing?.key === action.key ? null : state.editing,
      };
    }
    case "add": {
      const key = `new-${state.nextId}`;
      const item: DraftItem = {
        key,
        kind: "new",
        ref: null,
        version: null,
        text: "",
        original: "",
        cites: [],
        originalCites: [],
        source: null,
        from: "",
        state: "kept",
      };
      return {
        ...state,
        nextId: state.nextId + 1,
        sections: state.sections.map((s) => {
          if (s.key !== action.section) return s;
          const items = [...s.items];
          const after = action.after
            ? items.findIndex((i) => i.key === action.after)
            : -1;
          items.splice(after >= 0 ? after + 1 : items.length, 0, item);
          return { ...s, items };
        }),
      };
    }
    case "heading":
      return {
        ...state,
        sections: state.sections.map((s) =>
          s.key === action.section ? { ...s, heading: action.heading } : s,
        ),
      };
    case "addSection": {
      const key = sectionKeyFor(
        action.heading,
        state.sections.map((s) => s.key),
      );
      return {
        ...state,
        sections: [
          ...state.sections,
          { key, heading: action.heading, originalHeading: null, items: [] },
        ],
      };
    }
    case "rebase":
      return { ...state, base: action.base };
    case "reset":
      return action.state;
  }
}

/** One line of the Changes panel. */
export type EditorChange =
  | { kind: "edited"; key: string; ref: string; before: string; after: string }
  | {
      kind: "prose";
      key: string;
      section: string;
      before: string;
      after: string;
    }
  | {
      kind: "moved";
      key: string;
      ref: string | null;
      section: string;
      text: string;
      from: string;
    }
  | { kind: "added"; key: string; section: string; text: string }
  | { kind: "removed"; key: string; ref: string | null; text: string }
  | { kind: "reordered"; key: string; section: string }
  | { kind: "renamed"; key: string; from: string; to: string }
  | { kind: "section"; key: string; section: string }
  | { kind: "cited"; key: string; ref: string; text: string }
  | { kind: "uncited"; key: string; ref: string; text: string };

const headingOf = (state: EditorState, key: string) =>
  state.sections.find((s) => s.key === key)?.heading ?? key;

/** What Done will keep, in the order the panel lists it. */
export function changesOf(state: EditorState): EditorChange[] {
  const changes: EditorChange[] = [];
  for (const section of state.sections) {
    if (section.originalHeading === null) {
      changes.push({
        kind: "section",
        key: `s:${section.key}`,
        section: section.heading,
      });
    } else if (section.heading.trim() !== section.originalHeading) {
      changes.push({
        kind: "renamed",
        key: `s:${section.key}`,
        from: section.originalHeading,
        to: section.heading.trim(),
      });
    }
    for (const item of section.items) {
      if (item.kind === "new") {
        if (item.text.trim()) {
          changes.push({
            kind: "added",
            key: item.key,
            section: section.heading,
            text: item.text.trim(),
          });
        }
        continue;
      }
      if (item.text.trim() !== item.original) {
        changes.push(
          item.kind === "memory"
            ? {
                kind: "edited",
                key: `e:${item.key}`,
                ref: item.ref!,
                before: item.original,
                after: item.text.trim(),
              }
            : {
                kind: "prose",
                key: `e:${item.key}`,
                section: section.heading,
                before: item.original,
                after: item.text.trim(),
              },
        );
      }
      if (item.from !== section.key) {
        changes.push({
          kind: "moved",
          key: `m:${item.key}`,
          ref: item.ref,
          section: section.heading,
          text: item.text.trim(),
          from: headingOf(state, item.from),
        });
      }
      for (const ref of item.cites) {
        if (!item.originalCites.includes(ref)) {
          changes.push({
            kind: "cited",
            key: `c:${item.key}:${ref}`,
            ref,
            text: item.text.trim(),
          });
        }
      }
      for (const ref of item.originalCites) {
        if (!item.cites.includes(ref)) {
          changes.push({
            kind: "uncited",
            key: `u:${item.key}:${ref}`,
            ref,
            text: item.text.trim(),
          });
        }
      }
    }
    // Facts that stayed in this section, in a new order.
    const first = (state.order[section.key] ?? []).filter((key) =>
      section.items.some((i) => i.key === key),
    );
    const now = section.items
      .map((i) => i.key)
      .filter((key) => first.includes(key));
    if (now.join("\n") !== first.join("\n")) {
      changes.push({
        kind: "reordered",
        key: `r:${section.key}`,
        section: section.heading,
      });
    }
  }
  for (const item of state.removed) {
    changes.push({
      kind: "removed",
      key: `x:${item.key}`,
      ref: item.ref,
      text: item.original,
    });
  }
  return changes;
}

export type DraftError = "proseNeedsCite" | "emptyFact" | "emptyHeading";

/** What stops Done, by fact or section key. A new fact left empty is dropped, not an error. */
export function errorsOf(
  state: EditorState,
  isKept: (ref: string) => boolean,
): Map<string, DraftError> {
  const errors = new Map<string, DraftError>();
  for (const section of state.sections) {
    if (!section.heading.trim()) errors.set(`s:${section.key}`, "emptyHeading");
    for (const item of section.items) {
      if (item.kind === "new") continue;
      if (!item.text.trim()) errors.set(item.key, "emptyFact");
      else if (item.kind === "prose" && !item.cites.some(isKept)) {
        errors.set(item.key, "proseNeedsCite");
      }
    }
  }
  return errors;
}

/** What Done sends: facts to remember, memories to edit, then the new version. */
export interface DonePlan {
  remember: { key: string; section: Section; statement: string }[];
  edits: { ref: string; version: number; statement: string }[];
  /** The version, once each new fact has its ref. */
  structure: (refs: ReadonlyMap<string, string>) => BriefStructure;
}

export function donePlan(state: EditorState): DonePlan {
  const remember: DonePlan["remember"] = [];
  const edits: DonePlan["edits"] = [];
  for (const section of state.sections) {
    for (const item of section.items) {
      const text = item.text.trim();
      if (item.kind === "new" && text) {
        remember.push({
          key: item.key,
          section: memorySectionOf(section.key),
          statement: text,
        });
      } else if (
        item.kind === "memory" &&
        item.ref &&
        item.version !== null &&
        text !== item.original
      ) {
        edits.push({ ref: item.ref, version: item.version, statement: text });
      }
    }
  }
  return {
    remember,
    edits,
    structure: (refs) => ({
      title: state.title,
      summary: state.summary,
      sections: state.sections
        .map((section) => ({
          key: section.key,
          heading: section.heading.trim(),
          items: section.items.flatMap((item): BriefItem[] => {
            const text = item.text.trim();
            if (item.kind === "prose") return [{ text, cites: item.cites }];
            if (item.kind === "memory" && item.ref) return [{ ref: item.ref }];
            const ref = refs.get(item.key);
            return item.kind === "new" && text && ref ? [{ ref }] : [];
          }),
        }))
        // An empty section has nothing to compile.
        .filter((section) => section.items.length > 0),
    }),
  };
}

/** Facts in the draft that a reader would count: placed, cited or new. */
export function draftFacts(state: EditorState): number {
  const refs = new Set<string>();
  let fresh = 0;
  for (const section of state.sections) {
    for (const item of section.items) {
      if (item.kind === "new") {
        if (item.text.trim()) fresh += 1;
      } else if (item.ref) refs.add(item.ref);
      item.cites.forEach((ref) => refs.add(ref));
    }
  }
  return refs.size + fresh;
}
