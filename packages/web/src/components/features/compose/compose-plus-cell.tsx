"use client";

/**
 * ComposePlusCell — card-shape `+` button that occupies the first cell
 * of the v2 memories card grid (per plan 20 RFC §3.1 + kitchen 43-shell-v2
 * `PlusCard`).
 *
 * Distinct from `<ComposePlusCard>`:
 *   - `<ComposePlusCard>` is the wide horizontal CTA mounted above the
 *     RecentSection (v1 affordance — v1 has no card grid to host a
 *     leading cell).
 *   - `<ComposePlusCell>` is the card-shape leading cell of the v2
 *     grid. Centered icon + title + hint, dashed border instead of
 *     glass-card (the empty-cell aesthetic is dashed rather than
 *     filled — communicates "fill me in" affordance).
 *
 * Both call `useCompose().openModal()`. v1 users see the wide CTA;
 * v2 users see the card cell. ⌘⇧↵ global hotkey is the third entry
 * point and works everywhere.
 *
 * Resume-draft state: when the modal closes without saving, the
 * draft is preserved in ComposeProvider. The cell switches its
 * affordance from "+ New memory" to "✎ Resume draft" so the
 * user knows their work is still here. Border solidifies (vs
 * dashed) to visually communicate "this isn't empty anymore".
 */

import { Pencil, Plus } from "lucide-react";
import { useCompose } from "@/contexts/compose-context";
import { useLocale } from "@/i18n";

export function ComposePlusCell() {
  const { openModal, draft } = useCompose();
  const { t } = useLocale();
  // Draft-preserved state: user closed the modal without saving and
  // then sees the cell again. Switch to a resume affordance so it
  // acknowledges the in-progress draft instead of implying a fresh
  // start. Border solidifies to signal "not empty".
  const hasDraft = Boolean(draft.title.trim() || draft.content.trim());
  return (
    <button
      type="button"
      onClick={openModal}
      aria-label={hasDraft ? t.composeCell.resumeAria : t.composeCell.aria}
      // Dimensions + radius match `<MemoryCardLayout>` (plan 26 phase 2)
      // so the new-memory CTA aligns exactly with neighboring memory
      // cards in the same grid: same min-height (168px), same
      // `rounded-card` (20px), same p-4. Only the affordance shape
      // differs — dashed border + centered icon vs the memory card's
      // metadata + summary layout. The ⌘⇧↵ kbd is desktop-only — no
      // point teaching a keyboard shortcut on a touch surface.
      className="group flex h-full min-h-[168px] flex-col items-center justify-center rounded-card bg-transparent p-4 transition-colors duration-200 hover:bg-surface-1 cursor-pointer"
      style={{
        // var(--glass-border) (vs full-strength var(--border)) keeps the
        // dashed border subtle — same vocabulary as <ConnectAgentTile>
        // and the kitchen 43 PlusCard canonical. Resume state still
        // solidifies via 1px solid + the stronger var(--border) so
        // "draft preserved" reads as not-empty.
        border: hasDraft
          ? "1px solid var(--border)"
          : "1px dashed var(--glass-border)",
      }}
    >
      <span className="mb-2 flex h-9 w-9 items-center justify-center rounded-full bg-surface-2 transition-colors group-hover:bg-surface-3">
        {hasDraft ? (
          <Pencil
            className="h-3.5 w-3.5 text-fg-3 group-hover:text-foreground transition-colors"
            strokeWidth={2.2}
          />
        ) : (
          <Plus
            className="h-4 w-4 text-fg-3 group-hover:text-foreground transition-colors"
            strokeWidth={2.2}
          />
        )}
      </span>
      <span className="mb-0.5 text-[13px] font-medium text-foreground">
        {hasDraft ? t.composeCell.resumeTitle : t.composeCell.title}
      </span>
      <span className="max-w-[140px] text-center text-[11px] leading-snug text-fg-4">
        {hasDraft ? t.composeCell.resumeHint : t.composeCell.hint}
      </span>
      <kbd className="mt-1 hidden rounded bg-surface-2 px-1 py-px text-[10px] text-fg-4 sm:inline-block">
        ⌘⇧↵
      </kbd>
    </button>
  );
}
