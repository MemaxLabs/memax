import { readdirSync, readFileSync } from "node:fs";
import { createRequire } from "node:module";
import path from "node:path";
import { describe, expect, it } from "vitest";
import tokens from "@memaxlabs/ledger-tokens/tokens.json";
import { en } from "@/i18n/locales/en";
import { zh } from "@/i18n/locales/zh";

// The specimen page is built from tokens.json. These checks make sure
// tokens.json describes everything tokens.css and type.css define, so
// "every token, every type style" stays true when the package changes.

const require = createRequire(import.meta.url);
const tokensDir = path.dirname(
  require.resolve("@memaxlabs/ledger-tokens/tokens.json"),
);
const tokensCss = readFileSync(path.join(tokensDir, "tokens.css"), "utf8");
const typeCss = readFileSync(path.join(tokensDir, "type.css"), "utf8");

/** Custom properties declared inside the blocks whose selector matches. */
function declaredVars(css: string, selector: RegExp): Set<string> {
  const names = new Set<string>();
  for (const block of css.matchAll(/([^{}]+)\{([^{}]*)\}/g)) {
    if (!selector.test(block[1])) continue;
    for (const decl of block[2].matchAll(/--([a-z0-9-]+)\s*:/g)) {
      names.add(decl[1]);
    }
  }
  return names;
}

const lightVars = declaredVars(tokensCss, /data-theme="light"/);
const darkVars = declaredVars(tokensCss, /data-theme="dark"/);
const allVars = declaredVars(tokensCss, /./);

describe("token specimen coverage", () => {
  it("shows every colour token of both themes", () => {
    const shown = new Set([
      ...tokens.color.tokens.map((t) => t.name),
      ...tokens.shadow.tokens.map((t) => t.name),
    ]);
    expect([...lightVars].filter((name) => !shown.has(name))).toEqual([]);
    expect([...darkVars].filter((name) => !shown.has(name))).toEqual([]);
  });

  it("shows every space, radius and size token", () => {
    const shown = new Set([
      ...tokens.spacing.tokens.map((t) => t.name),
      ...tokens.radius.tokens.map((t) => t.name),
      ...tokens.size.tokens.map((t) => t.name),
    ]);
    const scale = [...allVars].filter((name) =>
      /^(space|radius|control)-|^(rail|receipt-rail|measure)$/.test(name),
    );
    expect(scale.filter((name) => !shown.has(name))).toEqual([]);
  });

  it("lists exactly the type styles in type.css, each with a sample", () => {
    const cssClasses = [...typeCss.matchAll(/^\.([a-z-]+)\s*\{/gm)].map(
      (m) => m[1],
    );
    const jsonStyles = tokens.type.groups.flatMap((g) =>
      g.styles.map((s) => s.name),
    );
    expect(jsonStyles.sort()).toEqual([...cssClasses].sort());
    for (const name of cssClasses) {
      expect(en.ledger.devTokens.samples, name).toHaveProperty([name]);
      expect(zh.ledger.devTokens.samples, name).toHaveProperty([name]);
    }
  });

  it("names every state glyph in ledger-tokens/assets/icons", () => {
    const glyphs = readdirSync(path.join(tokensDir, "assets/icons"))
      .filter((file) => file.endsWith(".svg"))
      .map((file) => file.replace(/^state-|\.svg$/g, ""));
    expect(glyphs.sort()).toEqual(
      Object.keys(en.ledger.devTokens.states).sort(),
    );
  });
});
