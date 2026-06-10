"use client";

/**
 * `<ShellLayoutV2>` — top-level v2 shell. Dispatches between desktop
 * + mobile chrome via `useIsMobile()`.
 *
 * Why dispatch (NOT dual-mount + CSS visibility):
 * Dual-mounting both layouts means children (route components, Tiptap
 * editors, useTrackMemoryAccessed, dnd droppables, TanStack Query
 * hooks, …) ALSO mount twice — duplicate effects, duplicate save
 * paths, race-prone interactions. Codex caught this on the first
 * attempt at dual-mount. Plan 22 §4.3 originally argued the hidden
 * tree was "paint-free," but it didn't account for hook + effect
 * duplication.
 *
 * The trade-off is a brief flash on mobile devices: SSR + first
 * client render (with `useIsMobile() === false` from the SSR-safe
 * default) shows the desktop layout, then the matchMedia listener
 * inside `IsMobileProvider` flips `isMobile = true` and the mobile
 * layout takes over. This is one paint at most, only on mobile, and
 * the swap is instantaneous because chrome is light. Dnd-correctness
 * + single-effect contracts are worth more than a frame of perfect
 * first-paint.
 *
 * Aurora root CSS var: set ONCE here so both branches read the same
 * `--page-aurora-gradient` from `:root` via inline `background-image`.
 */

import { useMemo, type ReactNode } from "react";
import { useActiveHub } from "@/lib/auth";
import { useIsMobile } from "@/hooks/use-is-mobile";
import { useSettings } from "@/hooks/use-settings";
import { resolveHubHeaderMode } from "@/lib/hub-header-mode";
import { ShellLayoutDesktop } from "./shell-layout-desktop";
import { ShellLayoutMobile } from "./shell-layout-mobile";
import { useAuroraOnRoot } from "./use-aurora-on-root";
import type { ShellTabId } from "./shell-tabs";

interface ShellLayoutV2Props {
  /**
   * The currently active tab — derived by the caller from the URL via
   * `getShellTabForPath`. `null` for routes that don't map to any
   * shell tab (e.g., `/`, `/login`); both layouts render no active
   * state in that case.
   */
  tab: ShellTabId | null;
  children: ReactNode;
}

export function ShellLayoutV2({ tab, children }: ShellLayoutV2Props) {
  const { activeHub } = useActiveHub();
  const { data: settings } = useSettings();
  const isMobile = useIsMobile();
  const auroraMode = useMemo(
    () => resolveHubHeaderMode(activeHub?.hub ?? null, settings),
    [activeHub, settings],
  );
  useAuroraOnRoot(auroraMode);

  return isMobile ? (
    <ShellLayoutMobile tab={tab}>{children}</ShellLayoutMobile>
  ) : (
    <ShellLayoutDesktop tab={tab}>{children}</ShellLayoutDesktop>
  );
}
