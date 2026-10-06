/**
 * The V2 keyboard map: every binding the app has, declared once (plan
 * §6.4, HANDOFF §7 "Keyboard map"). The dispatcher (keymap.ts) reads
 * these keys, and the `?` sheet is generated from this list, so the
 * sheet can't drift from what the keys do.
 *
 * Declaring a binding doesn't make it fire: a mounted screen registers
 * a handler for its id in a scope (useHotkey): Review's keys live in
 * its screen (_places/review/review-keys.ts).
 *
 * Labels are i18n: `t.ledger.keys.actions[id]` (registry.test.ts checks
 * en and zh have one for every id).
 */

/** Where a binding is listed on the `?` sheet. */
export type KeyGroup =
  /** The sheet's first column. */
  | "everywhere"
  /** Review's queue. */
  | "review"
  /** A memory's page or the Brief. */
  | "memory"
  /** Keys a single page or layer adds, listed under the three columns. */
  | "pages";

export const KEY_GROUPS: readonly KeyGroup[] = [
  "everywhere",
  "review",
  "memory",
  "pages",
];

export interface KeyBinding {
  readonly id: string;
  /**
   * Alternatives, each a spec from keys.ts. The handler learns which
   * one matched (its index): ↓/↑ in Review, ⌘1…⌘9 for spaces.
   * `null` means no shortcut, on purpose: Forget.
   */
  readonly keys: readonly string[] | null;
  readonly group: KeyGroup;
  /** How the sheet shows several keys: each one, or the first and last ("⌘1 to ⌘9"). */
  readonly display?: "each" | "range";
  /** Fires while typing in a text field. Only for ⌘/Ctrl chords. */
  readonly inInput?: boolean;
  /** Fires again while the key is held (moving through a list). */
  readonly repeat?: boolean;
}

const SPACE_KEYS = Array.from({ length: 9 }, (_, i) => `Mod+${i + 1}`);

export const KEYMAP = [
  // Everywhere
  { id: "command.open", keys: ["Mod+K"], group: "everywhere", inInput: true },
  { id: "go.today", keys: ["G T"], group: "everywhere" },
  { id: "go.review", keys: ["G R"], group: "everywhere" },
  { id: "go.brief", keys: ["G B"], group: "everywhere" },
  { id: "go.memories", keys: ["G M"], group: "everywhere" },
  {
    id: "space.switch",
    keys: SPACE_KEYS,
    group: "everywhere",
    display: "range",
    inInput: true,
  },
  { id: "help.keys", keys: ["?"], group: "everywhere" },

  // Review
  {
    id: "review.move",
    keys: ["ArrowDown", "ArrowUp"],
    group: "review",
    repeat: true,
  },
  { id: "review.keep", keys: ["K"], group: "review" },
  { id: "review.edit", keys: ["E"], group: "review" },
  { id: "review.reject", keys: ["X"], group: "review" },
  { id: "review.compare", keys: ["C"], group: "review" },
  { id: "review.source", keys: ["O"], group: "review" },
  // Undoes the last decision anywhere a toast offers Undo, not only in
  // Review; the sheet lists it under Review, as drawn.
  { id: "undo", keys: ["Mod+Z"], group: "review" },

  // A memory or the Brief
  { id: "memory.edit", keys: ["E"], group: "memory" },
  { id: "memory.verify", keys: ["V"], group: "memory" },
  { id: "memory.cite", keys: ["Mod+Shift+C"], group: "memory" },
  { id: "memory.compile", keys: ["Mod+Shift+S"], group: "memory" },
  { id: "memory.handoff", keys: ["H"], group: "memory" },
  // Forget has no binding, on purpose (HANDOFF §7, design review §4).
  { id: "memory.forget", keys: null, group: "memory" },

  // One page or layer
  { id: "today.review", keys: ["R"], group: "pages" },
  { id: "memories.remember", keys: ["N"], group: "pages" },
  { id: "command.keep", keys: ["Mod+Enter"], group: "pages", inInput: true },
] as const satisfies readonly KeyBinding[];

export type KeyActionId = (typeof KEYMAP)[number]["id"];

const BY_ID = new Map<string, KeyBinding>(KEYMAP.map((b) => [b.id, b]));

export function bindingFor(id: KeyActionId): KeyBinding {
  const binding = BY_ID.get(id);
  if (!binding) throw new Error(`No key binding "${id}"`);
  return binding;
}
