/**
 * Key specs for the V2 keymap: parsing, matching a KeyboardEvent, and
 * the keycaps each platform shows.
 *
 * A spec is written once, platform-neutral:
 *   "Mod+K"        ⌘K on a Mac, Ctrl+K elsewhere
 *   "G T"          a sequence: G, then T
 *   "?" "K" "ArrowDown" "Mod+Shift+C" "Mod+Enter" "Mod+1"
 *
 * Pure and DOM-free so it runs in unit tests and in the keymap.
 */

export type Platform = "mac" | "other";

export interface Chord {
  /** Normalised: a lowercase letter, a digit, a punctuation character, or a lowercase key name ("arrowdown"). */
  key: string;
  /** ⌘ on a Mac, Ctrl elsewhere. */
  mod: boolean;
  shift: boolean;
  alt: boolean;
}

export type KeySequence = readonly Chord[];

/** The parts of a KeyboardEvent the keymap reads. */
export interface KeyEventLike {
  key: string;
  code?: string;
  metaKey: boolean;
  ctrlKey: boolean;
  altKey: boolean;
  shiftKey: boolean;
  repeat?: boolean;
  isComposing?: boolean;
  /** Legacy, but the only IME signal Safari gives for the Enter that commits a composition. */
  keyCode?: number;
  defaultPrevented?: boolean;
  target?: unknown;
  preventDefault?: () => void;
}

const NAMED_KEYS: Record<string, string> = {
  enter: "enter",
  return: "enter",
  escape: "escape",
  esc: "escape",
  tab: "tab",
  space: " ",
  backspace: "backspace",
  delete: "delete",
  arrowup: "arrowup",
  arrowdown: "arrowdown",
  arrowleft: "arrowleft",
  arrowright: "arrowright",
  up: "arrowup",
  down: "arrowdown",
  left: "arrowleft",
  right: "arrowright",
};

const MODIFIER_KEYS = new Set(["meta", "control", "alt", "shift", "os"]);

/** Whether the event is a modifier pressed on its own. */
export function isModifierKey(event: KeyEventLike): boolean {
  return MODIFIER_KEYS.has(event.key.toLowerCase());
}

function parseChord(part: string, spec: string): Chord {
  const pieces = part.split("+");
  // "Mod++" would be a plus key; nothing in the map needs it.
  const keyPart = pieces.pop();
  if (!keyPart) throw new Error(`Empty key in "${spec}"`);
  const chord: Chord = { key: "", mod: false, shift: false, alt: false };
  for (const raw of pieces) {
    const mod = raw.toLowerCase();
    if (mod === "mod") chord.mod = true;
    else if (mod === "shift") chord.shift = true;
    else if (mod === "alt") chord.alt = true;
    else throw new Error(`Unknown modifier "${raw}" in "${spec}"`);
  }
  const lower = keyPart.toLowerCase();
  if (NAMED_KEYS[lower] !== undefined) chord.key = NAMED_KEYS[lower];
  else if ([...keyPart].length === 1) chord.key = lower;
  else throw new Error(`Unknown key "${keyPart}" in "${spec}"`);
  return chord;
}

/** "G T" → [g, t]; "Mod+Shift+C" → [⌘⇧c]. Throws on a spec it can't read. */
export function parseKeys(spec: string): KeySequence {
  const parts = spec.trim().split(/\s+/);
  if (parts.length === 0 || parts[0] === "") {
    throw new Error("Empty key spec");
  }
  return parts.map((part) => parseChord(part, spec));
}

const ASCII_LETTER = /^[a-z]$/;
const DIGIT = /^[0-9]$/;

/** Letters and digits: keys whose meaning Shift changes. */
function isAlphanumeric(key: string): boolean {
  return ASCII_LETTER.test(key) || DIGIT.test(key);
}

/**
 * The event's key, normalised like a Chord's. Letters and digits fall
 * back to the physical key (`code`) when the layout or a modifier
 * produced another character (a Cyrillic layout, ⌥ on a Mac, ⇧1 = !),
 * so G T and ⌘1 work on any layout.
 */
export function eventKey(event: KeyEventLike): string {
  const key = event.key;
  if ([...key].length === 1) {
    const lower = key.toLowerCase();
    if (ASCII_LETTER.test(lower) || DIGIT.test(lower)) return lower;
    const code = event.code ?? "";
    const letter = /^Key([A-Z])$/.exec(code);
    if (letter && (event.altKey || !/^[\x20-\x7e]$/.test(key))) {
      return letter[1].toLowerCase();
    }
    const digit = /^(?:Digit|Numpad)([0-9])$/.exec(code);
    if (digit && (event.shiftKey || event.altKey)) return digit[1];
    return lower;
  }
  return NAMED_KEYS[key.toLowerCase()] ?? key.toLowerCase();
}

/** Whether a modifier chord is held: ⌘ on a Mac, Ctrl elsewhere, and not the other one. */
function modHeld(event: KeyEventLike, platform: Platform): boolean {
  return platform === "mac"
    ? event.metaKey && !event.ctrlKey
    : event.ctrlKey && !event.metaKey;
}

function noMod(event: KeyEventLike): boolean {
  return !event.metaKey && !event.ctrlKey;
}

/** Whether the event presses this chord. Punctuation ignores Shift ("?" is ⇧/ on most layouts). */
export function matchesChord(
  event: KeyEventLike,
  chord: Chord,
  platform: Platform,
): boolean {
  if (eventKey(event) !== chord.key) return false;
  if (chord.mod ? !modHeld(event, platform) : !noMod(event)) return false;
  if (event.altKey !== chord.alt) return false;
  const shiftMatters =
    isAlphanumeric(chord.key) || [...chord.key].length > 1 || chord.shift;
  if (shiftMatters && event.shiftKey !== chord.shift) return false;
  return true;
}

/** Whether a chord is a bare key (no ⌘/Ctrl/⌥), the kind typing and IMEs produce. */
export function isBareChord(chord: Chord): boolean {
  return !chord.mod && !chord.alt;
}

const MAC_GLYPHS: Record<string, string> = {
  enter: "↵",
  escape: "Esc",
  tab: "Tab",
  " ": "Space",
  backspace: "⌫",
  delete: "⌦",
  arrowup: "↑",
  arrowdown: "↓",
  arrowleft: "←",
  arrowright: "→",
};

const OTHER_NAMES: Record<string, string> = {
  ...MAC_GLYPHS,
  enter: "Enter",
  backspace: "Backspace",
  delete: "Delete",
};

function keyLabel(key: string, platform: Platform): string {
  const names = platform === "mac" ? MAC_GLYPHS : OTHER_NAMES;
  return names[key] ?? key.toUpperCase();
}

/**
 * One keycap's text. A Mac stacks glyphs ("⌘⇧C", "⌘↵"), as the boards
 * draw them; other platforms spell the modifiers ("Ctrl+Shift+C").
 */
export function formatChord(chord: Chord, platform: Platform): string {
  const key = keyLabel(chord.key, platform);
  if (platform === "mac") {
    return `${chord.mod ? "⌘" : ""}${chord.alt ? "⌥" : ""}${chord.shift ? "⇧" : ""}${key}`;
  }
  const mods = [
    chord.mod && "Ctrl",
    chord.alt && "Alt",
    chord.shift && "Shift",
  ].filter(Boolean);
  return [...mods, key].join("+");
}

/** The keycaps for a spec: one per step of a sequence ("G T" → ["G", "T"]). */
export function keycaps(spec: string, platform: Platform): string[] {
  return parseKeys(spec).map((chord) => formatChord(chord, platform));
}

const ARIA_KEYS: Record<string, string> = {
  enter: "Enter",
  escape: "Escape",
  tab: "Tab",
  " ": "Space",
  backspace: "Backspace",
  delete: "Delete",
  arrowup: "ArrowUp",
  arrowdown: "ArrowDown",
  arrowleft: "ArrowLeft",
  arrowright: "ArrowRight",
};

/**
 * The aria-keyshortcuts value for a single-chord spec ("Meta+K",
 * "Control+Shift+C"). Sequences have no aria syntax, so they return
 * undefined rather than announcing the wrong thing.
 */
export function ariaKeyshortcuts(
  spec: string,
  platform: Platform,
): string | undefined {
  const sequence = parseKeys(spec);
  if (sequence.length !== 1) return undefined;
  const chord = sequence[0];
  const mods = [
    chord.mod && (platform === "mac" ? "Meta" : "Control"),
    chord.alt && "Alt",
    chord.shift && "Shift",
  ].filter(Boolean);
  const key = ARIA_KEYS[chord.key] ?? chord.key.toUpperCase();
  return [...mods, key].join("+");
}

interface NavigatorLike {
  platform?: string;
  userAgent?: string;
  userAgentData?: { platform?: string };
}

/** Apple platforms use ⌘; everything else uses Ctrl. */
export function detectPlatform(nav: NavigatorLike | undefined): Platform {
  const name =
    nav?.userAgentData?.platform || nav?.platform || nav?.userAgent || "";
  return /mac|iphone|ipad|ipod/i.test(name) ? "mac" : "other";
}
