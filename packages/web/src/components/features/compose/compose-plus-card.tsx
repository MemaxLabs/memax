"use client";

/**
 * ComposePlusCard — visible entry-point to the global Compose modal.
 *
 * The compose modal can also be opened anywhere via `⌘⇧↵`, but most
 * users won't discover that hotkey. This card sits at the top of the
 * memories overview and gives a tap-to-open affordance.
 *
 * Plan 20 phase 2 ships the full card-preset refactor of MemoryRow
 * which wraps cards in a 2-column grid; this component is the
 * leading "+" cell of that grid. Until that grid lands the card stands
 * alone as a wide call-to-action and slots in cleanly above the
 * existing RecentSection.
 *
 * Visual: glass-card recipe + neutral surface-2 / fg-2 on the `+`
 * glyph (per the design rule that signature accent is reserved for
 * places where memax AI is acting — compose is human capture, so the
 * affordance stays neutral). Hover lifts -1px with no colored shadow.
 */

import { Pencil, Plus } from "lucide-react";
import { useCompose } from "@/contexts/compose-context";
import { useLocale } from "@/i18n";

export function ComposePlusCard() {
  const { openModal, draft } = useCompose();
  const { t } = useLocale();
  // Draft preserved across close means the user is RESUMING work.
  // Surface that state so the affordance reads "your in-progress
  // memory is still here" instead of "start fresh".
  const hasDraft = Boolean(draft.title.trim() || draft.content.trim());
  return (
    <button
      type="button"
      onClick={openModal}
      aria-label={hasDraft ? t.composeCard.resumeAria : t.composeCard.aria}
      className="glass-card group flex w-full items-center gap-3 rounded-surface px-4 py-3 text-left transition-all hover:-translate-y-[1px] cursor-pointer"
    >
      <span
        className="flex h-8 w-8 shrink-0 items-center justify-center rounded-full bg-surface-2 text-fg-2"
        aria-hidden
      >
        {hasDraft ? (
          <Pencil className="h-3.5 w-3.5" strokeWidth={2.5} />
        ) : (
          <Plus className="h-4 w-4" strokeWidth={2.5} />
        )}
      </span>
      <div className="min-w-0 flex-1">
        <div className="text-[14px] font-medium text-fg-1">
          {hasDraft ? t.composeCard.resumeTitle : t.composeCard.title}
        </div>
        <div className="mt-0.5 text-[12px] text-fg-3">
          {hasDraft ? t.composeCard.resumeSubtitle : t.composeCard.subtitle}
        </div>
      </div>
      <kbd
        className="hidden shrink-0 rounded border border-border/60 px-1.5 py-0.5 text-[11px] font-medium text-fg-3 sm:inline-block"
        aria-hidden
      >
        ⌘⇧↵
      </kbd>
    </button>
  );
}
