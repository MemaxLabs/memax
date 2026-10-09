import { describe, expect, it } from "vitest";
import { ledgerEn } from "./en";
import { ledgerZh } from "./zh";

// Ledger voice lint over the V2 catalogue (plan §6.7, design-system
// README "Voice"). V1 namespaces keep their own voice until cutover.

function strings(value: unknown, path: string[] = []): [string, string][] {
  if (typeof value === "string") return [[path.join("."), value]];
  if (value && typeof value === "object") {
    return Object.entries(value).flatMap(([key, child]) =>
      strings(child, [...path, key]),
    );
  }
  return [];
}

const BANNED_EN =
  /\bAI\b|\bmagic|\bsmart|\bdelet|\bsav(e|es|ed|ing)\b|\bapprov|\bsupercharg|\bseamless/i;
const BANNED_ZH = /AI|智能|魔法|删除|保存|批准/;
const EXCLAMATION = /[!！]/;
const EMOJI = /\p{Extended_Pictographic}/u;

describe("Ledger catalogue voice", () => {
  it.each(strings(ledgerEn))("en %s", (_, text) => {
    expect(text).not.toMatch(BANNED_EN);
    expect(text).not.toMatch(EXCLAMATION);
    expect(text).not.toMatch(EMOJI);
  });

  it.each(strings(ledgerZh))("zh %s", (_, text) => {
    expect(text).not.toMatch(BANNED_ZH);
    expect(text).not.toMatch(EXCLAMATION);
    expect(text).not.toMatch(EMOJI);
  });

  it("has the same keys in en and zh", () => {
    const keys = (catalogue: unknown) =>
      strings(catalogue)
        .map(([key]) => key)
        .sort();
    expect(keys(ledgerZh)).toEqual(keys(ledgerEn));
  });
});
