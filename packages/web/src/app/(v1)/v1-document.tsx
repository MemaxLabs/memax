import type { Metadata, Viewport } from "next";
import "./globals.css";
import { Inter, JetBrains_Mono } from "next/font/google";
import { cn } from "@memaxlabs/ui/utils";
import { Providers } from "./providers";

// The V1 document: <html>/<body>, globals.css, fonts and the V1
// providers. Shared by the (v1) root layout and by
// app/global-not-found.tsx, which renders unmatched URLs outside every
// root layout and so has to bring its own document. Frozen with the
// rest of V1; deleted at cutover.

// Inter (sans) + JetBrains Mono (mono). Loaded via next/font/google so they
// land in the initial HTML without FOUT. The :root fallback chain in
// globals.css kicks in only if these fail to load. See kitchen §12 Typography.
const inter = Inter({ subsets: ["latin"], variable: "--font-sans" });
const jetbrainsMono = JetBrains_Mono({
  subsets: ["latin"],
  variable: "--font-mono",
});

export const v1Viewport: Viewport = {
  width: "device-width",
  initialScale: 1,
  maximumScale: 1,
  viewportFit: "cover",
  interactiveWidget: "resizes-content",
  themeColor: "#FAFAFA",
};

export const v1Metadata: Metadata = {
  title: "memax — your memory, every AI",
  description:
    "Save what you learn. Search what you know. memax makes your memory portable across every AI agent — shared, team-ready, always available.",
  manifest: "/manifest.json",
  icons: {
    icon: "/favicon.svg",
    apple: "/favicon.svg",
  },
  appleWebApp: {
    capable: true,
    statusBarStyle: "default",
    title: "Memax",
  },
};

export function V1Document({ children }: { children: React.ReactNode }) {
  return (
    <html
      lang="en"
      className={cn("font-sans", inter.variable, jetbrainsMono.variable)}
      suppressHydrationWarning
    >
      <body
        className="min-h-screen bg-background text-foreground antialiased"
        suppressHydrationWarning
      >
        <Providers>{children}</Providers>
      </body>
    </html>
  );
}
