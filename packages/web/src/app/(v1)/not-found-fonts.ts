import { Inter, JetBrains_Mono } from "next/font/google";

// The V1 faces for app/global-not-found.tsx, without preload. Next puts
// global-not-found in every route's tree and preloads every font it
// imports on every page, so the preloaded instances in v1-fonts.ts would
// make each Ledger page fetch Inter and JetBrains Mono. Same faces and
// variables, so the 404 looks the same; they just load on first use.
const inter = Inter({
  subsets: ["latin"],
  variable: "--font-sans",
  preload: false,
});
const jetbrainsMono = JetBrains_Mono({
  subsets: ["latin"],
  variable: "--font-mono",
  preload: false,
});

export const notFoundFontVariables = `${inter.variable} ${jetbrainsMono.variable}`;
