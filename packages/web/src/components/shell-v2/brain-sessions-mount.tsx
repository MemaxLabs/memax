"use client";

/**
 * BrainSessionsMount — wires `<ChatSessionsSidebar>` to the shell's
 * `<PinnedSecondaryPanel>` for the Brain tab. Mirrors the Memories
 * tab's `<TopicTreeMount>` so both tabs use the same secondary-panel
 * primitive (uniform open/close affordance, hover overlay behavior,
 * geometry).
 *
 * Active session id is read from the route segment (`/brain/sessions/<id>`)
 * — the URL is the source of truth for active-session identity, the
 * same pattern the chat surface uses internally. Selecting a session
 * navigates; "New chat" creates and navigates. The sidebar itself is
 * stateless beyond local UI state (per-row menu open, etc.).
 */

import { useCallback } from "react";
import { useRouter, usePathname } from "next/navigation";
import { ChatSessionsSidebar } from "@/components/features/chat/chat-sessions-sidebar";
import { useChatSessions } from "@/hooks/use-chat";

const SESSION_ROUTE_RE = /^\/brain\/sessions\/([^/]+)/;

export function BrainSessionsMount() {
  const router = useRouter();
  const pathname = usePathname();
  const sessionsQuery = useChatSessions();

  const activeSessionId = pathname?.match(SESSION_ROUTE_RE)?.[1] ?? undefined;

  const onSelect = useCallback(
    (sessionId: string) => {
      router.push(`/brain/sessions/${sessionId}`);
    },
    [router],
  );

  const onCreate = useCallback(() => {
    // Industry pattern (ChatGPT / Claude / Cursor): "New chat" does
    // NOT insert a sessions row. We just navigate to /brain — the
    // chat-brain-view renders the empty composer when there's no
    // activeSessionId, and `ensureSessionAndSend` creates the row
    // on first message send. Without this, every accidental tap
    // turned into an "Untitled chat" sidebar entry.
    router.push("/brain");
  }, [router]);

  return (
    <ChatSessionsSidebar
      sessions={sessionsQuery.data?.sessions ?? []}
      activeSessionId={activeSessionId}
      onSelect={onSelect}
      onCreate={onCreate}
      loading={sessionsQuery.isLoading}
      // PinnedSecondaryPanel owns the outer chrome (background,
      // border, header, scroll). Pass a transparent fill so the
      // sidebar visually merges with the panel material.
      className="w-full border-r-0 bg-transparent"
    />
  );
}
