"use client";

/**
 * ChatSessionsSidebar — session-list content mounted inside the
 * chat surface's secondary panel.
 *
 * Group order (top → bottom):
 *   1. Pinned (signature pin icon, ordered by recency)
 *   2. Today / Yesterday / This week / Earlier (active sessions)
 *   3. Archived (collapsed by default; lazy-fetches when expanded)
 *
 * Pin and archive both flow through usePatchChatSession — this
 * component just presents the visual grouping. The ⋯ menu on each
 * row provides the pin/archive/delete actions; archive recovery
 * lives at the bottom as an expandable footer so users always
 * know where to find archived chats.
 */

import { useState } from "react";
import { Archive, ChevronDown, MessageSquarePlus, Pin } from "lucide-react";
import type { ChatSession } from "memax-sdk";
import { cn } from "@memaxlabs/ui";
import { useChatSessions } from "@/hooks/use-chat";
import { useLocale } from "@/i18n";
import { ChatSessionRowMenu } from "./chat-session-row-menu";

export interface ChatSessionsSidebarProps {
  /** Active (non-archived) sessions — provided by parent. */
  sessions: ChatSession[];
  activeSessionId?: string;
  onSelect: (sessionId: string) => void;
  onCreate: () => void;
  loading?: boolean;
  className?: string;
}

interface SessionGroup {
  key: "pinned" | "today" | "yesterday" | "thisWeek" | "earlier";
  label: string;
  sessions: ChatSession[];
}

export function ChatSessionsSidebar({
  sessions,
  activeSessionId,
  onSelect,
  onCreate,
  loading,
  className,
}: ChatSessionsSidebarProps) {
  const { t } = useLocale();
  const groups = groupSessions(sessions, t.chat.sidebar);

  return (
    <aside
      className={cn(
        "flex h-full w-72 shrink-0 flex-col border-r border-border/30 bg-surface-1/40",
        className,
      )}
      aria-label={t.chat.sidebar.heading}
    >
      <div className="flex h-12 shrink-0 items-center justify-between gap-2 px-3">
        <h2 className="min-w-0 truncate text-[13px] font-medium text-fg-2">
          {t.chat.sidebar.heading}
        </h2>
        <button
          type="button"
          onClick={onCreate}
          className="flex h-8 w-8 shrink-0 cursor-pointer items-center justify-center rounded-md text-fg-3 transition-colors hover:bg-foreground/6 hover:text-fg-2"
          aria-label={t.chat.newChat}
          title={t.chat.newChat}
        >
          <MessageSquarePlus className="h-4 w-4" aria-hidden />
        </button>
      </div>

      <div className="flex-1 overflow-y-auto overscroll-contain px-3 pb-20 pt-2">
        {loading && sessions.length === 0 ? (
          <SidebarSkeleton />
        ) : sessions.length === 0 ? (
          <div className="px-3 py-8 text-center text-[12px] text-fg-3">
            {t.chat.sidebar.noSessions}
          </div>
        ) : (
          <div className="flex flex-col gap-4">
            {groups
              .filter((g) => g.sessions.length > 0)
              .map((g) => (
                <SidebarGroup
                  key={g.key}
                  label={g.label}
                  sessions={g.sessions}
                  activeSessionId={activeSessionId}
                  onSelect={onSelect}
                  showPin={g.key === "pinned"}
                />
              ))}
          </div>
        )}
        <ArchivedAccordion
          activeSessionId={activeSessionId}
          onSelect={onSelect}
        />
      </div>
    </aside>
  );
}

function SidebarGroup({
  label,
  sessions,
  activeSessionId,
  onSelect,
  showPin,
}: {
  label: string;
  sessions: ChatSession[];
  activeSessionId?: string;
  onSelect: (id: string) => void;
  showPin?: boolean;
}) {
  const { t } = useLocale();

  return (
    <div>
      <div className="mb-1 px-2 text-[11px] font-medium text-fg-3">{label}</div>
      <ul className="flex flex-col gap-0.5">
        {sessions.map((s) => {
          const active = activeSessionId === s.id;
          return (
            <li key={s.id} className="relative">
              <div
                className={cn(
                  "group flex h-8 w-full items-center rounded-lg pl-2 pr-1 transition-colors",
                  active
                    ? "bg-surface-3 text-fg-1"
                    : "text-fg-2 hover:bg-surface-2",
                )}
                data-active={active}
              >
                <button
                  type="button"
                  onClick={() => onSelect(s.id)}
                  className="flex min-w-0 flex-1 cursor-pointer items-center gap-1.5 text-left text-[13px]"
                >
                  {showPin || s.pinned_at ? (
                    <Pin
                      className="h-3 w-3 shrink-0 text-signature"
                      aria-hidden
                      strokeWidth={2.5}
                    />
                  ) : null}
                  <span className="line-clamp-1">
                    {s.title || t.chat.sidebar.untitled}
                  </span>
                </button>
                <ChatSessionRowMenu session={s} variant="desktop" />
              </div>
            </li>
          );
        })}
      </ul>
    </div>
  );
}

// ── Archived accordion ─────────────────────────────────────────

function ArchivedAccordion({
  activeSessionId,
  onSelect,
}: {
  activeSessionId?: string;
  onSelect: (id: string) => void;
}) {
  const { t } = useLocale();
  const [expanded, setExpanded] = useState(false);
  // Lazy fetch — only run the archived query when the section is
  // expanded, so the default sidebar load stays cheap.
  const archivedQuery = useChatSessions(
    expanded ? { archived: true } : undefined,
  );
  // Same defensive filter as the active list in ChatBrainView — only
  // sessions with `archived_at` set show in the archived accordion,
  // regardless of whether the server's `?archived=true` filter held.
  const archived = expanded
    ? (archivedQuery.data?.sessions ?? []).filter((s) => Boolean(s.archived_at))
    : [];

  return (
    <div className="mt-4 border-t border-border/30 pt-3">
      <button
        type="button"
        onClick={() => setExpanded((v) => !v)}
        className="flex h-7 w-full items-center gap-1.5 rounded-md px-2 text-[12px] text-fg-3 transition-colors hover:bg-surface-2 hover:text-fg-2"
        aria-expanded={expanded}
      >
        <Archive className="h-3.5 w-3.5 shrink-0" aria-hidden />
        <span className="flex-1 text-left">
          {expanded ? t.chat.sidebar.hideArchived : t.chat.sidebar.showArchived}
        </span>
        <ChevronDown
          className={cn(
            "h-3 w-3 shrink-0 transition-transform",
            expanded ? "rotate-180" : "",
          )}
          aria-hidden
        />
      </button>
      {expanded && (
        <div className="mt-2">
          {archivedQuery.isLoading && archived.length === 0 ? (
            <SidebarSkeleton />
          ) : archived.length === 0 ? (
            <div className="px-3 py-4 text-center text-[12px] text-fg-3">
              {t.chat.sidebar.noArchived}
            </div>
          ) : (
            <SidebarGroup
              label={t.chat.sidebar.groupArchived}
              sessions={archived}
              activeSessionId={activeSessionId}
              onSelect={onSelect}
            />
          )}
        </div>
      )}
    </div>
  );
}

function SidebarSkeleton() {
  return (
    <div className="flex flex-col gap-1.5 px-2 py-2">
      {Array.from({ length: 5 }).map((_, i) => (
        <div
          key={i}
          className="h-7 rounded-lg bg-surface-2/40 animate-pulse"
          style={{ width: `${70 + ((i * 13) % 25)}%` }}
        />
      ))}
    </div>
  );
}

// ── Grouping ──────────────────────────────────────────────────

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
