"use client";

/**
 * ChatBrainView — the /brain experience. Phase F (2026-05-04)
 * splits the surface into two routes:
 *
 *   - `/brain`                     → sessions LIST.
 *       Mobile: full-page list (no sidebar sheet).
 *       Desktop: sidebar + empty/first-run pane.
 *   - `/brain/sessions/<id>`       → active SESSION.
 *       Mobile: thread + composer fullscreen, dock hidden.
 *       Desktop: sidebar + thread + composer.
 *
 * **Active session = URL.** This component reads `routeSessionId`
 * from the route page; component state never owns it. That kills
 * the deep-link race the prior `?s=` flow had — the route segment
 * is resolved synchronously on first render, so hook closures see
 * the right id immediately.
 *
 * **Send semantics**:
 *   - Composer only renders on session routes (we don't show one
 *     on the bare list).
 *   - "New chat" → create session → navigate to /brain/sessions/<newId>.
 *   - On an existing session, send uses the route id directly.
 *
 * **Layout invariant** — the chat surface OWNS the entire shell
 * `<main>` viewport. Desktop stays `h-dvh` inside the shell main;
 * mobile is fixed because the mobile shell main is body-flow with
 * top/bottom chrome offsets.
 *
 * **Mobile chrome integration** — we no longer render an in-page
 * Menu button or a custom sidebar Sheet. The shell-v2 MobileTopBar
 * handles the leading icon (Menu on /brain, Back on
 * /brain/sessions/<id>); mobile-dock hides itself on session
 * routes (route-helpers.isChatSessionRoute drives both).
 */

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useRouter } from "next/navigation";
import type {
  ChatMessage,
  ChatToolDescriptor,
  CreateChatSessionInput,
  ListChatMessagesResult,
} from "memax-sdk";
import { cn } from "@memaxlabs/ui";
import { useBar } from "@/contexts/bar-context";
import {
  messagesListKey,
  useCancelChatMessage,
  useChatSessions,
  useChatTools,
  useCreateChatSession,
  useSendChatMessage,
} from "@/hooks/use-chat";
import { queryClient } from "@/lib/query-client";
import { useAuth } from "@/lib/auth";
import { useDevMode } from "@/hooks/use-dev-mode";
import { useLocale } from "@/i18n";
import { useIsMobile } from "@/hooks/use-is-mobile";
import { ChatEmptyState } from "./chat-empty-state";
import { ChatSessionsList } from "./chat-sessions-list";
import { ChatThread, PendingThinkingRow } from "./chat-thread";
import { ChatComposer, type ChatComposerHandle } from "./chat-composer";
import { ChatPersonaPicker } from "./chat-persona-picker";

function newIdempotencyKey(): string {
  if (typeof crypto !== "undefined" && "randomUUID" in crypto) {
    return crypto.randomUUID();
  }
  return `idem-${Date.now()}-${Math.random().toString(36).slice(2, 11)}`;
}

/**
 * ChatPendingSendBridge — visual bridge between "user hit send" and
 * "thread route mounted". The empty-state surface awaits
 * createSession before navigating, which on cold cache leaves the
 * user staring at an unchanged empty-state composer for 100-500ms —
 * reads as "did my send work?". This bridge mounts the user's
 * outgoing bubble + the SAME PendingThinkingRow ChatThread uses,
 * inside a container shaped IDENTICALLY to ChatThread's
 * (mx-auto max-w-3xl flex-col gap-1 px-3 py-8 sm:px-6), so the
 * bridge → thread transition is a no-op visually — the user sees
 * the same shapes in the same positions; only the data source
 * changes underneath.
 *
 * User-bubble styling mirrors ChatMessageRow's user variant
 * (rounded-[22px], bg-surface-2, px-4 py-2.5, text-[15px]) to
 * within a pixel.
 */
function ChatPendingSendBridge({ text }: { text: string }) {
  return (
    <div className="mx-auto flex w-full max-w-3xl flex-col gap-1 px-3 py-8 sm:px-6">
      <div
        className="flex w-full gap-3 py-3 justify-end"
        data-message-role="user"
        aria-live="polite"
      >
        <div className="flex max-w-[min(680px,100%)] flex-col gap-1.5 items-end">
          <div className="text-[15px] leading-relaxed break-words max-w-[min(560px,82vw)] rounded-[22px] bg-surface-2 px-4 py-2.5 text-fg-1">
            <span className="whitespace-pre-wrap">{text}</span>
          </div>
        </div>
      </div>
      <PendingThinkingRow />
    </div>
  );
}

export interface ChatBrainViewProps {
  /**
   * Active session id from the URL (`/brain/sessions/<id>`). When
   * undefined we're on the bare `/brain` list route and the
   * surface decides what to render based on viewport (full-page
   * list on mobile, sidebar + empty/first-run on desktop).
   */
  routeSessionId?: string;
}

/**
 * Build an optimistic user-message stub that ChatThread can render
 * instantly while the real send round-trips. Most fields are backfilled
 * with safe defaults — server will overwrite them when invalidateQueries
 * fires after `sendMessage` resolves and the real list refetches.
 *
 * The id is prefixed `temp-…` so future code can distinguish optimistic
 * rows if needed (today nothing branches on it; the row is replaced
 * wholesale by invalidation).
 */
function makeOptimisticUserMessage(
  sessionId: string,
  content: string,
  authorUserId: string,
): ChatMessage {
  const now = new Date().toISOString();
  return {
    id: `temp-user-${
      typeof crypto !== "undefined" && "randomUUID" in crypto
        ? crypto.randomUUID()
        : `${Date.now()}-${Math.random().toString(36).slice(2, 11)}`
    }`,
    session_id: sessionId,
    sequence: Date.now(),
    role: "user",
    author_user_id: authorUserId,
    status: "completed",
    scope_hub_ids_used: [],
    content,
    toolset_version_used: 0,
    created_at: now,
  };
}

export function ChatBrainView({ routeSessionId }: ChatBrainViewProps = {}) {
  const router = useRouter();
  const { t } = useLocale();
  const isMobile = useIsMobile();
  const { user } = useAuth();

  const sessionsQuery = useChatSessions();
  // Defensive client filter: only surface sessions without `archived_at`
  // in the active list. The `?archived` query param + ArchivedAccordion's
  // `?archived=true` partition is supposed to be enforced server-side,
  // but a stale-deploy / cache cross-contamination scenario (codex review
  // surfaced this empirically) was leaking archived rows into the active
  // list. Filtering at the consumption boundary makes the partition
  // robust regardless of upstream behaviour.
  const sessions = useMemo(
    () => (sessionsQuery.data?.sessions ?? []).filter((s) => !s.archived_at),
    [sessionsQuery.data?.sessions],
  );
  const createSession = useCreateChatSession();
  const sendMessage = useSendChatMessage();
  const cancelMessage = useCancelChatMessage();
  const toolsQuery = useChatTools();
  const { barOverlayOpen } = useBar();

  // Active session id is route-driven. We never write to it here;
  // navigation events update the URL and the prop flows back in.
  const activeSessionId = routeSessionId ?? null;

  const [composerValue, setComposerValue] = useState("");
  const [inflightAssistantId, setInflightAssistantId] = useState<string | null>(
    null,
  );
  const composerRef = useRef<ChatComposerHandle | null>(null);

  // Belt-and-suspenders focus handoff: when the bar overlay opens
  // (⌘K on /brain), blur the chat composer so the next keystroke
  // lands in the bar input. openBar() also blurs the active
  // element synchronously; this effect catches refs settling
  // afterward (route remount, etc).
  useEffect(() => {
    if (barOverlayOpen) {
      composerRef.current?.blur();
    }
  }, [barOverlayOpen]);

  const activeSession = useMemo(
    () => sessions.find((session) => session.id === activeSessionId) ?? null,
    [activeSessionId, sessions],
  );

  // Shared "send" path. Decoupled from the React event handlers so
  // we can call it from both the composer (with current
  // activeSessionId) and from suggestion taps (which need to
  // create-then-navigate-then-send atomically).
  const sendInSession = useCallback(
    async (sessionId: string, content: string) => {
      const trimmed = content.trim();
      if (!trimmed) return;
      await sendMessage.mutateAsync({
        sessionId,
        input: {
          content: trimmed,
          idempotencyKey: newIdempotencyKey(),
        },
      });
    },
    [sendMessage],
  );

  // Pending-send bridge state. When a user fires a send from the
  // empty state (no activeSessionId yet), createSession is awaited
  // before the route changes — that 100-500ms window otherwise
  // shows the user an empty composer in the unchanged empty state,
  // which reads as "did the send work?". Setting pendingSendText
  // immediately swaps the empty-state into a bridge view (user's
  // bubble + thinking dots) so the surface feels instant. Clears
  // automatically when activeSessionId catches up via the
  // route push, or on send error so the user can retype.
  const [pendingSendText, setPendingSendText] = useState<string | null>(null);
  useEffect(() => {
    if (activeSessionId) setPendingSendText(null);
  }, [activeSessionId]);

  const ensureSessionAndSend = useCallback(
    async (content: string) => {
      const trimmed = content.trim();
      if (!trimmed) return;
      // Concurrency guard — prevent a double-fire while a prior
      // send is in flight (codex review pass 2 medium). Without
      // this, rapid-tapping Send appends two optimistic rows AND
      // sends two mutations; the backend rejects the second with
      // ErrChatSessionLocked, leaving an orphaned optimistic row.
      // The composer also doesn't disable from inflightAssistantId
      // until the real assistant row lands via invalidate, so
      // gating at the JS layer is the reliable choice.
      if (sendMessage.isPending || createSession.isPending) return;
      // Optimistic clear: empty composer immediately so the user
      // sees their text submit. If the send fails the toast bridge
      // surfaces the error; users can retype if needed.
      setComposerValue("");
      composerRef.current?.clear();
      // Bridge view: render user's text + thinking dots while
      // createSession runs. Only meaningful when no session is
      // active yet (existing-session sends seed the thread cache
      // directly and the thread already shows the optimistic row).
      if (!activeSessionId) {
        setPendingSendText(trimmed);
      }
      // Track the optimistic id so we can roll it back on failure
      // (the alternative — leaving it in the cache — keeps the
      // PendingThinkingRow rendering forever because the last
      // message stays role="user"). Codex review pass 2 medium.
      let optimisticId: string | null = null;
      let seededSessionId: string | null = null;
      try {
        let sessionId = activeSessionId;
        if (!sessionId) {
          const input: CreateChatSessionInput = {
            scopeType: "user_all_hubs",
          };
          const session = await createSession.mutateAsync(input);
          sessionId = session.id;
        }

        // Seed the messages cache with an optimistic user-message stub
        // BEFORE we navigate (or before the existing-session send
        // round-trips). This is the issue 2 fix — without it, the
        // thread mounted on a fresh session shows the spinner during
        // the create-then-send latency, and an existing-session send
        // shows nothing in the thread until invalidateQueries fires.
        // Industry pattern (ChatGPT / Claude): the user's text is the
        // chat turn the moment they hit send. ChatThread's thinking
        // indicator (last message=user + no inflight assistant)
        // covers the gap until the real assistant row arrives via
        // invalidate. The optimistic row is replaced when invalidate
        // refetches.
        if (user) {
          const optimistic = makeOptimisticUserMessage(
            sessionId,
            trimmed,
            user.id,
          );
          optimisticId = optimistic.id;
          seededSessionId = sessionId;
          queryClient.setQueryData<ListChatMessagesResult>(
            messagesListKey(sessionId, undefined),
            (prev) => ({
              messages: [...(prev?.messages ?? []), optimistic],
              next_cursor: prev?.next_cursor ?? "",
              has_more: prev?.has_more ?? false,
            }),
          );
        }

        if (activeSessionId !== sessionId) {
          // Push the new session URL AFTER seeding the cache so the
          // ChatThread mount on the new route reads the optimistic
          // message immediately (no spinner gap).
          router.push(`/brain/sessions/${sessionId}`);
        }
        await sendInSession(sessionId, trimmed);
      } catch {
        // Roll back the optimistic row so the failure is visible
        // (composer restored, no orphan thinking indicator).
        if (optimisticId && seededSessionId) {
          queryClient.setQueryData<ListChatMessagesResult>(
            messagesListKey(seededSessionId, undefined),
            (prev) =>
              prev
                ? {
                    ...prev,
                    messages: prev.messages.filter(
                      (m) => m.id !== optimisticId,
                    ),
                  }
                : prev,
          );
        }
        // Mutation toast handles user copy via meta.errorMessage.
        // Restore the composer text so the user doesn't lose what
        // they typed on a failed send.
        setComposerValue(trimmed);
        // Clear the bridge so the empty state returns; the user
        // sees their text restored in the composer rather than a
        // stuck "sending" row.
        setPendingSendText(null);
      }
    },
    [activeSessionId, createSession, router, sendInSession, sendMessage, user],
  );

  const onSendFromComposer = useCallback(() => {
    void ensureSessionAndSend(composerValue);
  }, [composerValue, ensureSessionAndSend]);

  const onPromptSuggestion = useCallback(
    (prompt: string) => {
      void ensureSessionAndSend(prompt);
    },
    [ensureSessionAndSend],
  );

  const onCancel = useCallback(() => {
    if (!inflightAssistantId || !activeSessionId) return;
    cancelMessage.mutate({
      sessionId: activeSessionId,
      messageId: inflightAssistantId,
    });
  }, [activeSessionId, cancelMessage, inflightAssistantId]);

  const onSelectSession = useCallback(
    (sessionId: string) => {
      router.push(`/brain/sessions/${sessionId}`);
    },
    [router],
  );

  // Draft mode: industry pattern (ChatGPT / Claude / Cursor /
  // Perplexity) — tapping "New chat" doesn't persist a session row.
  // The user gets a focused composer; the server only inserts a
  // sessions row when the first message is sent. Without this, every
  // accidental "New chat" tap creates an "Untitled chat" turd in the
  // sidebar.
  //
  // Lifetime: local state, resets automatically once activeSessionId
  // is set (the post-send navigate). Browser-back from draft mode
  // returns to whatever was before (the list, if they came from
  // there); mobile hamburger is the explicit escape.
  const [mobileDraftMode, setMobileDraftMode] = useState(false);
  useEffect(() => {
    if (activeSessionId) setMobileDraftMode(false);
  }, [activeSessionId]);

  // ── Mobile: full-page sessions list (when no active session and
  //    not currently drafting a new chat) ─
  if (isMobile && !activeSessionId && !mobileDraftMode) {
    return (
      <ChatSessionsList
        sessions={sessions}
        loading={sessionsQuery.isLoading}
        onSelect={onSelectSession}
        onCreate={() => {
          // No createSession call here — the session row is born on
          // first send (ensureSessionAndSend below). This handler
          // flips draft mode so the empty composer mounts; the user
          // types, sends, and the session lands at that point. An
          // unsent draft never touches the DB.
          setMobileDraftMode(true);
        }}
      />
    );
  }

  const showEmpty = !activeSessionId;

  // Layout invariant: chat owns one viewport-height pane and scrolls
  // internally. Desktop stays in normal flow (`h-dvh`) inside the
  // shell main; the shell mounts the sessions sidebar via
  // `<PinnedSecondaryPanel><BrainSessionsMount /></...>` and owns
  // the left-chrome offset on `<motion.main>` — this view is
  // unaware of rail/panel geometry. Mobile remains fixed because
  // the mobile shell main is body-flow with chrome offsets; on
  // session routes the dock is hidden via isChatSessionRoute so
  // the chat extends to the viewport bottom.
  return (
    <div
      className={cn(
        "flex overflow-hidden",
        isMobile ? "fixed left-0 right-0 z-shell-bar" : "relative h-dvh w-full",
      )}
      style={
        isMobile
          ? {
              top: "calc(3.5rem + env(safe-area-inset-top))",
              bottom: "env(safe-area-inset-bottom)",
            }
          : undefined
      }
    >
      <main className="flex h-full min-w-0 flex-1 flex-col">
        {showEmpty && !pendingSendText ? (
          // Centered empty state — first-run surface, no in-flight
          // send yet. Composer lives INSIDE the centered container
          // so the empty-state intro + composer read as one block.
          <div className="flex min-h-0 flex-1 items-center justify-center overflow-y-auto px-4 py-8">
            <div className="mx-auto flex w-full max-w-[640px] flex-col items-center gap-6">
              <ChatEmptyState onPrompt={onPromptSuggestion} />
              <div className="w-full">
                <ChatCapabilitiesStrip
                  tools={toolsQuery.data ?? []}
                  enabledToolNames={undefined}
                />
                <ChatComposer
                  value={composerValue}
                  onChange={setComposerValue}
                  onSend={onSendFromComposer}
                  inflightAssistantId={inflightAssistantId}
                  onCancel={onCancel}
                  composerRef={composerRef}
                />
              </div>
            </div>
          </div>
        ) : showEmpty && pendingSendText ? (
          // Bridge view — user sent but createSession still in flight.
          // Adopt the EXACT layout the real ChatThread uses
          // (top-aligned scrollable area, composer in the footer
          // band) so the bridge → thread transition is a no-op
          // visually: same container shape, same row primitives,
          // same composer position. Without this, the row "jumps"
          // from centered-empty-state to top-thread when the
          // route lands.
          <div className="min-h-0 flex-1 overflow-y-auto [scrollbar-gutter:stable]">
            <ChatPendingSendBridge text={pendingSendText} />
          </div>
        ) : (
          <ChatThread
            sessionId={activeSessionId!}
            onInflightAssistantChange={setInflightAssistantId}
          />
        )}
        {(!showEmpty || pendingSendText) && (
          <div className="shrink-0 px-3 pb-4 pt-2 sm:px-6 sm:pb-6">
            <div className="mx-auto max-w-[640px]">
              <ChatCapabilitiesStrip
                tools={toolsQuery.data ?? []}
                enabledToolNames={activeSession?.tools}
              />
              {activeSession && <ChatPersonaPicker session={activeSession} />}
              <ChatComposer
                value={composerValue}
                onChange={setComposerValue}
                onSend={onSendFromComposer}
                inflightAssistantId={inflightAssistantId}
                onCancel={onCancel}
                composerRef={composerRef}
              />
            </div>
          </div>
        )}
      </main>
    </div>
  );
}

function ChatCapabilitiesStrip({
  tools,
  enabledToolNames,
}: {
  tools: ChatToolDescriptor[];
  enabledToolNames?: string[];
}) {
  const { t } = useLocale();
  const { isDevUser, flags } = useDevMode();
  const fallbackEnabled = useMemo(
    () =>
      tools
        .filter((tool) => tool.available && tool.default_in_chat)
        .map((tool) => tool.name),
    [tools],
  );
  const enabledSet = useMemo(
    () => new Set(enabledToolNames ?? fallbackEnabled),
    [enabledToolNames, fallbackEnabled],
  );
  const labels = t.chat.capabilities.tools as Record<string, string>;
  const enabledTools = useMemo(
    () =>
      tools.filter(
        (tool) =>
          tool.available &&
          enabledSet.has(tool.name) &&
          Boolean(labels[tool.name]),
      ),
    [enabledSet, labels, tools],
  );

  // Capabilities chips are a debug surface — the per-tool catalog
  // ("Recall, List, Read, Save asks first, …") is operator detail,
  // not something a user picks to use. Gated behind dev-org
  // membership AND an explicit Settings → Dev toggle so dev users
  // who don't currently care about the tool plumbing don't see it
  // either. Default off.
  if (!isDevUser) return null;
  if (!flags.showChatCapabilities) return null;
  if (tools.length === 0) return null;

  return (
    <div className="mb-2 flex min-h-7 items-center gap-2 overflow-x-auto px-1 text-[11px] text-fg-3">
      <span className="shrink-0 font-medium">{t.chat.capabilities.label}</span>
      <div className="flex min-w-0 items-center gap-1.5">
        {enabledTools.length === 0 ? (
          <span className="rounded-full bg-surface-1 px-2 py-1 text-fg-3">
            {t.chat.capabilities.empty}
          </span>
        ) : (
          enabledTools.map((tool) => (
            <span
              key={tool.name}
              className={cn(
                "inline-flex shrink-0 items-center gap-1 rounded-full bg-surface-1 px-2 py-1 text-fg-2",
                tool.approval_required && "text-fg-1",
              )}
            >
              {labels[tool.name]}
              {tool.approval_required && (
                <span className="text-fg-3">
                  {t.chat.capabilities.approval}
                </span>
              )}
            </span>
          ))
        )}
      </div>
    </div>
  );
}
