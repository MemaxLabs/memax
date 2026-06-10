"use client";

import {
  createContext,
  useContext,
  useState,
  useEffect,
  type ReactNode,
} from "react";

const MOBILE_BREAKPOINT = 768; // matches Tailwind md:

// Sentinel default — useIsMobile throws if called outside provider.
const UNSET = Symbol("IsMobileContext not provided");
const IsMobileContext = createContext<boolean | typeof UNSET>(UNSET);

/**
 * IsMobileProvider — single matchMedia listener at layout level.
 * All useIsMobile() consumers read from context (zero per-component listeners).
 * Mount once in app/(app)/layout.tsx, wrapping the entire app shell.
 */
export function IsMobileProvider({ children }: { children: ReactNode }) {
  const [isMobile, setIsMobile] = useState(false);

  useEffect(() => {
    const mq = window.matchMedia(`(max-width: ${MOBILE_BREAKPOINT - 1}px)`);
    const handler = (e: MediaQueryListEvent | MediaQueryList) =>
      setIsMobile(e.matches);
    handler(mq);
    mq.addEventListener("change", handler);
    return () => mq.removeEventListener("change", handler);
  }, []);

  return (
    <IsMobileContext.Provider value={isMobile}>
      {children}
    </IsMobileContext.Provider>
  );
}

/**
 * useIsMobile — reads from IsMobileProvider context.
 * Throws if called outside provider (catches silent misuse).
 */
export function useIsMobile(): boolean {
  const value = useContext(IsMobileContext);
  if (value === UNSET) {
    throw new Error(
      "useIsMobile must be used within IsMobileProvider. " +
        "Wrap your component tree with <IsMobileProvider>.",
    );
  }
  return value;
}

/**
 * useMobileShell — returns true when the UI should use its mobile
 * (full-screen) shell rather than its desktop (floating modal) shell.
 *
 * This is broader than useIsMobile() on purpose:
 *   - Any viewport narrower than MOBILE_BREAKPOINT → mobile shell
 *     (same as useIsMobile).
 *   - OR a coarse pointer on a short-height viewport → mobile shell.
 *     Landscape-oriented phones and small tablets can exceed 768px
 *     width while still being touch-first with cramped vertical space.
 *     Using a desktop shell there (220px sidebar + 80vh modal) eats
 *     nearly the entire screen and feels broken.
 *
 * Kept separate from useIsMobile so we don't cascade behavior changes
 * into surfaces that deliberately key off width only (inbox-control,
 * memory-row, bar chrome, etc.). Opt in where a full-screen mobile
 * shell is the right call in both portrait AND landscape-short.
 */
export function useMobileShell(): boolean {
  const isMobileByWidth = useIsMobile();
  const [shortCoarse, setShortCoarse] = useState(false);
  useEffect(() => {
    const mq = window.matchMedia("(max-height: 640px) and (pointer: coarse)");
    const handler = (e: MediaQueryListEvent | MediaQueryList) =>
      setShortCoarse(e.matches);
    handler(mq);
    mq.addEventListener("change", handler);
    return () => mq.removeEventListener("change", handler);
  }, []);
  return isMobileByWidth || shortCoarse;
}
