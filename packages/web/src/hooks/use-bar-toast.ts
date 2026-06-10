"use client";

import { useCallback } from "react";
import { useBar } from "@/contexts/bar-context";
import type { BarNotification } from "@/lib/bar-types";

/** Convenience wrapper over setBarNotification for toast-like feedback. */
export function useBarToast() {
  const { setBarNotification } = useBar();

  const dismiss = useCallback(
    () => setBarNotification(null),
    [setBarNotification],
  );

  const show = useCallback(
    (n: BarNotification) =>
      setBarNotification({
        ...n,
        onDismiss: n.onDismiss ?? (() => setBarNotification(null)),
      }),
    [setBarNotification],
  );

  return {
    success: (message: string, opts?: Partial<BarNotification>) =>
      show({ type: "success", message, autoDismissMs: 4000, ...opts }),
    error: (message: string, opts?: Partial<BarNotification>) =>
      show({ type: "error", message, autoDismissMs: 5000, ...opts }),
    info: (message: string, opts?: Partial<BarNotification>) =>
      show({ type: "info", message, ...opts }),
    dismiss,
  };
}
