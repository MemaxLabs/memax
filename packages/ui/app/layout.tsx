import type { Metadata } from "next";
import "./globals.css";
import { Geist_Mono } from "next/font/google";
import { cn } from "@/utils";

// Native system stack for sans (declared in globals.css :root as --font-sans).
// Geist Mono remains next/font-loaded because cross-OS mono drift (Menlo vs
// Consolas vs DejaVu) is too wide to leave to the system. See kitchen §12.
const geistMono = Geist_Mono({ subsets: ["latin"], variable: "--font-mono" });

export const metadata: Metadata = {
  title: "memax design kitchen",
  description:
    "Design system workbench — north star reference for all UI patterns",
};

export default function RootLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  return (
    <html
      lang="en"
      className={cn("font-sans", geistMono.variable)}
      suppressHydrationWarning
    >
      <body className="antialiased">{children}</body>
    </html>
  );
}
