"use client";

import { usePathname } from "next/navigation";
import { AnimatePresence, motion, useReducedMotion } from "framer-motion";
import { TopicGrid } from "@/components/features/topic/topic-grid";
import { TopicDetail } from "@/components/features/topic/topic-detail";
import { SelectionProvider } from "@/contexts/selection-context";
import { PageShell } from "@/components/layout";
import { EASE, FAST } from "@memaxlabs/ui/tokens/motion";
import { useCallback, useEffect, useRef } from "react";
import { useAuth } from "@/lib/auth";
import { usePreviousPathname } from "@/contexts/route-history-context";
import {
  isMemoriesRootPath,
  isTopicPath,
  readMemoriesScrollPosition,
  saveMemoriesScrollPosition,
  shouldRestoreMemoriesScroll,
} from "@/lib/memories-scroll";

interface WorkspaceMotionContext {
  direction: 1 | -1;
  shouldUseDrillMotion: boolean;
  reduced: boolean;
}

const workspaceVariants = {
  enter: ({
    direction,
    shouldUseDrillMotion,
    reduced,
  }: WorkspaceMotionContext) =>
    shouldUseDrillMotion
      ? reduced
        ? { opacity: 0 }
        : { opacity: 0, x: direction * 16 }
      : { opacity: 1, x: 0 },
  center: ({ reduced }: WorkspaceMotionContext) =>
    reduced ? { opacity: 1 } : { opacity: 1, x: 0 },
  exit: ({
    direction,
    shouldUseDrillMotion,
    reduced,
  }: WorkspaceMotionContext) =>
    shouldUseDrillMotion
      ? reduced
        ? { opacity: 0 }
        : { opacity: 0, x: -direction * 16 }
      : { opacity: 0 },
};

/**
 * Memories layout — renders children directly.
 *
 * TopicGrid is the primary browse surface (topics + recent).
 * Topics are the primary memory organization surface.
 * Detail pages get mobile slide-in animation.
 */
export default function MemoriesLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  const pathname = usePathname();
  const { activeHubId } = useAuth();
  const previousAppPathname = usePreviousPathname();
  const reduced = useReducedMotion();
  const prefersReducedMotion = Boolean(reduced);
  const isTopicsWorkspaceRoute =
    pathname === "/memories" || pathname.startsWith("/memories/topics/");
  const topicId = pathname.startsWith("/memories/topics/")
    ? (pathname.split("/memories/topics/")[1]?.split("/")[0] ?? null)
    : null;
  const previousPathname = useRef(pathname);
  const popNavigationRef = useRef(false);
  const workspaceDirection: 1 | -1 =
    previousPathname.current === "/memories" &&
    pathname.startsWith("/memories/topics/")
      ? 1
      : previousPathname.current.startsWith("/memories/topics/") &&
          pathname === "/memories"
        ? -1
        : 1;
  const cameFromMemoriesWorkspace = Boolean(
    previousAppPathname &&
    (isMemoriesRootPath(previousAppPathname) ||
      isTopicPath(previousAppPathname)),
  );
  const shouldUseWorkspaceDrillMotion = cameFromMemoriesWorkspace;
  const workspaceMotion: WorkspaceMotionContext = {
    direction: workspaceDirection,
    shouldUseDrillMotion: shouldUseWorkspaceDrillMotion,
    reduced: prefersReducedMotion,
  };

  const persistScrollPosition = useCallback(
    (path: string, top: number = window.scrollY) => {
      if (!activeHubId || typeof window === "undefined") return;
      saveMemoriesScrollPosition(activeHubId, path, top);
    },
    [activeHubId],
  );

  const restoreScrollPosition = useCallback((top: number) => {
    if (typeof window === "undefined") return () => {};

    let cancelled = false;
    let attempts = 0;
    const maxAttempts = 120;
    const start = performance.now();
    const maxWaitMs = 2000;

    const tick = () => {
      if (cancelled) return;
      const maxScrollableY = Math.max(
        document.documentElement.scrollHeight - window.innerHeight,
        0,
      );
      const nextTop = Math.min(top, maxScrollableY);
      window.scrollTo({ top: nextTop, behavior: "auto" });

      const settled =
        Math.abs(window.scrollY - nextTop) <= 2 || maxScrollableY >= top - 4;

      if (
        !settled &&
        attempts < maxAttempts &&
        performance.now() - start < maxWaitMs
      ) {
        attempts += 1;
        window.requestAnimationFrame(tick);
      }
    };

    window.requestAnimationFrame(tick);
    return () => {
      cancelled = true;
    };
  }, []);

  useEffect(() => {
    if (typeof window === "undefined") return;
    const handlePopState = () => {
      popNavigationRef.current = true;
    };
    window.addEventListener("popstate", handlePopState);
    return () => {
      window.removeEventListener("popstate", handlePopState);
    };
  }, []);

  useEffect(() => {
    if (!activeHubId || typeof window === "undefined") return;

    let frame = 0;
    const onScroll = () => {
      if (frame) {
        window.cancelAnimationFrame(frame);
      }
      frame = window.requestAnimationFrame(() => {
        persistScrollPosition(pathname);
      });
    };

    window.addEventListener("scroll", onScroll, { passive: true });
    return () => {
      window.removeEventListener("scroll", onScroll);
      if (frame) {
        window.cancelAnimationFrame(frame);
      }
      persistScrollPosition(pathname);
    };
  }, [activeHubId, pathname, persistScrollPosition]);

  useEffect(() => {
    if (!activeHubId || typeof window === "undefined") {
      previousPathname.current = pathname;
      return;
    }

    const previous = previousPathname.current;
    const shouldRestore = shouldRestoreMemoriesScroll({
      previousPath: previous,
      nextPath: pathname,
      isPopNavigation: popNavigationRef.current,
    });
    const nextScrollTop = shouldRestore
      ? readMemoriesScrollPosition(activeHubId, pathname)
      : null;

    popNavigationRef.current = false;
    previousPathname.current = pathname;

    if (nextScrollTop === null) return;

    return restoreScrollPosition(nextScrollTop);
  }, [activeHubId, pathname, restoreScrollPosition]);

  if (isTopicsWorkspaceRoute) {
    // ShellLayoutV2 owns content padding directly via its rail+panel
    // layout; the legacy pinned-tree offset (`TREE_PANEL_CONTENT_OFFSET`)
    // was shell-v1 only. Plan 24 phase 4b retired v1, so the offset is
    // gone — topic-detail content flows at its natural left edge under
    // v2's chrome.
    //
    // Topic drill: no `mode="wait"` so enter + exit run in parallel
    // (halves perceived duration), and FAST (150ms) instead of NORMAL
    // (200ms). Keeps kitchen 29n's ±16px translate + spring ease.
    return (
      <>
        <AnimatePresence initial={false} custom={workspaceMotion}>
          {topicId ? (
            <motion.div
              key="topics-detail"
              custom={workspaceMotion}
              variants={workspaceVariants}
              initial={shouldUseWorkspaceDrillMotion ? "enter" : false}
              animate="center"
              exit="exit"
              transition={{ duration: FAST, ease: EASE }}
            >
              <PageShell>
                <SelectionProvider>
                  <TopicDetail topicId={topicId} suppressInitialAnimation />
                </SelectionProvider>
              </PageShell>
            </motion.div>
          ) : (
            <motion.div
              key="topics-root"
              custom={workspaceMotion}
              variants={workspaceVariants}
              initial={shouldUseWorkspaceDrillMotion ? "enter" : false}
              animate="center"
              exit="exit"
              transition={{ duration: FAST, ease: EASE }}
            >
              <TopicGrid />
            </motion.div>
          )}
        </AnimatePresence>
        <div className="hidden">{children}</div>
      </>
    );
  }

  // Memory detail on mobile: no slide wrapper. The template already fades
  // content in via animate-fade-in (150ms opacity). A 350ms translateX slide
  // was over-spec per kitchen 29n (200ms ±16px) and felt laggy.
  return <>{children}</>;
}
