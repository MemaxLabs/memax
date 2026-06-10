"use client";

import { AnimatePresence, motion, useReducedMotion } from "framer-motion";
import { ChevronRight } from "lucide-react";
import Link from "next/link";
import { usePathname } from "next/navigation";
import { useLocale } from "@/i18n";
import { useActiveHub, useAuth } from "@/lib/auth";
import { hubRouteSlug } from "@/lib/hub-from-slug";
import { EASE, NORMAL } from "@memaxlabs/ui/tokens/motion";
import { buildMemoriesPath, getHubSlugForPath } from "@/lib/route-helpers";

interface BreadcrumbItem {
  label: string;
  href?: string;
  onClick?: () => void;
}

export function TopicBreadcrumb({
  items,
  direction = 1,
  suppressInitialAnimation = false,
}: {
  items: BreadcrumbItem[];
  direction?: 1 | -1;
  suppressInitialAnimation?: boolean;
}) {
  const { t } = useLocale();
  const { activeHub, isTeamHub } = useActiveHub();
  const { user } = useAuth();
  const reduced = useReducedMotion();
  const pathname = usePathname();
  // Back link resolution mirrors bar-context.goMemory: pathname slug
  // wins (covers v2 hub-scoped topic routes, the only place this
  // breadcrumb mounts today), with active-hub fallback so any future
  // hub-less topic surface doesn't silently route to /memories →
  // personal default. v1 fallback for the rare case neither resolves.
  const pathSlug = getHubSlugForPath(pathname);
  const activeSlug =
    activeHub && user ? hubRouteSlug(activeHub.hub, user.id) : null;
  const memoriesHref = buildMemoriesPath(pathSlug ?? activeSlug);

  const backLabel =
    isTeamHub && activeHub ? t.topics.titleTeam : t.topics.titlePersonal;

  const all: BreadcrumbItem[] = [
    { label: backLabel, href: memoriesHref },
    ...items,
  ];

  return (
    <nav className="flex items-center gap-1.5 mb-3">
      <AnimatePresence mode="popLayout" initial={false}>
        {all.map((item, i) => (
          <motion.span
            key={`${item.href ?? item.label}-${i}`}
            layout
            initial={
              suppressInitialAnimation
                ? false
                : reduced
                  ? { opacity: 0 }
                  : { opacity: 0, x: direction * 8 }
            }
            animate={reduced ? { opacity: 1 } : { opacity: 1, x: 0 }}
            exit={reduced ? { opacity: 0 } : { opacity: 0, x: -direction * 8 }}
            transition={{ duration: NORMAL, ease: EASE }}
            className="flex items-center gap-1.5"
          >
            {i > 0 && <ChevronRight className="h-3 w-3 text-fg-4 shrink-0" />}
            {item.href && i < all.length - 1 ? (
              item.onClick ? (
                <button
                  type="button"
                  onClick={item.onClick}
                  className="text-[14px] text-fg-3 hover:text-fg-2 transition-colors cursor-pointer"
                >
                  {item.label}
                </button>
              ) : (
                <Link
                  href={item.href}
                  scroll={false}
                  className="text-[14px] text-fg-3 hover:text-fg-2 transition-colors"
                >
                  {item.label}
                </Link>
              )
            ) : (
              <span className="text-[14px] text-fg-2 font-medium">
                {item.label}
              </span>
            )}
          </motion.span>
        ))}
      </AnimatePresence>
    </nav>
  );
}
