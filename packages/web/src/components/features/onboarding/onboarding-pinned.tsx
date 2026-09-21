"use client";

// =============================================================================
// Plan 18 — pinned super-notif renderers for /memories hero slot
// =============================================================================
//
// Ports kitchen §34 (`packages/ui/app/dev/kitchen/_sections/34-landing-and-
// onboarding.tsx`) to production components:
//
//   FounderNoteCard      — kind=system_notice, source_kind=onboarding_welcome
//   QuickStartHeroCard   — kind=checklist, slim launcher for the
//                          quick-start deck (quick-start-launchers.tsx)
//   PinnedNotifications  — host: queries pending user-audience rows and
//                          dispatches to the right renderer based on
//                          kind + payload.pin_context.
//
// Surface contract (plan 18 §3.4 + §3.6):
//   - Two rows stack vertically with the founder note above the checklist.
//   - Both render glass-strong card surfaces with the memax signature
//     pink corner glow on the founder note.
//   - Dismiss in either surface mutates the durable row; the SSE
//     stream keeps the other surface (inbox compact) in sync.

import { useEffect, useMemo } from "react";
import { AnimatePresence, motion } from "framer-motion";

import { QuickStartHeroCard } from "./quick-start-launchers";
import { ArrowUp, Coffee, X } from "lucide-react";

import type { ChecklistPayload, Notification } from "memax-sdk";
import { useLocale } from "@/i18n";
import {
  useNotificationDismiss,
  useNotifications,
} from "@/hooks/use-notifications";
import { useActiveHub } from "@/lib/auth";
import { useBar } from "@/contexts/bar-context";

// Render targets that PinnedNotifications knows about. Anything else
// is ignored — the inbox surface renders those.
const PINNABLE_KINDS = new Set<string>([
  "system_notice",
  "checklist",
  "digest",
]);

const SIGNATURE = "var(--signature)";

// -----------------------------------------------------------------------------
// PinnedNotifications — host
// -----------------------------------------------------------------------------

interface PinnedNotificationsProps {
  /** Render target. Today only `memories_hero` is wired; `inbox_hero`
   *  is reserved for the inbox top-strip (future). */
  context: "memories_hero" | "inbox_hero";
}

/**
 * Queries pending user-audience notifications and renders the ones
 * whose payload.pin_context matches the requested target. Returns
 * null when nothing is pinned — the page rhythm collapses cleanly.
 *
 * Client-side filter (plan 18 §5.3): the existing notifications list
 * endpoint has no pin_context query param. At user scale (<50 pending
 * rows) re-filtering in memory is free; a server-side filter is a
 * documented follow-up.
 */
export function PinnedNotifications({ context }: PinnedNotificationsProps) {
  const { data } = useNotifications({ status: "pending" });
  // Current-view hub kind drives the pin_scope_hub_kind filter — the
  // notification carries its own targeting on the payload (see
  // model.ChecklistPayload.PinScopeHubKind). Empty / missing means
  // "any hub view" (back-compat default); "personal" only renders
  // when the active hub is non-team; "team" only when team. The
  // renderer is the single source of truth — mount sites should not
  // gate by hub kind.
  const { activeHub } = useActiveHub();
  const activeHubKind: "personal" | "team" | null = activeHub
    ? activeHub.hub.hub_type === "team"
      ? "team"
      : "personal"
    : null;
  const rows = useMemo<Notification[]>(() => {
    const all = data?.notifications ?? [];
    return all
      .filter((n) => n.audience === "user")
      .filter((n) => PINNABLE_KINDS.has(n.kind))
      .filter((n) => {
        const payload = (n.payload ?? {}) as {
          pin_context?: string;
          pin_scope_hub_kind?: string;
        };
        if (payload.pin_context !== context) return false;
        const scope = payload.pin_scope_hub_kind ?? "";
        if (scope === "") return true; // any-hub default
        // While activeHub hydrates we don't yet know the kind — hold
        // the row off until the kind resolves rather than flashing
        // a row that will disappear a tick later.
        if (activeHubKind === null) return false;
        return scope === activeHubKind;
      })
      .sort((a, b) => {
        if (a.priority !== b.priority) return b.priority - a.priority;
        return (
          new Date(b.created_at).getTime() - new Date(a.created_at).getTime()
        );
      });
  }, [data, context, activeHubKind]);

  if (rows.length === 0) return null;

  return (
    <div className="flex flex-col gap-3">
      <AnimatePresence initial={false}>
        {rows.map((n) => (
          <motion.div
            key={n.id}
            layout
            initial={{ opacity: 0, y: -4 }}
            animate={{ opacity: 1, y: 0 }}
            exit={{ opacity: 0, y: -8 }}
            transition={{ duration: 0.18 }}
          >
            <PinnedDispatch notification={n} />
          </motion.div>
        ))}
      </AnimatePresence>
    </div>
  );
}

/**
 * PinnedDispatch — kind→renderer router for the pinned super-notif
 * surfaces. Exported so non-pin-context surfaces (the inbox view)
 * can render the same onboarding-row visuals as the memories hero
 * without re-implementing the dispatch table.
 */
export function PinnedDispatch({
  notification,
}: {
  notification: Notification;
}) {
  if (notification.kind === "system_notice") {
    return <FounderNoteCard notification={notification} />;
  }
  if (notification.kind === "checklist") {
    // The first week is done in the quick-start deck; the hero only
    // launches it (founder spec 2026-09-21).
    return <QuickStartHeroCard notification={notification} />;
  }
  // digest scaffold — minimal pinned rendering until the first
  // digest producer ships its renderer.
  if (notification.kind === "digest") {
    return <DigestScaffoldCard notification={notification} />;
  }
  return null;
}

// -----------------------------------------------------------------------------
// FounderNoteCard — system_notice + onboarding_welcome pinned variant
// -----------------------------------------------------------------------------

function FounderNoteCard({ notification }: { notification: Notification }) {
  const { t } = useLocale();
  const dismiss = useNotificationDismiss();
  const { openBar } = useBar();

  // Only render the canonical founder-voice content when this row IS
  // the onboarding welcome. Other system_notice producers must ship
  // their own pinned renderer — the contract is "if a producer pins
  // to memories_hero, it owns its visual" (codex P1 L2). A bare
  // system_notice with no matching renderer silently no-renders, by
  // design.
  if (notification.source_kind !== "onboarding_welcome") {
    return null;
  }
  const copy = t.onboarding.welcome;

  return (
    <div
      className="relative rounded-2xl p-5 overflow-hidden"
      style={{
        background: "var(--card)",
        boxShadow:
          "0 1px 0 oklch(from var(--foreground) l c h / 0.04), 0 8px 24px -12px oklch(from var(--foreground) l c h / 0.12)",
        border: "1px solid oklch(from var(--foreground) l c h / 0.06)",
      }}
    >
      {/* Signature gradient corner glow — matches kitchen §34 */}
      <div
        aria-hidden
        className="absolute -top-16 -right-20 w-60 h-60 rounded-full pointer-events-none"
        style={{
          background: `radial-gradient(circle, oklch(from ${SIGNATURE} l c h / 0.18) 0%, transparent 60%)`,
        }}
      />
      <div className="relative flex items-start gap-3">
        <div className="flex-1 min-w-0">
          <div className="flex items-center gap-2 mb-2">
            <span
              className="inline-flex items-center justify-center w-6 h-6 rounded-md"
              style={{
                background: `oklch(from ${SIGNATURE} l c h / 0.14)`,
              }}
              aria-hidden
            >
              <Coffee
                className="w-3.5 h-3.5"
                style={{ color: SIGNATURE }}
                strokeWidth={2.2}
              />
            </span>
            <h3
              className="text-[17px] font-semibold text-foreground"
              style={{ letterSpacing: "-0.015em" }}
            >
              👋 {copy.title}
            </h3>
          </div>
          <p className="text-[14px] leading-relaxed text-fg-2 mb-2">
            {copy.paragraph1}
          </p>
          <p className="text-[14px] leading-relaxed text-fg-2 mb-2">
            {copy.paragraph2}
          </p>
          <p className="text-[14px] leading-relaxed text-fg-2 mb-2">
            {copy.paragraph3}
          </p>
          <p className="text-[14px] leading-relaxed text-fg-2">
            {copy.paragraph4}
          </p>
          <div className="mt-4 flex items-center gap-3">
            <button
              type="button"
              onClick={() => openBar()}
              className="inline-flex items-center gap-1.5 h-9 px-4 rounded-lg text-[13px] font-medium transition-colors hover:brightness-110"
              style={{
                background: `oklch(from ${SIGNATURE} l c h / 0.12)`,
                color: SIGNATURE,
              }}
            >
              {copy.ctaLabel}
              <ArrowUp className="w-3.5 h-3.5 rotate-180" strokeWidth={2.2} />
            </button>
            <p className="text-[13px] text-fg-3">{copy.signature}</p>
          </div>
        </div>
        <button
          type="button"
          onClick={() => dismiss.mutate(notification.id)}
          disabled={dismiss.isPending}
          // 36px square — closer to iOS HIG 44px floor and big
          // enough to thumb-tap reliably on a 360px phone.
          className="shrink-0 w-9 h-9 -mr-2 -mt-2 rounded-md flex items-center justify-center text-fg-4 hover:text-fg-2 hover:bg-surface-2 transition-colors disabled:opacity-50"
          aria-label={copy.dismissAria}
        >
          <X className="w-3.5 h-3.5" strokeWidth={2} />
        </button>
      </div>
    </div>
  );
}

// -----------------------------------------------------------------------------
// DigestScaffoldCard — minimal placeholder until the first digest
// producer ships its renderer (plan 18 P1-2 scaffold)
// -----------------------------------------------------------------------------

function DigestScaffoldCard({ notification }: { notification: Notification }) {
  const payload = (notification.payload ?? {}) as {
    title?: string;
    description?: string;
    items?: Array<{ id?: string; title?: string; description?: string }>;
  };
  if (!payload.title) return null;
  return (
    <div
      className="rounded-2xl p-5"
      style={{
        background: "var(--card)",
        border: "1px solid oklch(from var(--foreground) l c h / 0.06)",
      }}
    >
      <h3 className="text-[14px] font-semibold text-foreground mb-1">
        {payload.title}
      </h3>
      {payload.description && (
        <p className="text-[12px] text-fg-3 mb-3">{payload.description}</p>
      )}
      {Array.isArray(payload.items) && payload.items.length > 0 && (
        <ul className="flex flex-col gap-1.5">
          {payload.items.map((item, i) => (
            <li
              key={item.id ?? i}
              className="text-[13px] text-fg-2 flex items-baseline gap-2"
            >
              <span className="text-fg-4">·</span>
              <div className="flex-1">
                <span className="text-foreground">{item.title}</span>
                {item.description && (
                  <span className="text-fg-3"> — {item.description}</span>
                )}
              </div>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

// -----------------------------------------------------------------------------
// helpers
