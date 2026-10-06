import type { Metadata, Viewport } from "next";
import "./globals.css";
import { cn } from "@memaxlabs/ui/utils";
import { Providers } from "./providers";

// The V1 document: <html>/<body>, globals.css, metadata and the V1
// providers. Shared by the (v1) root layout and by
// app/global-not-found.tsx, which renders unmatched URLs outside every
// root layout and so has to bring its own document. The fonts are passed
// in (v1-fonts.ts for the layout, not-found-fonts.ts for the 404) so
// this module imports no next/font. Frozen with the rest of V1; deleted
// at cutover.

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

export function V1Document({
  fontVariables,
  children,
}: {
  /** next/font variable classes for --font-sans and --font-mono. */
  fontVariables: string;
  children: React.ReactNode;
}) {
  return (
    <html
      lang="en"
      className={cn("font-sans", fontVariables)}
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
