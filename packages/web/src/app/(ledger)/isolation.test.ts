import { readdirSync, readFileSync } from "node:fs";
import { createRequire } from "node:module";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";

// Keeps V1 out of the (ledger) tree and the Ledger out of V1 (AGENTS.md,
// "V2 › UI (Ledger)"): no Tailwind, globals.css, @memaxlabs/ui,
// next-themes or V1 components in (ledger); no literal colours, glass,
// blur or gradients in V2 CSS or TSX; every static class name is a
// Ledger class. A static check, so it runs with `pnpm test`; the
// Playwright specimen test checks the built page loads no V1 CSS.

const ledgerDir = path.dirname(fileURLToPath(import.meta.url));
const appDir = path.dirname(ledgerDir);
const srcDir = path.dirname(appDir);
const require = createRequire(import.meta.url);
const typeCssPath = require.resolve("@memaxlabs/ledger-tokens/type.css");

function walk(dir: string): string[] {
  return readdirSync(dir, { withFileTypes: true }).flatMap((entry) => {
    const full = path.join(dir, entry.name);
    return entry.isDirectory() ? walk(full) : [full];
  });
}

const isTest = (file: string) => /\.test\.tsx?$/.test(file);
const ledgerFiles = walk(ledgerDir).filter((f) => !isTest(f));
const ledgerCode = ledgerFiles.filter((f) => /\.tsx?$/.test(f));
const ledgerCss = ledgerFiles.filter((f) => f.endsWith(".css"));
const rel = (file: string) => path.relative(srcDir, file);

function stripComments(source: string): string {
  return source
    .replace(/\/\*[\s\S]*?\*\//g, " ")
    .replace(/(^|[^:"'`\\])\/\/[^\n]*/g, "$1");
}

function importSpecifiers(source: string): string[] {
  const specs: string[] = [];
  const re =
    /\bimport\s+(?:[^'"`;]*?\sfrom\s+)?["']([^"']+)["']|\bimport\(\s*["']([^"']+)["']\s*\)|\brequire\(\s*["']([^"']+)["']\s*\)/g;
  for (const m of source.matchAll(re)) specs.push(m[1] ?? m[2] ?? m[3]);
  return specs;
}

const FORBIDDEN_IMPORTS: [RegExp, string][] = [
  [/globals\.css$/, "V1 globals.css"],
  [/^@memaxlabs\/ui(\/|$)/, "the V1 component library"],
  [/^next-themes$/, "next-themes (use data-theme)"],
  [/^(tailwindcss|@tailwindcss\/|tw-animate-css|tailwind-merge)/, "Tailwind"],
  [/^shadcn(\/|$)/, "shadcn"],
  [/^class-variance-authority$/, "V1 class tooling"],
  [/^(lucide-react|framer-motion|sonner|@radix-ui\/)/, "a V1 UI dependency"],
  [/^@\/components\//, "V1 components"],
  [/\(v1\)/, "the (v1) tree"],
];

// Literal colours, written as hex, a colour function or a CSS name.
const HEX_COLOUR =
  /(?<![\w&])#(?:[0-9a-fA-F]{8}|[0-9a-fA-F]{6}|[0-9a-fA-F]{3,4})\b/;
const COLOUR_FUNCTION =
  /\b(?:rgba?|hsla?|hwb|lab|lch|oklab|oklch|color|color-mix)\(/i;
const NAMED_COLOUR =
  /(?<![-\w])(?:white|black|red|green|blue|gr[ae]y|silver|yellow|orange|purple|pink|brown|navy|teal|maroon|olive|lime|aqua|fuchsia|cyan|magenta|gold|indigo|violet|beige|coral|crimson|ivory|khaki|lavender|salmon|tan|turquoise|plum|orchid|wheat|snow)(?![-\w])/i;
// V1's visual vocabulary, which Ledger rules out.
const V1_LOOK =
  /backdrop-filter|\bblur\(|-gradient\(|\bglass\b|\baurora\b|animate-spin|✦|✨/i;

describe("(ledger) imports", () => {
  it.each(ledgerCode.map(rel))("%s imports nothing from V1", (file) => {
    const source = readFileSync(path.join(srcDir, file), "utf8");
    const bad = importSpecifiers(source).flatMap((spec) =>
      FORBIDDEN_IMPORTS.filter(([re]) => re.test(spec)).map(
        ([, why]) => `${spec} (${why})`,
      ),
    );
    expect(bad).toEqual([]);
  });

  it.each(ledgerCode.map(rel))("%s imports only Ledger stylesheets", (file) => {
    const source = readFileSync(path.join(srcDir, file), "utf8");
    const css = importSpecifiers(source).filter(
      (spec) => spec.endsWith(".css") || spec === "@memaxlabs/ledger-tokens",
    );
    for (const spec of css) {
      expect(
        spec.startsWith("./") ||
          spec.startsWith("../") ||
          spec.startsWith("@memaxlabs/ledger-tokens"),
        spec,
      ).toBe(true);
    }
  });
});

describe("(ledger) CSS", () => {
  it.each(ledgerCss.map(rel))("%s uses tokens only", (file) => {
    const css = stripComments(readFileSync(path.join(srcDir, file), "utf8"));
    expect(css).not.toMatch(
      /@(tailwind|apply|theme|custom-variant|plugin|source|utility|variant)\b/,
    );
    for (const m of css.matchAll(/@import\s+(?:url\()?["']([^"']+)["']/g)) {
      expect(
        m[1].startsWith("./") || m[1].startsWith("@memaxlabs/ledger-tokens"),
        m[1],
      ).toBe(true);
    }
    for (const decl of css.matchAll(/([a-z-]+)\s*:\s*([^;{}]+)/g)) {
      const [text, , value] = decl;
      expect(value, text).not.toMatch(HEX_COLOUR);
      expect(value, text).not.toMatch(COLOUR_FUNCTION);
      expect(value, text).not.toMatch(NAMED_COLOUR);
    }
    expect(css).not.toMatch(V1_LOOK);
  });
});

describe("(ledger) TSX", () => {
  // Classes the tree may use: Ledger type styles, mx- components, and
  // global classes defined in (ledger) CSS. CSS modules are referenced
  // as styles.x and aren't static strings, so they pass by construction.
  const typeClasses = [
    ...readFileSync(typeCssPath, "utf8").matchAll(/^\.([a-z-]+)\s*\{/gm),
  ].map((m) => m[1]);
  const globalLedgerClasses = ledgerCss
    .filter((f) => !f.endsWith(".module.css"))
    .flatMap((f) => [
      ...stripComments(readFileSync(f, "utf8")).matchAll(
        /\.([a-zA-Z_][\w-]*)/g,
      ),
    ])
    .map((m) => m[1]);
  const allowed = new Set([...typeClasses, ...globalLedgerClasses]);

  function staticClassNames(source: string): string[] {
    const chunks: string[] = [];
    for (const m of source.matchAll(
      /className=(?:"([^"]*)"|'([^']*)'|\{\s*["']([^"']*)["']\s*\}|\{\s*`([^`]*)`\s*\})/g,
    )) {
      const raw = m[1] ?? m[2] ?? m[3] ?? m[4] ?? "";
      chunks.push(raw.replace(/\$\{[^}]*\}/g, " "));
    }
    return chunks.join(" ").split(/\s+/).filter(Boolean);
  }

  it.each(ledgerCode.map(rel))("%s uses Ledger classes only", (file) => {
    const source = readFileSync(path.join(srcDir, file), "utf8");
    const unknown = staticClassNames(source).filter(
      (name) => !name.startsWith("mx-") && !allowed.has(name),
    );
    expect(unknown, "Tailwind or V1 classes are not allowed").toEqual([]);
  });

  it.each(ledgerCode.map(rel))("%s has no literal colours", (file) => {
    const code = stripComments(readFileSync(path.join(srcDir, file), "utf8"));
    expect(code).not.toMatch(HEX_COLOUR);
    expect(code).not.toMatch(COLOUR_FUNCTION);
    expect(code).not.toMatch(V1_LOOK);
  });

  it("knows the type styles (sanity)", () => {
    expect(typeClasses).toEqual(
      expect.arrayContaining(["memory", "memory-proposed", "receipt", "ui"]),
    );
  });
});

describe("V1 stays free of the Ledger", () => {
  const v1Files = walk(srcDir).filter(
    (f) =>
      /\.(tsx?|css)$/.test(f) &&
      !isTest(f) &&
      !f.startsWith(ledgerDir + path.sep),
  );

  it("no file outside (ledger) imports Ledger styles or (ledger) modules", () => {
    const offenders = v1Files.filter((file) => {
      const source = readFileSync(file, "utf8");
      const specs = file.endsWith(".css")
        ? [...source.matchAll(/@import\s+["']([^"']+)["']/g)].map((m) => m[1])
        : importSpecifiers(source);
      return specs.some(
        (spec) =>
          spec.startsWith("@memaxlabs/ledger") || spec.includes("(ledger)"),
      );
    });
    expect(offenders.map(rel)).toEqual([]);
  });

  it("global-not-found pulls in no preloaded V1 fonts", () => {
    // Next preloads every next/font face global-not-found imports on
    // every page, Ledger pages included.
    const notFound = readFileSync(
      path.join(appDir, "global-not-found.tsx"),
      "utf8",
    );
    const document = readFileSync(
      path.join(appDir, "(v1)/v1-document.tsx"),
      "utf8",
    );
    expect(importSpecifiers(notFound)).not.toContain("./(v1)/v1-fonts");
    expect(
      importSpecifiers(document).filter((s) => s.startsWith("next/font")),
    ).toEqual([]);
    expect(
      readFileSync(path.join(appDir, "(v1)/not-found-fonts.ts"), "utf8"),
    ).toMatch(/preload: false[\s\S]*preload: false/);
  });

  it("global-error, shared by both root layouts, imports no stylesheet", () => {
    // Next preloads global-error's CSS on every page of both trees.
    const source = readFileSync(path.join(appDir, "global-error.tsx"), "utf8");
    expect(importSpecifiers(source).filter((s) => s.endsWith(".css"))).toEqual(
      [],
    );
  });
});
