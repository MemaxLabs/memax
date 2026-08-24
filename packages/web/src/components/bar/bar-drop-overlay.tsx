"use client";

/**
 * BarDropOverlay — full-viewport visual cue while a file drag is over
 * the window (founder report 2026-08-10: "拖拽文件没有 visual clue").
 *
 * The drag/drop *mechanics* already live in BarProvider (window-level
 * dragenter/drop listeners → `isDragging` → `addStagedFiles`); this
 * component is the missing consumer that makes the state visible:
 * a soft scrim, a dashed inset frame (the universal drop affordance —
 * same dashed vocabulary as the board ghost card), and a centered
 * glass pill naming the outcome.
 *
 * pointer-events-none throughout: the overlay must never intercept
 * the drop — the window listeners own the gesture. Purely visual.
 */

import { AnimatePresence, motion } from "framer-motion";
import { FileUp } from "lucide-react";
import { useBar } from "@/contexts/bar-context";
import { useLocale } from "@/i18n";

export function BarDropOverlay() {
  const { isDragging } = useBar();
  const { t } = useLocale();
  return (
    <AnimatePresence>
      {isDragging && (
        <motion.div
          className="pointer-events-none fixed inset-0 z-bar-notif"
          initial={{ opacity: 0 }}
          animate={{ opacity: 1 }}
          exit={{ opacity: 0 }}
          transition={{ duration: 0.15 }}
          aria-hidden
        >
          <div className="absolute inset-0 bg-foreground/6" />
          <div
            className="rounded-surface absolute inset-3 border-2 border-dashed"
            style={{
              borderColor: "oklch(from var(--signature) l c h / 0.45)",
            }}
          />
          <div className="absolute inset-0 flex items-center justify-center">
            <div className="glass-dropdown rounded-surface flex items-center gap-2.5 px-5 py-3.5">
              <FileUp
                className="h-5 w-5"
                strokeWidth={2}
                style={{ color: "var(--signature)" }}
              />
              <span className="text-[14px] font-semibold text-fg-1">
                {t.bar.dropHint}
              </span>
            </div>
          </div>
        </motion.div>
      )}
    </AnimatePresence>
  );
}
