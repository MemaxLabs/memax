import { describe, expect, it } from "vitest";
import { en } from "./en";
import { zh } from "./zh";

// The voice rules (design review §4, master plan §6.7) over the catalogues.

type Tree = { [key: string]: string | Tree };

function leaves(tree: Tree, prefix = ""): Array<[string, string]> {
  return Object.entries(tree).flatMap(([key, value]) =>
    typeof value === "string"
      ? [[`${prefix}${key}`, value] as [string, string]]
      : leaves(value, `${prefix}${key}.`),
  );
}

const EN = leaves(en as unknown as Tree);
const ZH = leaves(zh as unknown as Tree);
const placeholders = (s: string) =>
  [...s.matchAll(/\{(\w+)\}/g)].map((m) => m[1]).sort();
const EMOJI = /\p{Extended_Pictographic}/u;

describe("string catalogues", () => {
  it("have the same keys in en and zh", () => {
    expect(ZH.map(([key]) => key)).toEqual(EN.map(([key]) => key));
  });

  it("have no empty strings", () => {
    for (const [key, value] of [...EN, ...ZH])
      expect(value.trim(), key).not.toBe("");
  });

  it("use the same placeholders in every locale", () => {
    const zhByKey = new Map(ZH);
    for (const [key, value] of EN) {
      expect(placeholders(zhByKey.get(key) ?? ""), key).toEqual(
        placeholders(value),
      );
    }
  });

  it("keep the English voice: no AI, magic, smart, delete, save, approve, ! or emoji", () => {
    const banned = /\bAI\b|magic|smart|delete|\bsav(e|es|ed|ing)\b|approv|!/i;
    for (const [key, value] of EN) {
      expect(banned.test(value), `${key}: ${value}`).toBe(false);
      expect(EMOJI.test(value), `${key}: ${value}`).toBe(false);
    }
  });

  it("keep the Chinese voice: no AI, 智能, 删除, 保存, 批准, exclamation marks or emoji", () => {
    const banned = /AI|智能|删除|保存|批准|[!！]/;
    for (const [key, value] of ZH) {
      expect(banned.test(value), `${key}: ${value}`).toBe(false);
      expect(EMOJI.test(value), `${key}: ${value}`).toBe(false);
    }
  });

  it("never put a space before punctuation (design review §2)", () => {
    for (const [key, value] of [...EN, ...ZH]) {
      expect(/\s[.,;:!?，。：；！？]/.test(value), `${key}: ${value}`).toBe(
        false,
      );
    }
  });

  it("use sentence case for English labels", () => {
    // A label never capitalises a second word, except proper nouns and acronyms.
    const allowed =
      /^(MCP|CLI|IDE|Memax|Dream|Review|Kept|Proposed|Forgotten|Done|Needs|Esc|Tab)$/;
    for (const [key, value] of EN) {
      const sentences = value.replace(/\{\w+\}/g, "").split(/[.?]\s+/);
      for (const sentence of sentences) {
        for (const word of sentence.split(/\s+/).slice(1)) {
          const bare = word.replace(/[^A-Za-z]/g, "");
          if (/^[A-Z][a-z]/.test(bare) && !allowed.test(bare)) {
            throw new Error(`${key}: "${value}" capitalises "${word}"`);
          }
        }
      }
    }
  });

  it("use the Ledger verbs", () => {
    expect(en.review.keep).toBe("Keep");
    expect(en.review.edit).toBe("Edit");
    expect(en.review.reject).toBe("Reject");
    expect(en.command.remember).toBe("Remember");
    expect(zh.review.keep).toBe("保留");
    expect(zh.command.remember).toBe("记住");
    expect(zh.review.undo).toBe("撤销");
  });
});
