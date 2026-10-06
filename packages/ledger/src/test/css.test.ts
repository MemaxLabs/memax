import { readFileSync, readdirSync } from "node:fs";
import { createRequire } from "node:module";
import { dirname, join, relative } from "node:path";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";

// The Ledger's visual rules, checked over the CSS itself (design review §4).

const SRC = join(dirname(fileURLToPath(import.meta.url)), "..");
const require = createRequire(import.meta.url);
const TOKENS_CSS = readFileSync(
  require.resolve("@memaxlabs/ledger-tokens/tokens.css"),
  "utf8",
);

function walk(dir: string, ext: string): string[] {
  return readdirSync(dir, { withFileTypes: true }).flatMap((entry) => {
    const path = join(dir, entry.name);
    if (entry.isDirectory()) return walk(path, ext);
    return entry.name.endsWith(ext) ? [path] : [];
  });
}

const CSS_FILES = walk(SRC, ".css");
const read = (path: string) => readFileSync(path, "utf8");
const stripComments = (css: string) => css.replace(/\/\*[\s\S]*?\*\//g, "");

interface Declaration {
  file: string;
  property: string;
  value: string;
}

/** Declarations from the innermost blocks (rules inside @media and @keyframes included). */
function declarations(file: string): Declaration[] {
  const css = stripComments(read(file));
  const out: Declaration[] = [];
  for (const block of css.matchAll(/\{([^{}]*)\}/g)) {
    for (const part of (block[1] ?? "").split(";")) {
      const colon = part.indexOf(":");
      if (colon < 0) continue;
      const property = part.slice(0, colon).trim().toLowerCase();
      const value = part.slice(colon + 1).trim();
      if (property && value)
        out.push({ file: relative(SRC, file), property, value });
    }
  }
  return out;
}

const ALL = CSS_FILES.flatMap(declarations);

// CSS named colours (Level 4), minus the keywords Ledger allows.
const NAMED_COLOURS =
  "aliceblue antiquewhite aqua aquamarine azure beige bisque black blanchedalmond blue blueviolet brown burlywood cadetblue chartreuse chocolate coral cornflowerblue cornsilk crimson cyan darkblue darkcyan darkgoldenrod darkgray darkgreen darkgrey darkkhaki darkmagenta darkolivegreen darkorange darkorchid darkred darksalmon darkseagreen darkslateblue darkslategray darkslategrey darkturquoise darkviolet deeppink deepskyblue dimgray dimgrey dodgerblue firebrick floralwhite forestgreen fuchsia gainsboro ghostwhite gold goldenrod gray green greenyellow grey honeydew hotpink indianred indigo ivory khaki lavender lavenderblush lawngreen lemonchiffon lightblue lightcoral lightcyan lightgoldenrodyellow lightgray lightgreen lightgrey lightpink lightsalmon lightseagreen lightskyblue lightslategray lightslategrey lightsteelblue lightyellow lime limegreen linen magenta maroon mediumaquamarine mediumblue mediumorchid mediumpurple mediumseagreen mediumslateblue mediumspringgreen mediumturquoise mediumvioletred midnightblue mintcream mistyrose moccasin navajowhite navy oldlace olive olivedrab orange orangered orchid palegoldenrod palegreen paleturquoise palevioletred papayawhip peru pink plum powderblue purple rebeccapurple red rosybrown royalblue saddlebrown salmon sandybrown seagreen seashell sienna silver skyblue slateblue slategray slategrey snow springgreen steelblue tan teal thistle tomato turquoise violet wheat white whitesmoke yellow yellowgreen canvas canvastext linktext visitedtext activetext buttonface buttontext field fieldtext highlight highlighttext graytext mark marktext".split(
    " ",
  );

/** Hex, functional and named colours anywhere outside a custom property definition. */
function colourOffenders(decls: Declaration[]): string[] {
  const offenders: string[] = [];
  for (const { file, property, value } of decls) {
    if (property.startsWith("--")) continue;
    const v = value.toLowerCase();
    const where = `${file}: ${property}: ${value}`;
    if (/#[0-9a-f]{3,8}\b/.test(v)) offenders.push(where);
    else if (/\b(rgba?|hsla?|hwb|lab|lch|oklab|oklch|color)\(/.test(v))
      offenders.push(where);
    else if (property !== "font-family") {
      // Named colours, as whole words outside var() and quoted strings.
      const words = v
        .replace(/var\([^)]*\)/g, " ")
        .replace(/"[^"]*"|'[^']*'/g, " ");
      const named = words
        .split(/[^a-z-]+/)
        .find((word) => NAMED_COLOURS.includes(word));
      if (named) offenders.push(`${where} (${named})`);
    }
  }
  return offenders;
}

describe("ledger CSS", () => {
  it("has the handoff bundle and the hardening layer", () => {
    expect(CSS_FILES.length).toBeGreaterThanOrEqual(10);
    expect(ALL.length).toBeGreaterThan(500);
  });

  it("never uses a literal colour", () => {
    expect(colourOffenders(ALL)).toEqual([]);
  });

  it("would catch a literal colour", () => {
    const decl = (property: string, value: string) => ({
      file: "x.css",
      property,
      value,
    });
    expect(
      colourOffenders([
        decl("color", "#fff"),
        decl("background", "rgb(0 0 0 / 50%)"),
        decl("border", "1px solid white"),
        decl("fill", "oklch(70% 0.1 150)"),
        decl("background", "color-mix(in oklab, #000 10%, transparent)"),
      ]),
    ).toHaveLength(5);
    expect(
      colourOffenders([
        decl("color", "var(--ink)"),
        decl("background", "transparent"),
        decl("fill", "currentColor"),
        decl("background", "color-mix(in oklab, var(--ink) 10%, transparent)"),
        decl("--private", "#fff"),
      ]),
    ).toEqual([]);
  });

  it("only uses custom properties the tokens (or the rule itself) define", () => {
    const defined = new Set(
      [...TOKENS_CSS.matchAll(/(--[a-z0-9-]+)\s*:/g)].map((m) => m[1]),
    );
    for (const { property } of ALL)
      if (property.startsWith("--")) defined.add(property);
    const missing = new Set<string>();
    for (const { file, value } of ALL) {
      for (const m of value.matchAll(/var\((--[a-z0-9-_]+)/g)) {
        if (!defined.has(m[1]!)) missing.add(`${file}: ${m[1]}`);
      }
    }
    expect([...missing]).toEqual([]);
  });

  it("has no glass, gradients or surface blur", () => {
    const offenders = ALL.filter(
      ({ property, value }) =>
        property === "backdrop-filter" ||
        property === "-webkit-backdrop-filter" ||
        /gradient\(/.test(value) ||
        // The handoff's italic → roman cross-fade lifts the italic away with a
        // 1px blur while it fades to nothing. That transient is the only blur.
        (/blur\(/.test(value) && value !== "blur(1px)"),
    );
    expect(offenders).toEqual([]);
    const blurs = ALL.filter(({ value }) => /blur\(/.test(value));
    expect(blurs.map((d) => d.file)).toEqual(["styles/memory.css"]);
  });

  it("imports every style file from ledger.css, bundle first and hardening last", () => {
    const entry = read(join(SRC, "ledger.css"));
    const imports = [
      ...entry.matchAll(/@import "\.\/styles\/([a-z-]+\.css)";/g),
    ].map((m) => m[1]);
    expect(imports).toEqual([
      "base.css",
      "brand.css",
      "primitives.css",
      "provenance.css",
      "memory.css",
      "agents.css",
      "frame.css",
      "motion.css",
      "hardening.css",
    ]);
    const styleFiles = readdirSync(join(SRC, "styles")).sort();
    expect([...imports].sort()).toEqual(styleFiles);
  });

  it("defines every mx- class the components use", () => {
    const css = CSS_FILES.map(read).join("\n");
    const definedClasses = new Set(
      [...css.matchAll(/\.(mx-[a-z0-9-]+)/g)].map((m) => m[1]),
    );
    const used = new Set<string>();
    for (const file of walk(SRC, ".tsx")) {
      const source = read(file);
      // Whole class names in string literals; template parts like `mx-btn--${v}` are skipped.
      for (const m of source.matchAll(
        /["'` ](mx-[a-z0-9-]*[a-z0-9])(?=["'` ])/g,
      ))
        used.add(m[1]!);
    }
    // Structural hooks in the handoff's own markup that the bundle never styles.
    const handoffHooks = new Set([
      "mx-agent-id",
      "mx-btn-label",
      "mx-glyph-arc",
      "mx-lineage-body",
      "mx-receipt-source",
      "mx-slip-status",
    ]);
    const undefinedClasses = [...used].filter(
      (name) => !definedClasses.has(name) && !handoffHooks.has(name),
    );
    expect(undefinedClasses).toEqual([]);
  });

  it("keeps the motion off under reduced motion, everywhere", () => {
    const hardening = stripComments(read(join(SRC, "styles/hardening.css")));
    expect(hardening).toMatch(
      /@media \(prefers-reduced-motion: reduce\) \{\s*\[class\*="mx-"\], \[class\*="mx-"\]::before, \[class\*="mx-"\]::after \{ animation: none !important; transition: none !important; \}/,
    );
  });

  it("sets proposed Chinese upright with an ochre rule, never a synthesised oblique (D1)", () => {
    const hardening = read(join(SRC, "styles/hardening.css"));
    expect(hardening).toContain("PENDING DESIGNER APPROVAL");
    const rule = hardening.match(
      /:is\([^)]*\.mx-review-statement\.is-proposed[^)]*\):lang\(zh\) \{([^}]*)\}/,
    );
    expect(rule?.[1]).toContain("font-style: normal");
    expect(rule?.[1]).toContain("font-synthesis: none");
    expect(rule?.[1]).toContain("border-left: 2px solid var(--ochre)");
    expect(rule?.[1]).toContain("color: var(--ink-2)");
  });

  it("keeps a clickable stale row's dotted underline on its button", () => {
    // Decorations don't propagate into atomic inline boxes such as a
    // button, so MemoryRow onClick would lose the stale state's mark.
    const hardening = stripComments(read(join(SRC, "styles/hardening.css")));
    expect(hardening).toMatch(
      /\.mx-row\.is-stale button\.mx-row-link \{ text-decoration: inherit; \}/,
    );
  });

  it("sets ReviewCard's reason as a sentence, not in tabular figures", () => {
    // Schibsted Grotesk's tabular figures widen . and , too, which reads
    // as a space before the punctuation (design review §2).
    const hardening = stripComments(read(join(SRC, "styles/hardening.css")));
    expect(hardening).toMatch(
      /\.mx-review-foot > \.mx-meta \{ font-variant-numeric: normal; \}/,
    );
  });

  it("never wraps the terminal mid-token", () => {
    const body = declarations(join(SRC, "styles/hardening.css")).filter((d) =>
      ["white-space", "overflow-x"].includes(d.property),
    );
    expect(body).toContainEqual(
      expect.objectContaining({ property: "white-space", value: "pre" }),
    );
    expect(body).toContainEqual(
      expect.objectContaining({ property: "overflow-x", value: "auto" }),
    );
  });

  it("narrows the receipt rail to 176px between 1024 and 1180px (D4)", () => {
    const hardening = stripComments(read(join(SRC, "styles/hardening.css")));
    expect(hardening).toMatch(
      /@media \(min-width: 1024px\) and \(max-width: 1180px\) \{\s*\.mx-row \{ --receipt-rail: 176px; \}/,
    );
  });
});
