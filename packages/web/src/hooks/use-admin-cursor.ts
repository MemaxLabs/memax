"use client";

import { useCallback, useState } from "react";

/**
 * useAdminCursor — shared keyset pagination state for admin lists.
 *
 * Every admin list endpoint (`users`, `waitlist`, `hubs`, hub members)
 * follows the same server contract: request with an optional `cursor`,
 * response carries `{ items, next_cursor, total }`. This hook captures
 * the client-side bookkeeping so each page doesn't reinvent the stack
 * of "previous cursors" needed to walk backwards.
 *
 *   const { cursor, hasPrev, goForward, goBack, reset } = useAdminCursor();
 *   const { data } = useAdminUsers({ cursor, q: searchQuery });
 *   // ...
 *   <AdminListFooter
 *     total={data?.total ?? 0}
 *     hasPrev={hasPrev}
 *     hasNext={!!data?.next_cursor}
 *     onPrev={goBack}
 *     onNext={() => goForward(data!.next_cursor)}
 *   />
 *
 * `reset()` is the escape hatch when a filter changes — callers should
 * invoke it from their search / filter onChange so the cursor doesn't
 * point to a row that no longer matches the new predicate.
 */
export function useAdminCursor(): {
  cursor: string | undefined;
  hasPrev: boolean;
  goForward: (nextCursor: string | undefined) => void;
  goBack: () => void;
  reset: () => void;
} {
  const [cursor, setCursor] = useState<string | undefined>(undefined);
  // history is a stack of cursors the user has navigated through.
  // goBack pops the most recent one; reset clears the stack.
  // Storing the empty string for "start of list" keeps the Prev button
  // working after a single Next click (goBack returns the user to
  // cursor=undefined, which is page 1).
  const [history, setHistory] = useState<string[]>([]);

  const goForward = useCallback(
    (nextCursor: string | undefined) => {
      if (!nextCursor) return;
      setHistory((h) => [...h, cursor ?? ""]);
      setCursor(nextCursor);
    },
    [cursor],
  );

  const goBack = useCallback(() => {
    setHistory((h) => {
      if (h.length === 0) return h;
      const target = h[h.length - 1];
      setCursor(target === "" ? undefined : target);
      return h.slice(0, -1);
    });
  }, []);

  const reset = useCallback(() => {
    setCursor(undefined);
    setHistory([]);
  }, []);

  return {
    cursor,
    hasPrev: history.length > 0,
    goForward,
    goBack,
    reset,
  };
}
