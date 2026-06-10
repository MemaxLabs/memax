"use client";

import {
  useCallback,
  useEffect,
  useRef,
  useState,
  type KeyboardEvent,
  type ReactNode,
} from "react";
import type { LucideIcon } from "lucide-react";
import { MoreHorizontal } from "lucide-react";
import {
  MenuItem,
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@memaxlabs/ui";

/**
 * ActionMenu — stateful overflow menu for the single-memory surfaces
 * (detail page `⋯`, memory modal `⋯`) and any future consumer that
 * needs a popover that can SWAP CONTENT without closing.
 *
 * Two item kinds:
 *   - "action" (default): close-on-select → microtask yield → fire
 *     onSelect. Matches the pre-stateful contract for non-destructive
 *     items like Copy / Rename / Download.
 *   - "destructive": select → popover stays open → body swaps to the
 *     confirm sub-panel with destructive-LEFT + safe-RIGHT layout.
 *     onConfirm returns Promise<boolean>; true closes the popover,
 *     false keeps the sub-panel mounted so the user can retry.
 *     Esc returns to the menu (does NOT close the popover) so Esc
 *     ownership inside destructive confirmation stays with the
 *     cancel intent, not the dismiss intent.
 *
 * Keyboard model:
 *   - Menu mode: ↑/↓ navigate activatable items, Home/End jump,
 *     Enter/Space activate, Esc close popover.
 *   - Sub-panel mode: Tab/Shift+Tab cycle confirm/cancel (native
 *     focus flow), Enter on the focused button fires, Esc returns
 *     to menu. Focus defaults to the SAFE (cancel) button on open —
 *     a11y cursor-safety per WAI-ARIA alert-dialog patterns.
 *
 * Visuals: PopoverContent is always glass (.glass-dropdown) — same
 * material family as the command bar and Topic Explorer.
 *
 * Kept local to packages/web for now. Graduate to @memaxlabs/ui only
 * when a third consumer emerges AND the destructive semantics feel
 * stable across them.
 */

interface ActionMenuActionItem {
  id: string;
  label: string;
  icon?: LucideIcon;
  /** Optional keyboard shortcut hint (e.g. "⌘C"). Display only. */
  kbd?: string;
  /** Called when the user activates the item. Menu closes automatically. */
  onSelect: () => void | Promise<void>;
  /** Disable the item — still renders but not focusable or clickable. */
  disabled?: boolean;
  destructive?: false;
}

interface ActionMenuDestructiveItem {
  id: string;
  label: string;
  icon?: LucideIcon;
  kbd?: string;
  disabled?: boolean;
  destructive: true;
  /**
   * Called when the user confirms via the sub-panel.
   * Resolve `true` to close the popover on success; `false` to keep
   * the sub-panel mounted (failed mutation, retry path). Errors
   * bubble as unhandled promise rejections to the console, same as
   * the non-destructive path.
   */
  onConfirm: () => Promise<boolean>;
  /** Prompt copy above the buttons, e.g. "Forget this memory?" */
  confirmPrompt: string;
  /** Destructive button label, e.g. "Forget" */
  confirmLabel: string;
  /** Safe button label, e.g. "Keep" */
  cancelLabel: string;
  /** Pending label while `onConfirm` is in flight, e.g. "Forgetting…" */
  pendingLabel?: string;
}

export type ActionMenuItem = ActionMenuActionItem | ActionMenuDestructiveItem;

export interface ActionMenuDivider {
  divider: true;
  id: string;
}

export type ActionMenuEntry = ActionMenuItem | ActionMenuDivider;

function isDivider(entry: ActionMenuEntry): entry is ActionMenuDivider {
  return "divider" in entry && entry.divider === true;
}

function isDestructive(
  entry: ActionMenuItem,
): entry is ActionMenuDestructiveItem {
  return "destructive" in entry && entry.destructive === true;
}

type SubPanel = {
  item: ActionMenuDestructiveItem;
  pending: boolean;
};

export function ActionMenu({
  items,
  triggerAriaLabel,
  triggerClassName,
  align = "end",
  side = "bottom",
}: {
  items: ActionMenuEntry[];
  triggerAriaLabel: string;
  triggerClassName?: string;
  align?: "start" | "center" | "end";
  side?: "top" | "bottom" | "left" | "right";
}) {
  const [open, setOpen] = useState(false);
  const [focusIndex, setFocusIndex] = useState(0);
  const [subPanel, setSubPanel] = useState<SubPanel | null>(null);
  const itemRefs = useRef<Array<HTMLButtonElement | null>>([]);
  const cancelButtonRef = useRef<HTMLButtonElement | null>(null);

  const activatableIndices = items
    .map((item, i) => (!isDivider(item) && !item.disabled ? i : null))
    .filter((i): i is number => i !== null);

  // Reset focus to the first activatable item whenever the menu opens
  // OR whenever the sub-panel closes and we return to the item list.
  useEffect(() => {
    if (!open || subPanel) return;
    const first = activatableIndices[0] ?? 0;
    setFocusIndex(first);
    requestAnimationFrame(() => {
      itemRefs.current[first]?.focus();
    });
  }, [open, subPanel]); // eslint-disable-line react-hooks/exhaustive-deps

  // Sub-panel opened: default focus to the SAFE button (cancel) per
  // WAI-ARIA alert-dialog guidance. Destructive sits LEFT for cursor
  // safety (the user's cursor arrived there by clicking the menu
  // item, and we don't want to drop them on the destructive press).
  useEffect(() => {
    if (!subPanel) return;
    const raf = requestAnimationFrame(() => {
      cancelButtonRef.current?.focus();
    });
    return () => cancelAnimationFrame(raf);
  }, [subPanel]);

  // Reset sub-panel state when the menu closes externally (click-outside,
  // Esc at menu level, trigger toggle).
  useEffect(() => {
    if (!open) setSubPanel(null);
  }, [open]);

  const moveFocus = useCallback(
    (delta: 1 | -1 | "home" | "end") => {
      if (activatableIndices.length === 0) return;
      const current = activatableIndices.indexOf(focusIndex);
      let next: number;
      if (delta === "home") next = 0;
      else if (delta === "end") next = activatableIndices.length - 1;
      else
        next =
          (current + delta + activatableIndices.length) %
          activatableIndices.length;
      const targetIndex = activatableIndices[next];
      setFocusIndex(targetIndex);
      itemRefs.current[targetIndex]?.focus();
    },
    [activatableIndices, focusIndex],
  );

  const handleMenuKey = useCallback(
    (event: KeyboardEvent<HTMLDivElement>) => {
      switch (event.key) {
        case "ArrowDown":
          event.preventDefault();
          moveFocus(1);
          break;
        case "ArrowUp":
          event.preventDefault();
          moveFocus(-1);
          break;
        case "Home":
          event.preventDefault();
          moveFocus("home");
          break;
        case "End":
          event.preventDefault();
          moveFocus("end");
          break;
      }
    },
    [moveFocus],
  );

  const handleSubPanelKey = useCallback(
    (event: KeyboardEvent<HTMLDivElement>) => {
      // Esc returns to the menu; do not let it bubble to PopoverContent
      // (which would close the popover entirely). Tab/Shift+Tab flow
      // is handled by the browser's native focus traversal between the
      // two buttons — no JS needed.
      if (event.key === "Escape") {
        event.preventDefault();
        event.stopPropagation();
        if (subPanel && !subPanel.pending) {
          setSubPanel(null);
        }
      }
    },
    [subPanel],
  );

  const handleItemSelect = useCallback(async (item: ActionMenuItem) => {
    // Destructive items swap to the sub-panel and keep the popover open.
    if (isDestructive(item)) {
      setSubPanel({ item, pending: false });
      return;
    }
    // Action items: close, yield one microtask, fire handler.
    setOpen(false);
    await Promise.resolve();
    try {
      await item.onSelect();
    } catch (error) {
      console.error(
        `[ActionMenu] item "${item.id}" (${item.label}) threw:`,
        error,
      );
    }
  }, []);

  const handleConfirm = useCallback(async () => {
    if (!subPanel || subPanel.pending) return;
    const { item } = subPanel;
    setSubPanel({ item, pending: true });
    let shouldClose = false;
    try {
      shouldClose = await item.onConfirm();
    } catch (error) {
      console.error(
        `[ActionMenu] destructive "${item.id}" (${item.label}) threw:`,
        error,
      );
      shouldClose = false;
    }
    if (shouldClose) {
      setOpen(false);
      setSubPanel(null);
    } else {
      setSubPanel({ item, pending: false });
    }
  }, [subPanel]);

  const handleCancel = useCallback(() => {
    if (!subPanel || subPanel.pending) return;
    setSubPanel(null);
  }, [subPanel]);

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger
        aria-label={triggerAriaLabel}
        className={
          triggerClassName ??
          "text-fg-4 hover:text-fg-2 transition-colors cursor-pointer outline-none focus-visible:text-fg-2"
        }
      >
        <MoreHorizontal className="h-4 w-4" />
      </PopoverTrigger>

      <PopoverContent
        side={side}
        align={align}
        sideOffset={6}
        className="min-w-[220px] overflow-hidden p-1"
      >
        {subPanel ? (
          <div
            role="alertdialog"
            aria-label={subPanel.item.confirmPrompt}
            onKeyDown={handleSubPanelKey}
            className="flex flex-col gap-2 px-2 py-2"
          >
            <p className="text-[13px] text-fg-2">
              {subPanel.item.confirmPrompt}
            </p>
            <div className="flex items-center gap-2">
              {/* Destructive LEFT, safe RIGHT — cursor-safety. Default
                  focus is on the safe (cancel) button via the effect
                  above, not here via tabIndex. */}
              <button
                type="button"
                onClick={handleConfirm}
                disabled={subPanel.pending}
                className="text-[13px] min-h-9 px-3 rounded-chrome cursor-pointer transition-colors disabled:cursor-wait"
                style={{
                  background: "oklch(from var(--destructive) l c h / 0.12)",
                  color: "oklch(from var(--destructive) l c h / 0.85)",
                  opacity: subPanel.pending ? 0.7 : 1,
                }}
              >
                {subPanel.pending
                  ? (subPanel.item.pendingLabel ?? subPanel.item.confirmLabel)
                  : subPanel.item.confirmLabel}
              </button>
              <button
                ref={cancelButtonRef}
                type="button"
                onClick={handleCancel}
                disabled={subPanel.pending}
                className="text-[13px] min-h-9 px-3 rounded-chrome cursor-pointer transition-colors text-fg-2 hover:text-fg-1 disabled:cursor-default disabled:text-fg-4"
              >
                {subPanel.item.cancelLabel}
              </button>
            </div>
          </div>
        ) : (
          <div
            role="menu"
            aria-label={triggerAriaLabel}
            onKeyDown={handleMenuKey}
          >
            {items.map((entry, i) => {
              if (isDivider(entry)) {
                return (
                  <div
                    key={entry.id}
                    role="separator"
                    className="my-1 h-px bg-border/40"
                  />
                );
              }
              const Icon = entry.icon;
              return (
                <MenuItem
                  key={entry.id}
                  ref={(el) => {
                    itemRefs.current[i] = el;
                  }}
                  role="menuitem"
                  tabIndex={focusIndex === i ? 0 : -1}
                  disabled={entry.disabled}
                  variant={isDestructive(entry) ? "destructive" : undefined}
                  onClick={() => handleItemSelect(entry)}
                  icon={
                    Icon ? (
                      <Icon className="h-3.5 w-3.5 text-fg-3" aria-hidden />
                    ) : undefined
                  }
                  keybind={entry.kbd}
                >
                  {entry.label}
                </MenuItem>
              );
            })}
          </div>
        )}
      </PopoverContent>
    </Popover>
  );
}

/**
 * Helper — build an entry list with dividers. Each `ActionMenuItem` or
 * the string `"divider"` produces the right discriminated union entry.
 * Slight sugar so callers can author menus declaratively without
 * thinking about divider ids.
 */
export function actionMenuEntries(
  ...entries: Array<ActionMenuItem | "divider">
): ActionMenuEntry[] {
  return entries.map((entry, i) =>
    entry === "divider"
      ? ({
          divider: true as const,
          id: `divider-${i}`,
        } satisfies ActionMenuDivider)
      : entry,
  );
}

export type { ReactNode };
