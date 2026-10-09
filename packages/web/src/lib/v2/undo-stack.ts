/**
 * The per-tab undo stack (plan §6.4: "Undo (⌘Z) where the command has an
 * inverse"): the person's own decisions in each space, newest last, each
 * with the receipt its command returned and when it landed. ⌘Z undoes
 * the newest one still inside its window; a decision toast's Undo
 * undoes its own. Pure and framework-free, so the rules are unit-tested;
 * React reads it through `(app)/_lib/undo.tsx`.
 *
 * Per tab, on purpose: it lives in this page's memory, never in storage.
 * Another tab, or a reload, starts with nothing to undo here; the
 * server's window still holds, and Activity names every receipt.
 */
import type { ReviewItem } from "./data/review";
import type { SpaceSummary } from "./data/types";
import { undoWindowMs, type UndoCommand } from "./data/undo";

export interface UndoEntry {
  /** This tab's id for it. */
  id: string;
  /** The data source that made it: the demo's and the server's don't mix. */
  source: string;
  space: SpaceSummary;
  command: UndoCommand;
  /** The memory the toast names. */
  ref: string;
  /** An edit that also kept a proposal (Review's E, then ⌘↵). */
  kept?: boolean;
  /** The receipt Undo addresses. */
  receipt: string;
  /** When it landed, by this tab's clock (the server keeps the real window). */
  at: number;
  /** The queue row before the decision, to put back at once, and where it sat. */
  restore: { item: ReviewItem; index: number } | null;
  status: "done" | "undoing" | "undone";
  /** One idempotency key per undo, reused across its retries. */
  key: string;
}

export type UndoInput = Omit<UndoEntry, "id" | "status" | "key" | "at">;

/** What Review hears while an undo runs: put the card back, or take it out again. */
export type UndoEvent =
  | { type: "restore"; entry: UndoEntry }
  | { type: "rollback"; entry: UndoEntry };

/** At most this many entries per tab; older ones are past any window anyway. */
const MAX_ENTRIES = 50;

export function createUndoStack({
  now = () => Date.now(),
  newId = () => crypto.randomUUID(),
}: { now?: () => number; newId?: () => string } = {}) {
  let entries: readonly UndoEntry[] = [];
  const listeners = new Set<() => void>();
  const watchers = new Set<(event: UndoEvent) => void>();

  const changed = () => {
    for (const listener of listeners) listener();
  };

  const live = (entry: UndoEntry) =>
    now() - entry.at < undoWindowMs(entry.command);

  return {
    /** Remembers a decision that can be undone; returns its entry. */
    push(input: UndoInput): UndoEntry {
      const entry: UndoEntry = {
        ...input,
        id: newId(),
        key: newId(),
        at: now(),
        status: "done",
      };
      entries = [...entries.filter(live), entry].slice(-MAX_ENTRIES);
      changed();
      return entry;
    },

    get(id: string): UndoEntry | undefined {
      return entries.find((e) => e.id === id);
    },

    /** The newest decision in this space that ⌘Z can still undo. */
    latest(source: string, space: string): UndoEntry | null {
      for (let i = entries.length - 1; i >= 0; i--) {
        const e = entries[i];
        if (e.source !== source || e.space.slug !== space) continue;
        if (e.status === "done" && live(e)) return e;
      }
      return null;
    },

    update(id: string, patch: Partial<Pick<UndoEntry, "status">>) {
      entries = entries.map((e) => (e.id === id ? { ...e, ...patch } : e));
      changed();
    },

    /** Takes an entry off the stack: a refusal another ⌘Z would only repeat. */
    drop(id: string) {
      entries = entries.filter((e) => e.id !== id);
      changed();
    },

    snapshot(): readonly UndoEntry[] {
      return entries;
    },

    subscribe(listener: () => void): () => void {
      listeners.add(listener);
      return () => listeners.delete(listener);
    },

    emit(event: UndoEvent) {
      for (const watcher of watchers) watcher(event);
    },

    watch(watcher: (event: UndoEvent) => void): () => void {
      watchers.add(watcher);
      return () => watchers.delete(watcher);
    },
  };
}

export type UndoStack = ReturnType<typeof createUndoStack>;

/** This tab's stack. */
export const undoStack = createUndoStack();
