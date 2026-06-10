"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { Suspense } from "react";
import {
  MessageSquare,
  Megaphone,
  Users2,
  Palette,
  type LucideIcon,
} from "lucide-react";
import { MemaxLoader } from "@memaxlabs/ui";
import { useLocale } from "@/i18n";

type TabId = "transactional" | "campaigns" | "audiences" | "brand";

const TABS: readonly { id: TabId; href: string; icon: LucideIcon }[] = [
  {
    id: "transactional",
    href: "/admin/communications/transactional",
    icon: MessageSquare,
  },
  {
    id: "campaigns",
    href: "/admin/communications/campaigns",
    icon: Megaphone,
  },
  {
    id: "audiences",
    href: "/admin/communications/audiences",
    icon: Users2,
  },
  {
    id: "brand",
    href: "/admin/communications/brand",
    icon: Palette,
  },
] as const;

export default function CommunicationsLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  return (
    <Suspense fallback={<MemaxLoader />}>
      <CommunicationsShell>{children}</CommunicationsShell>
    </Suspense>
  );
}

function CommunicationsShell({ children }: { children: React.ReactNode }) {
  const pathname = usePathname();
  const { t } = useLocale();
  const comms = t.admin.communications;

  return (
    <div className="min-h-screen bg-background">
      <header className="border-b border-border/50 bg-background/95 backdrop-blur supports-[backdrop-filter]:bg-background/80">
        <div className="mx-auto max-w-6xl px-8 pt-8 pb-4">
          <h1 className="text-[22px] font-semibold tracking-tight text-fg-1">
            {comms.title}
          </h1>
          <p className="mt-1 max-w-2xl text-[14px] leading-relaxed text-fg-2">
            {comms.description}
          </p>
        </div>

        <nav
          aria-label={comms.tabsAriaLabel}
          className="mx-auto flex max-w-6xl items-end gap-1 overflow-x-auto px-6"
        >
          {TABS.map((tab) => {
            const Icon = tab.icon;
            const isActive = pathname.startsWith(tab.href);
            const label = comms.tabs[tab.id];
            return (
              <Link
                key={tab.id}
                href={tab.href}
                aria-current={isActive ? "page" : undefined}
                className={`group relative flex items-center gap-2 rounded-t-lg px-4 py-3 text-[13.5px] font-medium transition-colors ${
                  isActive
                    ? "text-fg-1"
                    : "text-fg-3 hover:text-fg-1 hover:bg-surface-1/60"
                }`}
              >
                <Icon className="h-4 w-4 shrink-0" />
                <span>{label}</span>
                {isActive ? (
                  <span className="absolute inset-x-2 -bottom-px h-0.5 rounded-full bg-fg-1" />
                ) : null}
              </Link>
            );
          })}
        </nav>
      </header>

      <div className="mx-auto max-w-6xl px-8 py-8">{children}</div>
    </div>
  );
}
