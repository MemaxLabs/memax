"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { Suspense } from "react";
import {
  Settings,
  Activity,
  Inbox,
  ListChecks,
  type LucideIcon,
} from "lucide-react";
import { MemaxLoader } from "@memaxlabs/ui";
import { useLocale } from "@/i18n";

type TabId = "config" | "pulse" | "ingestion" | "jobs";

const TABS: readonly { id: TabId; href: string; icon: LucideIcon }[] = [
  { id: "config", href: "/admin/infra/config", icon: Settings },
  { id: "pulse", href: "/admin/infra/pulse", icon: Activity },
  { id: "ingestion", href: "/admin/infra/ingestion", icon: Inbox },
  { id: "jobs", href: "/admin/infra/jobs", icon: ListChecks },
] as const;

export default function InfraLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  return (
    <Suspense fallback={<MemaxLoader />}>
      <InfraShell>{children}</InfraShell>
    </Suspense>
  );
}

function InfraShell({ children }: { children: React.ReactNode }) {
  const pathname = usePathname();
  const { t } = useLocale();
  const opsCopy = t.admin.infra.ops;
  const subnavCopy = t.admin.infra.subnav;

  return (
    <div className="min-h-screen bg-background">
      <header className="sticky top-0 z-10 border-b border-border/50 bg-background/95 backdrop-blur supports-[backdrop-filter]:bg-background/80">
        <div className="mx-auto max-w-6xl px-4 pb-3 pt-6 sm:px-6 sm:pt-8 md:px-8">
          <h1 className="text-[22px] font-semibold tracking-tight text-fg-1">
            {t.admin.nav.infra}
          </h1>
          <p className="mt-1 max-w-2xl text-[14px] leading-relaxed text-fg-2">
            {opsCopy.description}
          </p>
        </div>

        {/* Horizontal scroll pill strip on mobile; side-by-side tabs on desktop.
            Overflow scrolling is the mobile-first admin pattern (see
            communications/layout.tsx) — keeps sub-nav always reachable
            with thumb. */}
        <nav
          aria-label={t.admin.nav.infra}
          className="mx-auto flex max-w-6xl items-end gap-1 overflow-x-auto px-2 sm:px-4 md:px-6"
        >
          {TABS.map((tab) => {
            const Icon = tab.icon;
            // /admin/infra/jobs/[id] should still highlight Jobs —
            // startsWith covers nested detail routes.
            const isActive = pathname.startsWith(tab.href);
            const label = subnavCopy[tab.id];
            return (
              <Link
                key={tab.id}
                href={tab.href}
                aria-current={isActive ? "page" : undefined}
                className={`group relative flex shrink-0 items-center gap-2 rounded-t-lg px-3 py-3 text-[13.5px] font-medium transition-colors sm:px-4 ${
                  isActive
                    ? "text-fg-1"
                    : "text-fg-3 hover:bg-surface-1/60 hover:text-fg-1"
                }`}
              >
                <Icon className="h-4 w-4 shrink-0" aria-hidden />
                <span>{label}</span>
                {isActive ? (
                  <span className="absolute inset-x-2 -bottom-px h-0.5 rounded-full bg-fg-1" />
                ) : null}
              </Link>
            );
          })}
        </nav>
      </header>

      <div className="mx-auto max-w-6xl px-4 py-6 sm:px-6 sm:py-8 md:px-8">
        {children}
      </div>
    </div>
  );
}
