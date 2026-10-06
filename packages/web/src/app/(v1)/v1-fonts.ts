import { Inter, JetBrains_Mono } from "next/font/google";

// Inter (sans) + JetBrains Mono (mono) for the V1 root layout. Loaded via
// next/font/google so they land in the initial HTML without FOUT. The
// :root fallback chain in globals.css kicks in only if these fail to
// load. See kitchen §12 Typography.
const inter = Inter({ subsets: ["latin"], variable: "--font-sans" });
const jetbrainsMono = JetBrains_Mono({
  subsets: ["latin"],
  variable: "--font-mono",
});

export const v1FontVariables = `${inter.variable} ${jetbrainsMono.variable}`;
