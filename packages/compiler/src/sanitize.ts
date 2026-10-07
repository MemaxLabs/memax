/**
 * Hidden-character stripping.
 *
 * Agents read these files verbatim, so a character a person can't see is a
 * place to hide an instruction: the "rules file backdoor" (bidi controls and
 * zero-width characters in Cursor and Copilot rules), and "ASCII smuggling"
 * through Unicode tag characters. Every piece of text the compiler writes
 * goes through {@link cleanLine} first, and parse-back cleans what it reads.
 *
 * What goes:
 * - control characters (C0, DEL, C1); line breaks and tabs become spaces;
 * - format characters (category Cf): bidi embeddings, overrides and
 *   isolates, LRM/RLM/ALM, zero-width space and joiners, word joiner,
 *   invisible operators, BOM, soft hyphen, interlinear annotations and tag
 *   characters;
 * - variation selectors (they can carry hidden bytes), the combining
 *   grapheme joiner and the Hangul fillers that render as nothing;
 * - lone surrogates, which become U+FFFD as they would in any UTF-8 encoder.
 *
 * The cost is small and deliberate: emoji lose their presentation selector
 * and ZWJ sequences split, and languages that use ZWNJ (Persian, for one)
 * lose it. Memory text is plain language, so we take that trade.
 *
 * Escapes only below: a source file about invisible characters shouldn't
 * contain any.
 */

/** Line breaks (including NEL, LS and PS) and tabs, which become spaces. */
const SPACES = /\r\n|[\r\n\t\v\f\u{85}\u{2028}\u{2029}]/gu;

/**
 * Cc and Cf, CGJ, the Hangul fillers, and both variation selector blocks.
 * Alternatives rather than one class, because several of these combine with
 * their neighbours inside a class.
 */
const HIDDEN =
  /[\p{Cc}\p{Cf}]|\u{34F}|\u{115F}|\u{1160}|\u{3164}|\u{FFA0}|[\u{FE00}-\u{FE0F}]|[\u{E0100}-\u{E01EF}]/gu;

const LONE_SURROGATE =
  /[\uD800-\uDBFF](?![\uDC00-\uDFFF])|(?<![\uD800-\uDBFF])[\uDC00-\uDFFF]/g;

const REPLACEMENT = "\u{FFFD}";

export interface Cleaned {
  text: string;
  /** How many hidden characters were removed (line breaks and tabs don't count). */
  removed: number;
}

/**
 * Makes one clean line of text: line breaks and tabs become spaces, hidden
 * characters go, runs of spaces collapse, and the result is NFC-normalised
 * and trimmed.
 */
export function cleanLine(text: string): Cleaned {
  let removed = 0;
  const visible = text
    .replace(LONE_SURROGATE, REPLACEMENT)
    .replace(SPACES, " ")
    .replace(HIDDEN, () => {
      removed += 1;
      return "";
    });
  return {
    text: visible.normalize("NFC").replace(/ {2,}/g, " ").trim(),
    removed,
  };
}
