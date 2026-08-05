"use client";

/**
 * useChatDraft — per-session composer draft persistence.
 *
 * A half-typed message must survive navigating away and back (industry
 * baseline: ChatGPT, Claude.ai, Linear comments all keep drafts).
 * sessionStorage is the right scope: survives route changes and reloads
 * within the tab, but doesn't sync drafts across devices or linger for
 * weeks the way localStorage would.
 *
 * Keyed per session id ("new" for the empty state's not-yet-created
 * session) so switching sessions swaps drafts instead of leaking text
 * between conversations. Callers clear the value on successful send —
 * the setter writes through, so clearing state clears storage too.
 */

import { useCallback, useEffect, useState } from "react";

const draftKey = (sessionId: string | null | undefined): string =>
  `memax_chat_draft_${sessionId ?? "new"}`;

function readDraft(sessionId: string | null | undefined): string {
  if (typeof window === "undefined") return "";
  try {
    return window.sessionStorage.getItem(draftKey(sessionId)) ?? "";
  } catch {
    return ""; // storage disabled (private mode quirks) — degrade silently
  }
}

export function useChatDraft(
  sessionId: string | null | undefined,
): [string, (next: string) => void] {
  const [value, setValue] = useState<string>(() => readDraft(sessionId));

  // Session switch → load that session's draft (not the previous one's).
  useEffect(() => {
    setValue(readDraft(sessionId));
  }, [sessionId]);

  const setDraft = useCallback(
    (next: string) => {
      setValue(next);
      try {
        if (next === "") {
          window.sessionStorage.removeItem(draftKey(sessionId));
        } else {
          window.sessionStorage.setItem(draftKey(sessionId), next);
        }
      } catch {
        // Storage full/disabled — keep the in-memory value working.
      }
    },
    [sessionId],
  );

  return [value, setDraft];
}
