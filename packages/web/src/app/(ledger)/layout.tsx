import type { Metadata, Viewport } from "next";
import "@memaxlabs/ledger-tokens";
import "./ledger.css";
import tokens from "@memaxlabs/ledger-tokens/tokens.json";
import { en } from "@/i18n/locales/en";
import { fontVariables } from "./_lib/fonts";
import { themeInitScript } from "./_lib/theme";
import { LedgerProviders } from "./providers";

// Root layout of the V2 Ledger UI (plan E1, §6.1). A second root
// layout next to (v1), so neither tree loads the other's CSS: only
// @memaxlabs/ledger-tokens, ledger.css and the next/font faces load
// here. No Tailwind, no globals.css, no next-themes
// (isolation.test.ts enforces it).
//
// The layout reads no cookies or headers, so (ledger) pages stay
// eligible for static rendering (§6.9 wants static public pages).
// - Theme: data-theme comes from the memax_theme cookie or the system
//   preference, applied by an inline script in <head> before first
//   paint (see _lib/theme.ts). suppressHydrationWarning covers that
//   attribute, which the server can't know.
// - Locale: lang starts as "en", the language LocaleProvider renders on
//   the server, and follows the active locale on the client
//   (LocaleDocumentSync).

const paper = tokens.color.tokens.find((token) => token.name === "paper");

export const viewport: Viewport = {
  width: "device-width",
  initialScale: 1,
  viewportFit: "cover",
  colorScheme: "light dark",
  themeColor: paper
    ? [
        { media: "(prefers-color-scheme: light)", color: paper.value.light },
        { media: "(prefers-color-scheme: dark)", color: paper.value.dark },
      ]
    : undefined,
};

export const metadata: Metadata = {
  title: { default: "Memax", template: "%s · Memax" },
  description: en.ledger.meta.description,
  icons: { icon: "/favicon.svg", apple: "/favicon.svg" },
};

export default function LedgerRootLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  return (
    <html lang="en" className={fontVariables} suppressHydrationWarning>
      <head>
        <script dangerouslySetInnerHTML={{ __html: themeInitScript }} />
      </head>
      <body>
        <LedgerProviders>{children}</LedgerProviders>
      </body>
    </html>
  );
}
