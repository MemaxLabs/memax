"use client";

/**
 * ChatSessionsList — mobile full-page sessions list. Replaces the
 * prior MobileSidebarSheet pattern (industry: ChatGPT / Telegram /
 * Linear all use a dedicated route, not a slide-over).
 *
 * Group order matches the desktop sidebar:
 *   1. Pinned (signature pin icon)
 *   2. Today / Yesterday / This week / Earlier
 *   3. Archived (collapsed accordion at the bottom)
 *
 * Pin/Archive/Delete actions live in the per-row ⋯ menu; the page
 * itself is read-only chrome above that surface.
 */

import { useMemo, useState } from "react";
import {
  Archive,
  ArrowLeft,
  ChevronRight,
  MessageSquarePlus,
  Pin,
} from "lucide-react";
import type { ChatSession } from "memax-sdk";
import { cn } from "@memaxlabs/ui";
import { useChatSessions } from "@/hooks/use-chat";
import { useLocale } from "@/i18n";
import { MOBILE_BAR_HEIGHT, MOBILE_DOCK_BOTTOM_GAP_PX } from "@/lib/layout";
import { ChatSessionRowMenu } from "./chat-session-row-menu";

export interface ChatSessionsListProps {
  sessions: ChatSession[];
  loading?: boolean;
  onSelect: (sessionId: string) => void;
  onCreate: () => void | Promise<void>;
}

interface SessionGroup {
  key: "pinned" | "today" | "yesterday" | "thisWeek" | "earlier";
  label: string;
  sessions: ChatSession[];
}

export function ChatSessionsList({
  sessions,
  loading,
  onSelect,
  onCreate,
}: ChatSessionsListProps) {
  const { t } = useLocale();
  const groups = useMemo(
    () => groupSessions(sessions, t.chat.sidebar),
    [sessions, t.chat.sidebar],
  );

  // View state: active (default) or archived. Tapping the
  // "Archived" entry pushes into a dedicated archived view that
  // takes over the entire surface — iOS Mail / Telegram / WhatsApp
  // pattern. The previous inline accordion competed with the active
  // list for vertical space and read as buggy on mobile where the
  // viewport is small. Browser back is the natural escape; the
  // explicit back arrow in the archived header is the affordance.
  const [view, setView] = useState<"active" | "archived">("active");

  // Bottom reserve clears the floating mobile bar entirely. The bar
  // occupies the viewport band [MOBILE_DOCK_BOTTOM_GAP_PX,
  // MOBILE_DOCK_BOTTOM_GAP_PX + MOBILE_BAR_HEIGHT] = [68px, 140px]
  // from the bottom edge (+ safe-area). Earlier this used a smaller
  // 56+16px reserve that overlapped the bar's band — the last 60-ish
  // px of the list disappeared behind it. Now the list ends 12px
  // above the bar's top edge.
  const mobileBarHeightPx = parseInt(MOBILE_BAR_HEIGHT, 10);
  const bottomReserve = `calc(${MOBILE_DOCK_BOTTOM_GAP_PX + mobileBarHeightPx + 12}px + env(safe-area-inset-bottom))`;

  if (view === "archived") {
    return (
      <ArchivedFullView
        onSelect={onSelect}
        onBack={() => setView("active")}
        bottomReserve={bottomReserve}
      />
    );
  }

  return (
    <div
      className="fixed left-0 right-0 flex flex-col overflow-hidden"
      style={{
        top: "calc(3.5rem + env(safe-area-inset-top))",
        bottom: bottomReserve,
      }}
    >
      <button
        type="button"
        onClick={() => void onCreate()}
        className={cn(
          "mx-3 mt-3 flex h-12 shrink-0 items-center justify-center gap-2 rounded-[var(--app-radius-surface)]",
          "border border-border/40 bg-surface-1 text-[14px] font-medium text-fg-1",
          "transition-colors active:bg-surface-2",
        )}
      >
        <MessageSquarePlus className="h-4 w-4" aria-hidden />
        <span>{t.chat.newChat}</span>
      </button>

      {/* Active sessions — only this region scrolls. The Archived
          entry below is a navigation tap that pushes into a
          dedicated takeover view (iOS Mail / Telegram pattern).
          The previous inline accordion that grew UP from the bottom
          competed with the active list for vertical space and
          read as buggy on small viewports. */}
      <div className="mt-3 min-h-0 flex-1 overflow-y-auto overscroll-contain px-3">
        {loading && sessions.length === 0 ? (
          <ListSkeleton />
        ) : sessions.length === 0 ? (
          <div className="px-2 py-12 text-center text-[13px] text-fg-3">
            {t.chat.sidebar.noSessions}
          </div>
        ) : (
          <div className="flex flex-col gap-5 pb-2 pt-2">
            {groups
              .filter((g) => g.sessions.length > 0)
              .map((g) => (
                <ListGroup
                  key={g.key}
                  label={g.label}
                  sessions={g.sessions}
                  onSelect={onSelect}
                  showPin={g.key === "pinned"}
                />
              ))}
          </div>
        )}
      </div>
      <ArchivedNavRow onOpen={() => setView("archived")} />
    </div>
  );
}

/**
 * ArchivedNavRow — bottom-anchored entry that navigates into the
 * dedicated archived takeover view. Single tap target, ChevronRight
 * to signal "this pushes somewhere" rather than the prior ChevronDown
 * accordion affordance.
 */
function ArchivedNavRow({ onOpen }: { onOpen: () => void }) {
  const { t } = useLocale();
  return (
    <div className="shrink-0 border-t border-border/30 bg-background">
      <button
        type="button"
        onClick={onOpen}
        className="flex h-12 w-full items-center gap-2 px-5 text-[13px] text-fg-3 transition-colors active:bg-surface-2"
      >
        <Archive className="h-4 w-4 shrink-0" aria-hidden />
        <span className="flex-1 text-left">{t.chat.sidebar.showArchived}</span>
        <ChevronRight className="h-4 w-4 shrink-0 text-fg-4" aria-hidden />
      </button>
    </div>
  );
}

/**
 * ArchivedFullView — dedicated takeover for the archived chats list
 * on mobile. Fixed-position container same shape as the active list
 * (top: header offset, bottom: bar-clear reserve), but with a back
 * affordance in a sticky header so the user can return. Mirrors
 * iOS Mail's Junk folder, Telegram's Archived chats, WhatsApp's
 * Archived screen — dedicated viewport, single scroll region, clear
 * way back. No more 40vh accordion fighting the active list for
 * space.
 */
function ArchivedFullView({
  onSelect,
  onBack,
  bottomReserve,
}: {
  onSelect: (id: string) => void;
  onBack: () => void;
  bottomReserve: string;
}) {
  const { t } = useLocale();
  const archivedQuery = useChatSessions({ archived: true });
  const archived = (archivedQuery.data?.sessions ?? []).filter((s) =>
    Boolean(s.archived_at),
  );

  return (
    <div
      className="fixed left-0 right-0 flex flex-col overflow-hidden"
      style={{
        top: "calc(3.5rem + env(safe-area-inset-top))",
        bottom: bottomReserve,
      }}
    >
      {/* Sticky header: back arrow + title. Standard iOS push-nav
          chrome — left-anchored back, centered/leading title. */}
      <div className="flex h-12 shrink-0 items-center gap-2 border-b border-border/30 px-3">
        <button
          type="button"
          onClick={onBack}
          aria-label={t.chat.sidebar.heading}
          className="flex h-9 w-9 items-center justify-center rounded-md text-fg-2 transition-colors active:bg-surface-2"
        >
          <ArrowLeft className="h-5 w-5" aria-hidden />
        </button>
        <span className="text-[14px] font-medium text-fg-1">
          {t.chat.sidebar.groupArchived}
        </span>
      </div>

      <div className="min-h-0 flex-1 overflow-y-auto overscroll-contain px-3 py-3">
        {archivedQuery.isLoading && archived.length === 0 ? (
          <ListSkeleton />
        ) : archived.length === 0 ? (
          <div className="px-3 py-12 text-center text-[13px] text-fg-3">
            {t.chat.sidebar.noArchived}
          </div>
        ) : (
          <ListGroup
            label=""
            sessions={archived}
            onSelect={(id) => {
              onSelect(id);
              onBack();
            }}
          />
        )}
      </div>
    </div>
  );
}

function ListGroup({
  label,
  sessions,
  onSelect,
  showPin,
}: {
  label: string;
  sessions: ChatSession[];
  onSelect: (id: string) => void;
  showPin?: boolean;
}) {
  const { t } = useLocale();

  return (
    <div>
      {label && (
        <div className="px-1 pb-1.5 text-[11px] font-medium uppercase tracking-wide text-fg-3">
          {label}
        </div>
      )}
      <ul className="flex flex-col gap-0.5">
        {sessions.map((s) => (
          <li key={s.id}>
            <div
              className={cn(
                "flex h-12 w-full items-center rounded-[var(--app-radius-surface)] pl-3 pr-1 text-fg-1 transition-colors active:bg-surface-2",
              )}
            >
              <button
                type="button"
                onClick={() => onSelect(s.id)}
                className="flex min-w-0 flex-1 items-center gap-2 text-left text-[15px]"
              >
                {showPin || s.pinned_at ? (
                  <Pin
                    className="h-3.5 w-3.5 shrink-0 text-signature"
                    aria-hidden
                    strokeWidth={2.5}
                  />
                ) : null}
                <span className="line-clamp-1">
                  {s.title || t.chat.sidebar.untitled}
                </span>
              </button>
              <ChatSessionRowMenu session={s} variant="mobile" />
            </div>
          </li>
        ))}
      </ul>
    </div>
  );
}

function ListSkeleton() {
  return (
    <div className="flex flex-col gap-2 px-1 py-2">
      {Array.from({ length: 6 }).map((_, i) => (
        <div
          key={i}
          className="h-10 rounded-[var(--app-radius-surface)] bg-surface-2/40 animate-pulse"
          style={{ width: `${72 + ((i * 11) % 24)}%` }}
        />
      ))}
    </div>
  );
}

function groupSessions(
  sessions: ChatSession[],
  labels: {
    groupPinned: string;
    groupToday: string;
    groupYesterday: string;
    groupThisWeek: string;
    groupEarlier: string;
  },
): SessionGroup[] {
  const today = startOfToday();
  const yesterday = today - 24 * 60 * 60 * 1000;
  const sevenDaysAgo = today - 7 * 24 * 60 * 60 * 1000;

  const buckets: Record<SessionGroup["key"], ChatSession[]> = {
    pinned: [],
    today: [],
    yesterday: [],
    thisWeek: [],
    earlier: [],
  };

  const sorted = [...sessions].sort(
    (a, b) => compareSessionRecency(b) - compareSessionRecency(a),
  );

  for (const s of sorted) {
    if (s.pinned_at) {
      buckets.pinned.push(s);
      continue;
    }
    const ts = compareSessionRecency(s);
    if (ts >= today) buckets.today.push(s);
    else if (ts >= yesterday) buckets.yesterday.push(s);
    else if (ts >= sevenDaysAgo) buckets.thisWeek.push(s);
    else buckets.earlier.push(s);
  }

  return [
    { key: "pinned", label: labels.groupPinned, sessions: buckets.pinned },
    { key: "today", label: labels.groupToday, sessions: buckets.today },
    {
      key: "yesterday",
      label: labels.groupYesterday,
      sessions: buckets.yesterday,
    },
    {
      key: "thisWeek",
      label: labels.groupThisWeek,
      sessions: buckets.thisWeek,
    },
    { key: "earlier", label: labels.groupEarlier, sessions: buckets.earlier },
  ];
}

function compareSessionRecency(s: ChatSession): number {
  const ts = s.last_message_at ?? s.updated_at ?? s.created_at;
  const parsed = ts ? Date.parse(ts) : NaN;
  return Number.isFinite(parsed) ? parsed : 0;
}

function startOfToday(): number {
  const d = new Date();
  d.setHours(0, 0, 0, 0);
  return d.getTime();
}
