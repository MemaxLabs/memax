const MODIFIERS: Record<string, string> = {
  "⌘": "Meta",
  "⌃": "Control",
  "⌥": "Alt",
  "⇧": "Shift",
};

const KEYS: Record<string, string> = {
  "↵": "Enter",
  "⏎": "Enter",
  "↑": "ArrowUp",
  "↓": "ArrowDown",
  "←": "ArrowLeft",
  "→": "ArrowRight",
  "⌫": "Backspace",
  Esc: "Escape",
  Tab: "Tab",
  Space: "Space",
};

/**
 * Converts a keycap as drawn ("⌘Z", "⌘↵", "K", "Esc") into the
 * `aria-keyshortcuts` syntax ("Meta+Z", "Meta+Enter", "K", "Escape").
 * Returns undefined for a cap it can't read, so nothing wrong is announced.
 */
export function toAriaKeyshortcuts(cap: string): string | undefined {
  const chars = [...cap.trim()];
  const mods: string[] = [];
  while (chars.length > 1 && chars[0] !== undefined && MODIFIERS[chars[0]]) {
    mods.push(MODIFIERS[chars.shift()!]!);
  }
  const rest = chars.join("");
  if (!rest) return undefined;
  const key =
    KEYS[rest] ?? ([...rest].length === 1 ? rest.toUpperCase() : undefined);
  return key ? [...mods, key].join("+") : undefined;
}
