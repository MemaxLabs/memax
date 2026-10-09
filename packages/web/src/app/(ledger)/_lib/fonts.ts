import localFont from "next/font/local";

// The four Ledger typefaces, self-hosted from @memaxlabs/ledger-tokens
// with next/font/local (design review §3.8).
//
// - next/font emits each file under /_next/static/media with a hashed
//   name and writes the @font-face rules. ledger.css points the tokens'
//   --font-serif / --font-sans / --font-mono / --font-brand at the
//   --ledger-face-* variables below, so tokens.css's own @font-face
//   rules are never referenced and the browser never fetches them.
// - Family names. Turbopack (the dev and build bundler in Next 16)
//   names a next/font/local family after the variable it's assigned
//   to, unhashed. Font family names match case-insensitively, so the
//   consts are named ledgerSerif etc.: a const called `newsreader`
//   would merge with tokens.css's "Newsreader" faces.
// - Preload. Only Newsreader roman and Schibsted Grotesk are preloaded.
//   next/font preloads per call, so Newsreader italic is a second call,
//   folded into the same family by naming its @font-face "ledgerSerif"
//   (`declarations`). Italic text then uses the real italic face, not a
//   synthesised oblique. That relies on Turbopack's unhashed names (a
//   webpack build would hash the two calls apart), so the Playwright
//   specimen test checks the italic face really loads. Everything else
//   loads on first use, so Gloock is only fetched where the wordmark is.
// - Fallbacks. Size-adjusted to each font's metrics (Times New Roman for
//   the serifs, Arial for the sans) so the swap doesn't shift layout.
//   IBM Plex Mono falls back to the system monospace faces instead: they
//   already share its 0.6em advance, and an adjusted Arial would show
//   receipts in a proportional face while the file loads. The lists end
//   with tokens.css's CJK faces so Chinese keeps the specified serif and
//   sans.
//
// next/font needs literal arguments, so each call spells out its values.

export const ledgerSerif = localFont({
  src: "../../../../node_modules/@memaxlabs/ledger-tokens/fonts/Newsreader-Variable.woff2",
  weight: "200 800",
  style: "normal",
  display: "swap",
  preload: true,
  adjustFontFallback: "Times New Roman",
  fallback: ["Songti SC", "Noto Serif SC", "Georgia", "serif"],
  variable: "--ledger-face-serif",
});

export const ledgerSerifItalic = localFont({
  src: "../../../../node_modules/@memaxlabs/ledger-tokens/fonts/Newsreader-Italic-Variable.woff2",
  weight: "200 800",
  style: "italic",
  display: "swap",
  preload: false,
  adjustFontFallback: false,
  declarations: [{ prop: "font-family", value: "ledgerSerif" }],
  // Unused by CSS; applying the class keeps this call's @font-face in
  // the bundle.
  variable: "--ledger-face-serif-italic",
});

export const ledgerSans = localFont({
  src: "../../../../node_modules/@memaxlabs/ledger-tokens/fonts/SchibstedGrotesk-Variable.woff2",
  weight: "400 900",
  style: "normal",
  display: "swap",
  preload: true,
  adjustFontFallback: "Arial",
  fallback: ["PingFang SC", "Noto Sans SC", "system-ui", "sans-serif"],
  variable: "--ledger-face-sans",
});

export const ledgerMono = localFont({
  src: [
    {
      path: "../../../../node_modules/@memaxlabs/ledger-tokens/fonts/IBMPlexMono-Regular.woff2",
      weight: "400",
      style: "normal",
    },
    {
      path: "../../../../node_modules/@memaxlabs/ledger-tokens/fonts/IBMPlexMono-Medium.woff2",
      weight: "500",
      style: "normal",
    },
  ],
  display: "swap",
  preload: false,
  adjustFontFallback: false,
  fallback: ["ui-monospace", "SF Mono", "Menlo", "monospace"],
  variable: "--ledger-face-mono",
});

export const ledgerBrand = localFont({
  src: "../../../../node_modules/@memaxlabs/ledger-tokens/fonts/Gloock-Regular.woff2",
  weight: "400",
  style: "normal",
  display: "swap",
  preload: false,
  adjustFontFallback: "Times New Roman",
  fallback: ["Songti SC", "Georgia", "serif"],
  variable: "--ledger-face-brand",
});

/** Class names that define the --ledger-face-* variables; set on <html>. */
export const fontVariables = [
  ledgerSerif.variable,
  ledgerSerifItalic.variable,
  ledgerSans.variable,
  ledgerMono.variable,
  ledgerBrand.variable,
].join(" ");
