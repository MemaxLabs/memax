"use client";

import { useCallback, useState } from "react";

interface RunOptions {
  closeOnSuccess?: boolean;
  onSuccess?: () => void;
  onError?: (error: unknown) => void;
}

/**
 * Shared destructive-action controller.
 *
 * One action can be confirming or running at a time. Failure leaves the
 * confirmation mounted so the user stays in context and can retry or cancel.
 *
 * Failure is signalled two ways:
 *   1. The wrapped action throws — the catch branch handles it.
 *   2. The wrapped action resolves to `false` — an "expected failure"
 *      return value for actions that report success/failure via a
 *      boolean (e.g., hooks that surface partial-skip / permission
 *      denial / single-flight-reject as `false` without throwing).
 *
 * `void` / `undefined` / any truthy resolution is treated as success.
 */
export function useDestructiveAction<T extends string>() {
  const [confirmingKey, setConfirmingKey] = useState<T | null>(null);
  const [runningKey, setRunningKey] = useState<T | null>(null);

  const start = useCallback(
    (key: T) => {
      if (runningKey !== null) return;
      setConfirmingKey(key);
    },
    [runningKey],
  );

  const cancel = useCallback(
    (key?: T) => {
      setConfirmingKey((current) => {
        if (current === null) return null;
        if (runningKey !== null && Object.is(current, runningKey)) {
          return current;
        }
        if (key !== undefined && !Object.is(current, key)) {
          return current;
        }
        return null;
      });
    },
    [runningKey],
  );

  const reset = useCallback(() => {
    setConfirmingKey(null);
    setRunningKey(null);
  }, []);

  const isConfirming = useCallback(
    (key: T) => Object.is(confirmingKey, key),
    [confirmingKey],
  );

  const isRunning = useCallback(
    (key: T) => Object.is(runningKey, key),
    [runningKey],
  );

  const run = useCallback(
    async (
      key: T,
      action: () => Promise<boolean | void>,
      options?: RunOptions,
    ) => {
      if (runningKey !== null) return false;

      setRunningKey(key);
      try {
        const result = await action();
        // Explicit `false` — expected failure. Treat identically to the
        // catch branch: keep confirmingKey mounted, do not fire onSuccess,
        // return false. Callers that want the confirmation card to close
        // on failure can still throw instead. This branch exists so hooks
        // like useMemoryForget can signal full-skip / permission-denied /
        // single-flight-reject without raising synthetic exceptions.
        if (result === false) {
          return false;
        }
        options?.onSuccess?.();
        if (options?.closeOnSuccess !== false) {
          setConfirmingKey((current) =>
            Object.is(current, key) ? null : current,
          );
        }
        return true;
      } catch (error) {
        options?.onError?.(error);
        return false;
      } finally {
        setRunningKey((current) => (Object.is(current, key) ? null : current));
      }
    },
    [runningKey],
  );

  return {
    start,
    cancel,
    reset,
    run,
    isConfirming,
    isRunning,
    confirmingKey,
    runningKey,
  };
}
