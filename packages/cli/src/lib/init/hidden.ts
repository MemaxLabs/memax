// Counting the hidden characters an import strips, by kind, so init can
// say what it removed: bidi controls (the "rules file backdoor" reorders
// what a person sees), zero-width characters, Unicode tag characters
// ("ASCII smuggling" hides whole sentences in them), variation selectors
// (they can carry hidden bytes) and other invisible controls. The text
// itself is cleaned by the compiler's cleanLine (sanitize.ts), which
// removes every one of these; this only names them. Line breaks and tabs
// aren't hidden characters.

export interface HiddenCounts {
  bidi: number;
  zero_width: number;
  tag: number;
  variation: number;
  other: number;
}

export function emptyHidden(): HiddenCounts {
  return { bidi: 0, zero_width: 0, tag: 0, variation: 0, other: 0 };
}

export function totalHidden(h: HiddenCounts): number {
  return h.bidi + h.zero_width + h.tag + h.variation + h.other;
}

export function addHidden(into: HiddenCounts, h: HiddenCounts): void {
  into.bidi += h.bidi;
  into.zero_width += h.zero_width;
  into.tag += h.tag;
  into.variation += h.variation;
  into.other += h.other;
}

const BIDI = /[\u{061C}\u{200E}\u{200F}\u{202A}-\u{202E}\u{2066}-\u{2069}]/u;
const ZERO_WIDTH = /[\u{200B}-\u{200D}\u{2060}\u{FEFF}\u{180E}\u{00AD}]/u;
const TAG = /[\u{E0000}-\u{E007F}]/u;
const VARIATION = /[\u{FE00}-\u{FE0F}\u{E0100}-\u{E01EF}]/u;
const OTHER = /[\p{Cc}\p{Cf}\u{034F}\u{115F}\u{1160}\u{3164}\u{FFA0}]/u;

/** Counts the hidden characters in text, by kind. */
export function countHidden(text: string): HiddenCounts {
  const h = emptyHidden();
  for (const ch of text) {
    if (ch === "\n" || ch === "\r" || ch === "\t") continue;
    if (BIDI.test(ch)) h.bidi++;
    else if (TAG.test(ch)) h.tag++;
    else if (ZERO_WIDTH.test(ch)) h.zero_width++;
    else if (VARIATION.test(ch)) h.variation++;
    else if (OTHER.test(ch)) h.other++;
  }
  return h;
}

/** "3 bidi controls, 1 zero-width character". */
export function describeHidden(h: HiddenCounts): string {
  const parts: string[] = [];
  const say = (n: number, one: string, many: string) => {
    if (n > 0) parts.push(`${n} ${n === 1 ? one : many}`);
  };
  say(h.bidi, "bidi control", "bidi controls");
  say(h.zero_width, "zero-width character", "zero-width characters");
  say(h.tag, "tag character", "tag characters");
  say(h.variation, "variation selector", "variation selectors");
  say(h.other, "other invisible character", "other invisible characters");
  return parts.join(", ");
}
