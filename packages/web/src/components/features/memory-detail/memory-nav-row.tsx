"use client";

/**
 * MemoryNavRow — shared row for cross-memory navigation cards inside
 * `<MemoryDetailView>` (Related Memories + Topic Siblings).
 *
 * Both surfaces had byte-identical button markup:
 *   - flex item with title (truncated) + age (tabular-nums)
 *   - same hover bg, same border, same padding
 *   - same `formatAge` formatter + locale args
 * Plan 26 phase 6 dedup carryover from the earlier audit. One row
 * primitive means the two surfaces can never drift in spacing or
 * type ramp, and a future tweak (e.g. add an icon, change density)
 * lands in one file.
 *
 * 2026-09-14 (founder): each row carries a trailing ⋯ menu so a
 * related/sibling memory can be re-filed WITHOUT navigating into it
 * first — 移到主题 opens the same DestinationPicker the metadata
 * pill uses, as a sub-panel behind the universal MenuSubPanelHeader
 * (‹ name row; Esc steps back, not out). The menu needs the row's
 * hub/topic identity, so callers pass `hubId`/`topicId`; omit them
 * and the row renders navigation-only (old contract intact).
 */

import { useState } from "react";
import { useRouter, usePathname } from "next/navigation";
import { FolderInput, MoreHorizontal } from "lucide-react";
import {
  MenuItem,
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@memaxlabs/ui";
import { useLocale, useInterpolate } from "@/i18n";
import { formatAge } from "@/lib/format-age";
import { buildMemoryDetailPath, getHubSlugForPath } from "@/lib/route-helpers";
import { useMemoryMove } from "@/hooks/use-memory-move";
import { DestinationPicker } from "@/components/features/destination-picker";
import {
  MenuSubPanelHeader,
  subPanelEscapeHandler,
} from "@/components/features/menu-sub-panel";

interface MemoryNavRowProps {
  memoryId: string;
  title: string;
  /** ISO date string used for the right-aligned age label. */
  ageISO: string;
  /** Enables the ⋯ quick actions (移到主题). Omit for nav-only rows. */
  hubId?: string;
  topicId?: string;
}

export function MemoryNavRow({
  memoryId,
  title,
  ageISO,
  hubId,
  topicId,
}: MemoryNavRowProps) {
  const { t } = useLocale();
  const interpolate = useInterpolate();
  const router = useRouter();
  const pathname = usePathname();
  const currentHubSlug = getHubSlugForPath(pathname);
  const mover = useMemoryMove();
  const [menuOpen, setMenuOpen] = useState(false);
  const [menuView, setMenuView] = useState<"menu" | "move">("menu");
  const openMenu = (open: boolean) => {
    setMenuOpen(open);
    if (open) setMenuView("menu");
  };
  // On the /full escape route, stay in full-page mode: pushing the
  // bare memory URL from here would be caught by the @panel intercept
  // and open a PANEL on top of the full page (codex review). Suffixing
  // /full keeps sibling navigation full-page-to-full-page.
  const fullSuffix = pathname.endsWith("/full") ? "/full" : "";
  return (
    <div className="touch-no-hover group flex w-full items-center border-t border-border/30 transition-colors hover:bg-surface-1 first:border-t-0">
      <button
        onClick={() =>
          router.push(
            buildMemoryDetailPath(currentHubSlug, memoryId) + fullSuffix,
          )
        }
        className="flex min-w-0 flex-1 cursor-pointer items-center gap-2 px-4 py-2.5 text-left"
      >
        <span className="text-[13px] text-fg-2 truncate flex-1">{title}</span>
        <span className="text-[12px] text-fg-3 shrink-0 tabular-nums">
          {formatAge(ageISO, t, interpolate)}
        </span>
      </button>
      {hubId ? (
        <Popover open={menuOpen} onOpenChange={openMenu}>
          <PopoverTrigger
            onClick={(e) => e.stopPropagation()}
            aria-label={t.topics.moreActions}
            title={t.topics.moreActions}
            className="touch-no-hover mr-2 shrink-0 cursor-pointer rounded-md p-1.5 text-fg-4 transition-colors hover:text-fg-2"
          >
            <MoreHorizontal className="h-3.5 w-3.5" />
          </PopoverTrigger>
          <PopoverContent
            side="bottom"
            align="end"
            sideOffset={4}
            // Portal clicks bubble through the React tree to the row —
            // stop everything so panel padding never navigates.
            onClick={(e) => e.stopPropagation()}
          >
            {menuView === "menu" ? (
              <div className="flex min-w-[168px] flex-col gap-0.5 p-1">
                <MenuItem
                  icon={<FolderInput className="h-3.5 w-3.5" />}
                  onClick={() => setMenuView("move")}
                >
                  {t.topics.moveToTopic}
                </MenuItem>
              </div>
            ) : (
              <div
                className="flex max-h-[320px] min-w-[220px] flex-col overflow-hidden"
                onKeyDown={subPanelEscapeHandler(() => setMenuView("menu"))}
              >
                <MenuSubPanelHeader
                  label={t.topics.moveToTopic}
                  onBack={() => setMenuView("menu")}
                  backAriaLabel={t.common.backToMenu}
                />
                <DestinationPicker
                  variant="plain"
                  selectedTopicId={topicId}
                  onSelectTopic={(selectedTopicId, targetHubId, name) => {
                    setMenuOpen(false);
                    void mover.moveWithUndo(
                      [{ id: memoryId, hubId, topicId }],
                      { topicId: selectedTopicId, hubId: targetHubId },
                      mover.moveOneSuccess(name),
                    );
                  }}
                  onSelectHub={(targetHubId, name) => {
                    setMenuOpen(false);
                    void mover.moveWithUndo(
                      [{ id: memoryId, hubId, topicId }],
                      { hubId: targetHubId },
                      mover.moveOneSuccess(name),
                    );
                  }}
                  // DrillDownTree owns Escape internally and routes the
                  // un-drilled case here — this MUST step back to the
                  // menu, not kill the popover (adversarial review:
                  // wiring it to setMenuOpen(false) reproduced the
                  // exact "no way back" bug this surface exists to
                  // fix). Outside-press still dismisses via base-ui.
                  onClose={() => setMenuView("menu")}
                />
              </div>
            )}
          </PopoverContent>
        </Popover>
      ) : null}
    </div>
  );
}
