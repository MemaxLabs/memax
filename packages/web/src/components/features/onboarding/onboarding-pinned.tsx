"use client";

// =============================================================================
// Plan 18 — pinned super-notif renderers for /memories hero slot
// =============================================================================
//
// Ports kitchen §34 (`packages/ui/app/dev/kitchen/_sections/34-landing-and-
// onboarding.tsx`) to production components:
//
//   FounderNoteCard      — kind=system_notice, source_kind=onboarding_welcome
//   ChecklistCard        — kind=checklist, expanded variant
//   ChecklistStrip       — kind=checklist, collapsed variant (client-only)
//   ChecklistItemRow     — single item row, used by ChecklistCard
//   ProgressDots         — small atom for the header gloss
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

import { useEffect, useMemo, useState } from "react";
import { createPortal } from "react-dom";
import { AnimatePresence, motion } from "framer-motion";

import { HubCreateDialog } from "@/components/features/settings/hub-create-dialog";
import {
  ArrowRight,
  ArrowUp,
  BookOpen,
  Bot,
  Boxes,
  Check,
  Coffee,
  Inbox,
  Library,
  Lock,
  MessageCircle,
  Minus,
  Moon,
  PenLine,
  Sparkles,
  UsersRound,
  X,
  type LucideIcon,
} from "lucide-react";

import type { ChecklistItem, ChecklistPayload, Notification } from "memax-sdk";
import { useInterpolate, useLocale } from "@/i18n";
import {
  useNotificationDismiss,
  useNotifications,
  useResolveNotification,
  useViewChecklistItem,
  useCompleteChecklistItem,
} from "@/hooks/use-notifications";
import { useDreamReport, useDreamTrigger } from "@/hooks/use-dreams";
import { useActiveHub } from "@/lib/auth";
import { useBar } from "@/contexts/bar-context";
import { useIsMobile } from "@/hooks/use-is-mobile";

// Render targets that PinnedNotifications knows about. Anything else
// is ignored — the inbox surface renders those.
const PINNABLE_KINDS = new Set<string>([
  "system_notice",
  "checklist",
  "digest",
]);

const SIGNATURE = "var(--signature)";

// Icon dispatcher — payload items[].icon comes from the server as a
// lucide name (or emoji). We support the known set plus an emoji
// fallback. Unknown name → Sparkles.
const ICON_BY_NAME: Record<string, LucideIcon> = {
  Sparkles,
  Bot,
  Inbox,
  MessageCircle,
  Boxes,
  UsersRound,
  Moon,
  // 2026-05-19: more-semantic-icons swap. Old `welcome`/`first_memory`/
  // `five_memories` rows from the DB still carry the old names —
  // keep the old keys above so historical payloads render, and add
  // the new names below for fresh emissions.
  BookOpen,
  PenLine,
  Library,
};

function resolveItemIcon(name: string | undefined): LucideIcon | string {
  if (!name) return Sparkles;
  if (ICON_BY_NAME[name]) return ICON_BY_NAME[name];
  // Single-glyph emojis pass through.
  if (name.length <= 2) return name;
  return Sparkles;
}

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
    return <ChecklistContainer notification={notification} />;
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
// FounderNoteModal — overlay variant of the founder note, used by the
// welcome checklist row's "Read it" CTA. Lives separately from the
// pinned card because the pinned card may have been dismissed before
// the user clicks "Read it" — without this modal there's no way to
// actually read the note from the checklist (codex Low: welcome could
// complete without surfacing content). Marks the welcome item complete
// on close so the checklist truthfulness contract holds.
// -----------------------------------------------------------------------------

function FounderNoteModal({ onClose }: { onClose: () => void }) {
  const { t } = useLocale();
  const { openBar } = useBar();
  const copy = t.onboarding.welcome;

  // Esc closes per Dialog convention. Scoped listener — removed on
  // unmount so we don't leak between open/close cycles.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") onClose();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onClose]);

  return (
    <div
      className="fixed inset-0 z-shell-bar flex items-center justify-center p-4"
      onClick={onClose}
      style={{
        background: "oklch(from var(--foreground) l c h / 0.32)",
        backdropFilter: "blur(2px)",
      }}
    >
      <div
        role="dialog"
        aria-modal="true"
        aria-labelledby="founder-note-modal-title"
        onClick={(e) => e.stopPropagation()}
        // max-h + overflow-y-auto cap the modal at 90vh so on short
        // viewports (landscape phone, ~480px tall) the content
        // scrolls inside the dialog rather than overflowing off the
        // top + bottom edges. Was unbounded — caused users on small
        // screens to lose access to the X button + CTA.
        className="relative w-full max-w-xl max-h-[90vh] overflow-y-auto rounded-2xl p-6"
        style={{
          background: "var(--card)",
          boxShadow:
            "0 1px 0 oklch(from var(--foreground) l c h / 0.04), 0 24px 64px -12px oklch(from var(--foreground) l c h / 0.24)",
          border: "1px solid oklch(from var(--foreground) l c h / 0.06)",
        }}
      >
        <div
          aria-hidden
          className="absolute -top-16 -right-20 w-60 h-60 rounded-full pointer-events-none"
          style={{
            background: `radial-gradient(circle, oklch(from ${SIGNATURE} l c h / 0.18) 0%, transparent 60%)`,
          }}
        />
        <div className="relative flex items-start gap-3">
          <div className="flex-1 min-w-0">
            <div className="flex items-center gap-2 mb-3">
              <span
                className="inline-flex items-center justify-center w-6 h-6 rounded-md"
                style={{ background: `oklch(from ${SIGNATURE} l c h / 0.14)` }}
                aria-hidden
              >
                <Coffee
                  className="w-3.5 h-3.5"
                  style={{ color: SIGNATURE }}
                  strokeWidth={2.2}
                />
              </span>
              <h3
                id="founder-note-modal-title"
                className="text-[18px] font-semibold text-foreground"
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
            <div className="mt-5 flex items-center gap-3">
              <button
                type="button"
                onClick={() => {
                  openBar();
                  onClose();
                }}
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
            onClick={onClose}
            // 36px touch target — see FounderNoteCard's dismiss.
            className="shrink-0 w-9 h-9 -mr-2 -mt-2 rounded-md flex items-center justify-center text-fg-4 hover:text-fg-2 hover:bg-surface-2 transition-colors"
            aria-label={copy.dismissAria}
          >
            <X className="w-3.5 h-3.5" strokeWidth={2} />
          </button>
        </div>
      </div>
    </div>
  );
}

// -----------------------------------------------------------------------------
// ChecklistCard / ChecklistStrip — kind=checklist
// -----------------------------------------------------------------------------

function ChecklistContainer({ notification }: { notification: Notification }) {
  // Collapse state is client-only (plan 18 §5.1). localStorage so a
  // page reload preserves user choice but it doesn't hit the server.
  // Default-collapsed on mobile so the checklist doesn't push the
  // founder note + seed memories off-screen on a 360px viewport.
  // Once the user explicitly opens or closes it, that choice
  // persists via localStorage and overrides the mobile default.
  //
  // IsMobileProvider initialises isMobile to `false` and only fills
  // in the real matchMedia value in an effect (use-is-mobile.tsx),
  // so the useState initializer below CANNOT read the real value on
  // first render — it always sees `false`. The post-hydration
  // useEffect below reconciles: when isMobile flips true AND
  // localStorage has no prior choice, we collapse retroactively and
  // persist that as "true" so future renders + reloads stay
  // collapsed. Without this effect, the mobile auto-collapse
  // silently failed on first-time mobile users (codex Medium).
  const storageKey = `memax.checklist.${notification.id}.collapsed`;
  const isMobile = useIsMobile();
  const [collapsed, setCollapsed] = useState<boolean>(() => {
    if (typeof window === "undefined") return false;
    const stored = window.localStorage.getItem(storageKey);
    if (stored === "true") return true;
    if (stored === "false") return false;
    return false; // default open; the mobile path is patched up by the effect
  });
  useEffect(() => {
    if (typeof window === "undefined") return;
    const stored = window.localStorage.getItem(storageKey);
    // User has already chosen — respect that choice over the
    // mobile default. Also early-exit on desktop so a resize-down
    // doesn't surprise-collapse an open checklist.
    if (stored !== null) return;
    if (!isMobile) return;
    setCollapsed(true);
    window.localStorage.setItem(storageKey, "true");
  }, [isMobile, storageKey]);
  const setCollapsedWithStore = (next: boolean) => {
    setCollapsed(next);
    if (typeof window !== "undefined") {
      window.localStorage.setItem(storageKey, String(next));
    }
  };

  const payload = (notification.payload ?? {}) as Partial<ChecklistPayload>;
  if (!payload.items?.length) {
    return null;
  }

  // Expand/collapse motion philosophy (Linear / Notion / Vercel
  // convention): the surrounding PinnedNotifications wrapper already
  // carries `layout` on each row, so the page reflow when card ↔
  // strip swap is FLIP-driven and free. The job HERE is just to
  // cross-fade the content cleanly without staging "gone → blank →
  // here" via height animation.
  //
  // Earlier this used `mode="wait"` + `height: 0 → auto` which:
  //   1. Sequential exit→enter = 440ms total (user-perceived as
  //      "the card disappears before the strip arrives" — exactly
  //      what the user reported as "heavy and arbitrary").
  //   2. Animating `height` triggers reflow per frame and stutters
  //      on long checklists.
  //
  // The popLayout fix:
  //   - mode="popLayout" pops the exiting node out of the document
  //     flow so the entering node takes its place immediately. No
  //     blank gap.
  //   - Drop `height` entirely; let the outer `layout` wrapper
  //     reflow naturally.
  //   - 120ms opacity-only fades with the industry-standard
  //     material-emphasized easing curve. Snappy, not luxurious.
  //   - Respects prefers-reduced-motion via framer-motion's global
  //     setting (configured at app shell level).
  return (
    <AnimatePresence initial={false} mode="popLayout">
      {collapsed ? (
        <motion.div
          key="strip"
          initial={{ opacity: 0 }}
          animate={{ opacity: 1 }}
          exit={{ opacity: 0 }}
          transition={{ duration: 0.12, ease: [0.2, 0, 0, 1] }}
        >
          <ChecklistStrip
            payload={payload as ChecklistPayload}
            onOpen={() => setCollapsedWithStore(false)}
          />
        </motion.div>
      ) : (
        <motion.div
          key="card"
          initial={{ opacity: 0 }}
          animate={{ opacity: 1 }}
          exit={{ opacity: 0 }}
          transition={{ duration: 0.12, ease: [0.2, 0, 0, 1] }}
        >
          <ChecklistCard
            notificationId={notification.id}
            payload={payload as ChecklistPayload}
            onCollapse={() => setCollapsedWithStore(true)}
          />
        </motion.div>
      )}
    </AnimatePresence>
  );
}

function ChecklistCard({
  notificationId,
  payload,
  onCollapse,
}: {
  notificationId: string;
  payload: ChecklistPayload;
  onCollapse: () => void;
}) {
  const { t } = useLocale();
  const interpolate = useInterpolate();
  const resolve = useResolveNotification();
  const copy = t.onboarding.checklist;

  const requiredIDs = useMemo(
    () => new Set(payload.required_ids ?? []),
    [payload.required_ids],
  );
  const doneAll = payload.items.filter((it) => !!it.completed_at).length;
  const doneRequired = payload.items.filter(
    (it) => !!it.completed_at && requiredIDs.has(it.id),
  ).length;
  const totalAll = payload.items.length;
  const requiredTotal = requiredIDs.size;
  const allRequiredDone = requiredTotal > 0 && doneRequired === requiredTotal;

  return (
    <div
      className="relative rounded-2xl overflow-hidden"
      style={{
        background: "var(--card)",
        boxShadow:
          "0 1px 0 oklch(from var(--foreground) l c h / 0.04), 0 8px 24px -12px oklch(from var(--foreground) l c h / 0.12)",
        border: "1px solid oklch(from var(--foreground) l c h / 0.06)",
      }}
    >
      <div className="flex items-center gap-3 px-5 pt-5 pb-3">
        <div className="flex-1 min-w-0">
          <h3 className="text-[14px] font-semibold text-foreground">
            {copy.title}
          </h3>
          <p className="text-[12px] text-fg-3 mt-0.5">
            {allRequiredDone
              ? copy.celebrateSubtitle
              : interpolate(copy.progressFormat, {
                  done: String(doneAll),
                  total: String(totalAll),
                })}
          </p>
        </div>
        <ProgressDots total={totalAll} done={doneAll} />
        <button
          type="button"
          onClick={onCollapse}
          // 36px touch target — header buttons rendered on the same
          // row as the dismiss; keep them visually paired.
          className="shrink-0 w-9 h-9 rounded-md flex items-center justify-center text-fg-4 hover:text-fg-2 hover:bg-surface-2 transition-colors"
          aria-label={copy.collapseAria}
        >
          <Minus className="w-3.5 h-3.5" strokeWidth={2} />
        </button>
        <button
          type="button"
          onClick={() =>
            resolve.mutate({ id: notificationId, action: "dismiss" })
          }
          disabled={resolve.isPending}
          className="shrink-0 w-9 h-9 -mr-2 rounded-md flex items-center justify-center text-fg-4 hover:text-fg-2 hover:bg-surface-2 transition-colors disabled:opacity-50"
          aria-label={copy.dismissAria}
        >
          <X className="w-3.5 h-3.5" strokeWidth={2} />
        </button>
      </div>

      <div
        aria-hidden
        className="h-px mx-5"
        style={{ background: "oklch(from var(--foreground) l c h / 0.05)" }}
      />

      <div className="px-5 py-2 relative">
        {payload.items.map((item) => (
          <ChecklistItemRow
            key={item.id}
            notificationId={notificationId}
            item={item}
            payload={payload}
          />
        ))}
      </div>
    </div>
  );
}

function ChecklistStrip({
  payload,
  onOpen,
}: {
  payload: ChecklistPayload;
  onOpen: () => void;
}) {
  const { t } = useLocale();
  const copy = t.onboarding.checklist;
  const doneAll = payload.items.filter((it) => !!it.completed_at).length;
  return (
    <button
      type="button"
      onClick={onOpen}
      className="w-full rounded-xl px-4 py-2.5 flex items-center gap-3 hover:bg-surface-2/60 transition-colors text-left"
      style={{
        background: "var(--card)",
        border: "1px solid oklch(from var(--foreground) l c h / 0.06)",
      }}
    >
      <span className="text-[13px] font-medium text-foreground">
        {copy.title}
      </span>
      <span className="text-[12px] text-fg-3">
        · {doneAll}/{payload.items.length}
      </span>
      <ProgressDots total={payload.items.length} done={doneAll} />
      <span className="ml-auto text-[12px] text-fg-3 inline-flex items-center gap-1">
        {copy.stripOpen}
        <ArrowRight className="w-3 h-3" strokeWidth={2.2} />
      </span>
    </button>
  );
}

// -----------------------------------------------------------------------------
// ChecklistItemRow — per-item row with status icon, CTA, and progress bar
// -----------------------------------------------------------------------------

function ChecklistItemRow({
  notificationId,
  item,
  payload,
}: {
  notificationId: string;
  item: ChecklistItem;
  payload: ChecklistPayload;
}) {
  const { t } = useLocale();
  const completeItem = useCompleteChecklistItem();
  const viewItem = useViewChecklistItem();
  const dreamTrigger = useDreamTrigger();
  const dreamReport = useDreamReport();
  const { openBar } = useBar();
  const copy = t.onboarding.checklist;
  const itemCopy = lookupItemCopy(t, item.id);

  // first_dream "Dreaming…" hint state. Tied to the actual
  // dream_runs.status the worker reports (via useDreamReport, which
  // polls every 20s while running and invalidates on SSE) so the
  // caption auto-hides on terminal status instead of sticking for
  // the rest of the session. `hintDismissed` lets the user close
  // the hint manually; lives in local state because the hint is
  // short-lived (the underlying run finishes in 30-90s and the
  // dismiss intent doesn't need to survive a reload).
  const [dreamingHintDismissed, setDreamingHintDismissed] = useState(false);

  // Local modal/dialog state for items that open a UI affordance
  // inline instead of navigating:
  //   - first_hub_invite → HubCreateDialog
  //   - welcome          → FounderNoteModal (so dismissed-pinned-card
  //                        users can still read the note)
  const [showHubCreate, setShowHubCreate] = useState(false);
  const [showFounderModal, setShowFounderModal] = useState(false);

  // Server is the single source of truth. The materializer on the
  // notifications read path computes completed_at for queryable
  // items (memories, agents, team hubs) from the underlying tables
  // on every fetch, and SSE invalidations on memory.changed /
  // agent.changed / hub.list.changed refresh the notifications cache
  // — so this row's `item.completed_at` already reflects live truth
  // by the time it gets here. No client-side live-fallback, no
  // write-through effect, no locked_by override. If you're seeing
  // a stale state, the bug is in the server materializer or the
  // SSE invalidation registry, not here.
  const completed = !!item.completed_at;

  const locked =
    Array.isArray(item.locked_by) &&
    item.locked_by.some((depID) => {
      const dep = payload.items.find((x) => x.id === depID);
      return !dep?.completed_at;
    });

  const Icon = resolveItemIcon(item.icon);
  const isEmojiIcon = typeof Icon === "string";

  const progressTarget = item.progress?.target ?? null;
  const progressCurrent = item.progress?.current ?? null;
  const showProgress =
    !completed &&
    !locked &&
    typeof progressCurrent === "number" &&
    typeof progressTarget === "number" &&
    progressTarget > 0;

  // Per-item click semantics:
  //   - welcome          → open FounderNoteModal, complete on close
  //   - connect_agent    → navigate /agents
  //   - first_memory     → openBar (memory create → memory.changed SSE
  //                        → notifications invalidate → materializer
  //                        ticks five_memories / first_memory on refetch)
  //   - first_ask        → openBar (no backing table — completion is
  //                        written from the ask handler inline)
  //   - first_hub_invite → open HubCreateDialog (hub.list.changed →
  //                        notifications invalidate → materializer ticks)
  //   - first_dream      → fire dreams.trigger(); the materializer
  //                        does not cover first_dream (still uses
  //                        payload.completed_at), so we optimistically
  //                        complete on success. useCompleteChecklistItem
  //                        has skipGlobalToast so a server-side
  //                        item_locked failure here doesn't surface a
  //                        misleading toast — the dream already ran.
  //   - everything else  → fall through to item.cta_url navigation
  const isWelcomeItem = item.id === "welcome";
  const isAgentItem = item.id === "connect_agent";
  const isHubInviteItem = item.id === "first_hub_invite";
  const isFirstDreamItem = item.id === "first_dream";
  const isBarItem =
    !item.cta_url && (item.id === "first_memory" || item.id === "first_ask");

  const handleClick = () => {
    if (isWelcomeItem) {
      setShowFounderModal(true);
      return;
    }
    viewItem.mutate({ notificationId, itemId: item.id });
    if (isAgentItem) {
      window.location.href = "/agents";
      return;
    }
    if (isHubInviteItem) {
      setShowHubCreate(true);
      return;
    }
    if (isFirstDreamItem) {
      if (dreamTrigger.isPending) return;
      dreamTrigger.mutate(undefined, {
        onSuccess: () => {
          completeItem.mutate({ notificationId, itemId: item.id });
        },
      });
      return;
    }
    if (item.cta_url) {
      window.location.href = item.cta_url;
      return;
    }
    if (isBarItem) {
      openBar();
    }
  };

  // The first_dream button needs a pending-state lock so a double-tap
  // doesn't fire two triggers before the optimistic complete lands.
  const ctaPending = isFirstDreamItem && dreamTrigger.isPending;

  return (
    <div
      className="flex items-center gap-3 py-2 px-2 -mx-2 rounded-lg transition-colors hover:bg-surface-2/60"
      style={{ opacity: locked ? 0.55 : 1 }}
    >
      <div
        className="shrink-0 w-7 h-7 rounded-full flex items-center justify-center"
        style={{
          background: completed
            ? `oklch(from ${SIGNATURE} l c h / 0.12)`
            : "oklch(from var(--foreground) l c h / 0.04)",
        }}
      >
        {completed ? (
          <Check
            className="w-3.5 h-3.5"
            strokeWidth={2.5}
            style={{ color: SIGNATURE }}
          />
        ) : locked ? (
          <Lock className="w-3 h-3 text-fg-4" strokeWidth={2} />
        ) : isEmojiIcon ? (
          <span className="text-[14px]">{Icon as string}</span>
        ) : (
          (() => {
            const IconComp = Icon as LucideIcon;
            return (
              <IconComp className="w-3.5 h-3.5 text-fg-3" strokeWidth={1.8} />
            );
          })()
        )}
      </div>

      <div className="flex-1 min-w-0">
        <p
          className={`text-[13px] font-medium ${
            completed ? "text-fg-3" : "text-foreground"
          }`}
        >
          {completed ? itemCopy.completedTitle : itemCopy.title}
        </p>
        {!completed && (
          <p className="text-[12px] text-fg-4 mt-0.5 leading-snug">
            {locked ? copy.lockedHint : itemCopy.description}
          </p>
        )}
        {completed &&
          isFirstDreamItem &&
          !dreamingHintDismissed &&
          dreamReport.data?.run?.status === "running" &&
          itemCopy.dreamingHint && (
            // Tied to the ACTUAL dream_runs.status the worker reports
            // — auto-hides when the run reaches a terminal state
            // (completed/failed/partial_failed/skipped). useDreamReport
            // polls every 20s while running as an SSE-drop fallback,
            // so a stale "running" doesn't leave the caption stuck
            // forever. Dismiss button below is a manual escape hatch
            // for users who prefer no in-row footer chatter.
            <div className="flex items-center gap-1.5 mt-0.5">
              <p
                className="text-[12px] leading-snug"
                style={{ color: SIGNATURE }}
              >
                {itemCopy.dreamingHint}
              </p>
              <button
                type="button"
                onClick={() => setDreamingHintDismissed(true)}
                aria-label={copy.dismissAria}
                // Inline dismiss: a true 36px touch target would add
                // a too-tall hint row; 28px keeps the hint visually
                // light while still tap-reliable on a phone.
                className="shrink-0 w-7 h-7 -my-1 rounded flex items-center justify-center text-fg-4 hover:text-fg-2 hover:bg-surface-2 transition-colors"
              >
                <X className="w-3 h-3" strokeWidth={2} />
              </button>
            </div>
          )}
      </div>

      <div className="shrink-0 flex items-center gap-2">
        {showProgress && (
          <>
            <div
              className="w-20 h-1.5 rounded-full overflow-hidden"
              style={{
                background: "oklch(from var(--foreground) l c h / 0.08)",
              }}
            >
              <div
                className="h-full rounded-full transition-all"
                style={{
                  width: `${Math.min(100, (progressCurrent! / progressTarget!) * 100)}%`,
                  background: SIGNATURE,
                }}
              />
            </div>
            <span className="text-[11px] tabular-nums text-fg-3 font-medium">
              {progressCurrent}/{progressTarget}
            </span>
          </>
        )}
        {(isWelcomeItem || (!completed && !locked && !showProgress)) &&
          itemCopy.ctaLabel && (
            <button
              type="button"
              onClick={handleClick}
              disabled={ctaPending}
              // 36px height + 12px x-padding — same touch-target
              // treatment as the dismiss/close buttons. Was 28px
              // tall which was unreliable to tap on a phone.
              className="inline-flex items-center gap-1 h-9 px-3 rounded-md text-[12px] font-medium text-fg-2 hover:text-foreground hover:bg-surface-3 transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
              style={{
                background: "oklch(from var(--foreground) l c h / 0.05)",
              }}
            >
              {itemCopy.ctaLabel}
              <ArrowRight className="w-3 h-3" strokeWidth={2.2} />
            </button>
          )}
      </div>
      {/* Both modals MUST portal to document.body. PinnedNotifications
          wraps each row in a `motion.div`, which applies a `transform`
          style that becomes the containing block for position:fixed
          descendants (CSS Transforms L2 §6.1). Without the portal the
          modal's fixed positioning + the parent card's
          `overflow:hidden` together clip the dialog inside the
          checklist card frame. */}
      {showHubCreate &&
        typeof document !== "undefined" &&
        createPortal(
          // HubCreateDialog is just the form (a <Surface>), with no
          // modal chrome — callers supply the backdrop + centered
          // container. Mirrors the hub-identity-chip.tsx wrapping
          // pattern so this opens identically to the rest of the
          // app's "Create team hub" affordance instead of rendering
          // bare in the top-left of document.body.
          <>
            <div
              className="fixed inset-0 z-takeover"
              style={{
                background: "rgba(0,0,0,0.4)",
                touchAction: "none",
                overscrollBehavior: "contain",
              }}
              onClick={() => setShowHubCreate(false)}
              onTouchMove={(e) => e.preventDefault()}
            />
            <div className="fixed inset-0 z-takeover flex items-center justify-center pointer-events-none p-6">
              <div className="pointer-events-auto w-full max-w-md">
                <HubCreateDialog
                  onCreated={() => setShowHubCreate(false)}
                  onCancel={() => setShowHubCreate(false)}
                />
              </div>
            </div>
          </>,
          document.body,
        )}
      {showFounderModal &&
        typeof document !== "undefined" &&
        createPortal(
          <FounderNoteModal
            onClose={() => {
              setShowFounderModal(false);
              // Mark welcome complete only AFTER the user has actually
              // opened (and is now closing) the note — the click that
              // opened the modal doesn't auto-complete, so a misclick
              // is recoverable. Idempotent: re-opens after completion
              // are fine because we gate the mutation on !completed.
              if (!completed) {
                completeItem.mutate({ notificationId, itemId: item.id });
              }
            }}
          />,
          document.body,
        )}
    </div>
  );
}

// -----------------------------------------------------------------------------
// ProgressDots — small atom for the header gloss
// -----------------------------------------------------------------------------

function ProgressDots({ total, done }: { total: number; done: number }) {
  return (
    <div className="flex items-center gap-1" aria-hidden>
      {Array.from({ length: total }).map((_, i) => (
        <span
          key={i}
          className="block rounded-full transition-colors"
          style={{
            width: 6,
            height: 6,
            background:
              i < done
                ? SIGNATURE
                : "oklch(from var(--foreground) l c h / 0.18)",
          }}
        />
      ))}
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
// -----------------------------------------------------------------------------

// Locale lookup tolerates unknown item IDs (e.g. an admin restart
// emits an item id that hasn't been translated yet) by falling back
// to the raw server-side title/description on the payload — same
// safety net as the inbox unknown-kind fallback (plan 18 §4.2).
function lookupItemCopy(
  t: ReturnType<typeof useLocale>["t"],
  itemId: string,
): {
  title: string;
  description: string;
  completedTitle: string;
  ctaLabel?: string;
  // first_dream-only: caption shown after the user triggers the
  // dream, replacing the description while the worker actually
  // runs the dream (~30-90s). Optional because other items don't
  // have a "background work in progress" state.
  dreamingHint?: string;
} {
  const known = t.onboarding.checklist.items as Record<
    string,
    {
      title: string;
      description: string;
      completedTitle: string;
      ctaLabel?: string;
      dreamingHint?: string;
    }
  >;
  const entry = known[itemId];
  if (entry) return entry;
  return {
    title: itemId,
    description: "",
    completedTitle: itemId,
  };
}
