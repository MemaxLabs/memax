"use client";

import { motion, AnimatePresence } from "framer-motion";
import { ChevronRight, X } from "lucide-react";
import { useBar } from "@/contexts/bar-context";
import type { BarNotificationType } from "@/lib/bar-types";
import { FAST, EASE } from "@memaxlabs/ui/tokens/motion";
import { DREAM_BANNER } from "@memaxlabs/ui/tokens/dream";

/* ── Style config per notification type ── */
export const NOTIFICATION_STYLES: Record<
  BarNotificationType,
  {
    accent: string;
    bg: string;
    icon: string;
  }
> = {
  dream: {
    accent: DREAM_BANNER.dream.accent,
    bg: DREAM_BANNER.dream.bg,
    icon: "✦",
  },
  dreaming: {
    accent: DREAM_BANNER.dreaming.accent,
    bg: DREAM_BANNER.dreaming.bg,
    icon: "✦",
  },
  dream_complete: {
    accent: DREAM_BANNER.dream.accent,
    bg: DREAM_BANNER.dream.bg,
    icon: "✦",
  },
  dream_clean: {
    accent: DREAM_BANNER.dream.accent,
    bg: DREAM_BANNER.dream.bg,
    icon: "✦",
  },
  dream_partial: {
    // Same dream visual family so the banner still reads as "the
    // engine ran and this is the receipt". The icon shifts from ✦
    // to a subtle ! so the user sees at a glance that something
    // wasn't clean. Separate token could land later if amber
    // treatment proves necessary.
    accent: DREAM_BANNER.dream.accent,
    bg: DREAM_BANNER.dream.bg,
    icon: "!",
  },
  update: {
    accent: "var(--signature)",
    bg: `linear-gradient(135deg, oklch(from var(--signature) l c h / 0.08), oklch(from var(--signature) l c h / 0.04))`,
    icon: "✦",
  },
  info: {
    accent: "var(--fg-1)",
    bg: "transparent",
    icon: "✦",
  },
  success: {
    accent: "var(--fg-1)",
    bg: "transparent",
    icon: "✓",
  },
  error: {
    accent: "var(--destructive)",
    bg: "oklch(from var(--destructive) l c h / 0.06)",
    icon: "✕",
  },
};

/**
 * Independent notification card — rendered as a global toast surface
 * decoupled from the bar's position (plan 26 follow-up).
 *
 * Positioned by `app-shell-client.tsx`'s notif container — bottom-
 * anchored on every surface (24px from viewport bottom on desktop,
 * above the mobile dock-bar on mobile) at z-bar-notif. No longer
 * tucked behind the bar's bottom edge. Own border-radius, own
 * background, own shadow.
 *
 * Slides up from below on enter (`motion.div`'s `y: 8`) — same
 * direction as before, but for a different reason (was: rise-from-
 * behind-bar; now: rise-into-position from below).
 */
export function BarNotificationCard() {
  const { activeNotification } = useBar();

  return (
    <AnimatePresence>
      {activeNotification && (
        <motion.div
          key="bar-notification-card"
          // Bottom-anchored on every surface (plan 26 follow-up), so the
          // notif rises from below: positive enter-y → 0. Same direction
          // the original tucked-behind-bar version used; just for a
          // different reason.
          initial={{ opacity: 0, y: 8 }}
          animate={{ opacity: 1, y: 0 }}
          exit={{ opacity: 0, y: 8 }}
          transition={{ duration: FAST, ease: EASE }}
        >
          <NotificationContent notification={activeNotification} />
        </motion.div>
      )}
    </AnimatePresence>
  );
}

function NotificationContent({
  notification,
}: {
  notification: NonNullable<ReturnType<typeof useBar>["activeNotification"]>;
}) {
  const style = NOTIFICATION_STYLES[notification.type];
  const isDreaming = notification.type === "dreaming";
  const isDreamFamily =
    notification.type === "dream" ||
    notification.type === "dreaming" ||
    notification.type === "dream_complete" ||
    notification.type === "dream_clean" ||
    notification.type === "dream_partial";

  return (
    <div
      data-testid="bar-notification-card"
      className={`notif-glass${isDreamFamily ? " is-dream" : ""} relative w-full overflow-hidden`}
      style={{
        // Single 44px height with content vertically centered. Earlier
        // the card had a 30px content strip + 14px "tuck" bottom that
        // disappeared behind the bar to read as a continuation of the
        // bar's curve. With the notif now globally bottom-anchored
        // (decoupled from bar position, plan 26 follow-up), the tuck
        // had no anchor to disappear into and rendered as empty padding,
        // pushing the content visually off-center. Collapsed both into
        // a single full-height row so content is centered by default.
        height: "var(--notif-outer)",
        // border-radius owned by .notif-glass (see notification-glass.css).
      }}
    >
      {/* Background — animated aurora only while dreaming. */}
      <div
        className={`absolute inset-0 pointer-events-none ${isDreaming ? "animate-dream-banner" : ""}`}
        style={{ background: style.bg }}
      />

      {/* Content row — fills the full outer height, content vertically
          centered via flex (no longer split between a content strip and
          a tuck-bottom). */}
      <div className="relative z-10 flex h-full items-center gap-2 px-4 sm:px-5">
        {/* Icon */}
        <span
          className={`text-[13px] shrink-0 ${isDreaming || notification.type === "info" ? "state-slow-breathe" : ""}`}
          style={{ color: style.accent }}
        >
          {style.icon}
        </span>

        {/* Text */}
        <span className="text-[13px] text-fg-2 flex-1 truncate">
          <span style={{ color: style.accent }} className="font-medium">
            {notification.message}
          </span>
          {notification.detail && (
            <span className="text-fg-3">
              {" · "}
              {notification.detail}
            </span>
          )}
        </span>

        {/* Action — text label or chevron icon. Expanded hit area via
            before pseudo so the visible button stays compact but the
            touch target meets the 44pt iOS minimum on mobile. */}
        {notification.onAction && (
          <button
            onClick={notification.onAction}
            className="relative shrink-0 cursor-pointer transition-colors before:absolute before:-inset-2.5 before:content-['']"
          >
            {notification.actionLabel ? (
              <span className="text-[13px] font-medium text-fg-2 hover:text-fg-1 px-1">
                {notification.actionLabel}
              </span>
            ) : (
              <span className="p-0.5 text-fg-3 hover:text-fg-2">
                <ChevronRight className="h-3.5 w-3.5" />
              </span>
            )}
          </button>
        )}

        {/* Dismiss — 24x24 visible button, 44x44 hit target via before pseudo. */}
        {notification.onDismiss && (
          <button
            onClick={notification.onDismiss}
            aria-label="Dismiss"
            data-testid="bar-notification-dismiss"
            className="relative flex h-6 w-6 shrink-0 cursor-pointer items-center justify-center rounded-md text-fg-2 transition-colors hover:bg-surface-2 hover:text-fg-1 before:absolute before:-inset-2.5 before:content-['']"
          >
            <X className="h-3.5 w-3.5" />
          </button>
        )}
      </div>
    </div>
  );
}
