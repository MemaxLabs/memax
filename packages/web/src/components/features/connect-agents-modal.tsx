"use client";

/**
 * ConnectAgentsModal — the avatar-menu entry to the connect-an-agent
 * flow, as a centered glass overlay (founder call 2026-08-21).
 *
 * The menu item used to route to /agents/connect, which navigated the
 * user away from wherever they were AND replaced the /agents tab's
 * real connected-state view with setup instructions. Connecting an
 * agent from the avatar menu is a quick side-task, not a destination:
 * a centered modal (the design language's overlay idiom — search and
 * capture are the same shape) keeps the user's surface intact behind
 * it. The /agents/connect route stays for deep links and the "+" tile
 * on the agents grid, where a full page IS the right treatment.
 */

import { useEffect } from "react";
import { createPortal } from "react-dom";
import { X } from "lucide-react";
import { Surface } from "@memaxlabs/ui";
import { useLocale } from "@/i18n";
import { acquireBodyScrollLock } from "@/lib/scroll-lock";
import { ConnectAgentsBody } from "./connect-agents-section";

export function ConnectAgentsModal({ onClose }: { onClose: () => void }) {
  const { t } = useLocale();

  useEffect(() => acquireBodyScrollLock(), []);
  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        e.stopPropagation();
        onClose();
      }
    };
    window.addEventListener("keydown", handler, true);
    return () => window.removeEventListener("keydown", handler, true);
  }, [onClose]);

  if (typeof document === "undefined") return null;

  return createPortal(
    <>
      <div
        className="fixed inset-0 z-takeover"
        style={{
          background: "rgba(0,0,0,0.4)",
          touchAction: "none",
          overscrollBehavior: "contain",
        }}
        onClick={onClose}
        onTouchMove={(e) => e.preventDefault()}
      />
      <div className="fixed inset-0 z-takeover flex items-center justify-center pointer-events-none p-4 sm:p-6">
        <div
          role="dialog"
          aria-modal="true"
          aria-label={t.agentConfigs.connectPageTitle}
          className="pointer-events-auto w-full max-w-xl animate-fade-up"
        >
          <Surface
            variant="subtle"
            rounded="2xl"
            className="glass-dropdown backdrop-blur-sm max-h-[85dvh] overflow-y-auto px-5 py-5"
          >
            <div className="mb-3 flex items-start justify-between gap-3">
              <div>
                <h2 className="text-[15px] font-semibold text-foreground">
                  {t.agentConfigs.connectPageTitle}
                </h2>
                <p className="mt-0.5 text-[13px] text-fg-3">
                  {t.agentConfigs.connectPageSubtitle}
                </p>
              </div>
              <button
                type="button"
                onClick={onClose}
                aria-label={t.personas.close}
                className="shrink-0 rounded-lg p-1.5 text-fg-3 transition-colors cursor-pointer hover:bg-surface-2 hover:text-fg-1"
              >
                <X className="h-4 w-4" />
              </button>
            </div>
            <ConnectAgentsBody />
          </Surface>
        </div>
      </div>
    </>,
    document.body,
  );
}
