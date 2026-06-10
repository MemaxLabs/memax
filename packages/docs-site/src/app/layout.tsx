import type { Metadata } from "next";
import type { ReactNode } from "react";
import { GeistSans } from "geist/font/sans";
import { GeistMono } from "geist/font/mono";
import { RootProvider } from "fumadocs-ui/provider";
import { DocsLayout } from "fumadocs-ui/layouts/docs";
import { source } from "@/lib/source";
import { APP_URL, DOCS_URL } from "@/lib/urls";
import { MemaxLogo } from "@/components/memax-logo";
import "./globals.css";

export const metadata: Metadata = {
  title: {
    default: "Memax Docs",
    template: "%s | Memax Docs",
  },
  description:
    "Documentation for Memax — the universal context and memory hub for AI coding agents. Push knowledge from anywhere, recall it from any agent.",
  metadataBase: new URL(DOCS_URL),
};

export default function RootLayout({ children }: { children: ReactNode }) {
  return (
    <html
      lang="en"
      className={`${GeistSans.variable} ${GeistMono.variable}`}
      suppressHydrationWarning
    >
      <body className="flex min-h-screen flex-col">
        <RootProvider>
          <DocsLayout
            tree={source.pageTree}
            nav={{
              title: (
                <span className="flex items-center gap-2 font-semibold tracking-tight">
                  <MemaxLogo size={20} className="text-fd-primary" />
                  <span>
                    <span className="text-fd-foreground">memax</span>
                    <span className="text-fd-muted-foreground ml-1 text-xs font-normal">
                      docs
                    </span>
                  </span>
                </span>
              ),
              url: "/",
            }}
            sidebar={{
              defaultOpenLevel: 1,
            }}
            githubUrl="https://github.com/memaxlabs/memax"
            links={[
              {
                text: "App",
                url: APP_URL,
                external: true,
              },
            ]}
          >
            {children}
          </DocsLayout>
        </RootProvider>
      </body>
    </html>
  );
}
