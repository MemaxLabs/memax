"use client";

import { useCallback, type ReactNode } from "react";
import { Toast } from "@base-ui/react/toast";
import { Button, StateMark, type MarkState } from "@memaxlabs/ledger";
import { useLocale } from "@/i18n";
import { useHotkey, useKeycap } from "@/lib/v2/keymap/react";
import styles from "./toasts.module.css";

// Toasts (plan §6.4, States2): bottom left, one at a time, with Undo
// where the command has an inverse. Base UI supplies the behaviour (a
// polite live region, pause on hover and focus, F6 to reach it, swipe
// to dismiss); the look is the States2 board's.

interface ToastData {
  /**
   * The state mark, only when the toast means one (Ledger's colours mean
   * something): kept for a Keep, proposed (ochre) when it waits on the
   * person, working when it waits on an agent, off for paused or revoked.
   * Leave it out for neutral news such as "Copied" or a level change.
   */
  state?: MarkState;
  /**
   * Undoes the toast's command (its button and ⌘Z while it shows). The
   * toast closes; the undo says what happened in a toast of its own, with
   * no mark (`(app)/_lib/undo.tsx`).
   */
  undo?: () => void;
  action?: { label: string; onClick: () => void };
}

export interface ShowToast extends ToastData {
  text: string;
}

const TIMEOUT_MS = 6000;
const UNDO_TIMEOUT_MS = 10_000;

export function ToastProvider({ children }: { children: ReactNode }) {
  return (
    <Toast.Provider limit={1} timeout={TIMEOUT_MS}>
      {children}
    </Toast.Provider>
  );
}

/** Shows one toast, replacing whatever was showing. */
export function useToast() {
  const manager = Toast.useToastManager();
  return useCallback(
    ({ text, ...data }: ShowToast) => {
      manager.add<ToastData>({
        title: text,
        data,
        timeout: data.undo ? UNDO_TIMEOUT_MS : TIMEOUT_MS,
      });
    },
    [manager],
  );
}

function ToastItem({ toast }: { toast: Toast.Root.ToastObject<ToastData> }) {
  const { t } = useLocale();
  const copy = t.ledger.app.toast;
  const manager = Toast.useToastManager();
  const undoKey = useKeycap("undo");
  const data = toast.data;

  const runUndo = useCallback(() => {
    if (!data?.undo) return;
    manager.close(toast.id);
    data.undo();
  }, [data, manager, toast.id]);

  // ⌘Z undoes the toast's command while it shows (registry: "undo").
  useHotkey("undo", () => runUndo(), {
    enabled: Boolean(data?.undo) && toast.transitionStatus !== "ending",
  });

  return (
    <Toast.Root toast={toast} className={styles.toast} swipeDirection="left">
      {data?.state ? <StateMark state={data.state} label={false} /> : null}
      <Toast.Title className={styles.text}>{toast.title}</Toast.Title>
      {data?.undo ? (
        <Button variant="quiet" size="sm" kbd={undoKey} onClick={runUndo}>
          {copy.undo}
        </Button>
      ) : null}
      {data?.action ? (
        <Button
          variant="secondary"
          size="sm"
          onClick={() => {
            data.action?.onClick();
            manager.close(toast.id);
          }}
        >
          {data.action.label}
        </Button>
      ) : null}
    </Toast.Root>
  );
}

export function ToastViewport() {
  const { t } = useLocale();
  const { toasts } = Toast.useToastManager();
  return (
    <Toast.Portal>
      <Toast.Viewport
        className={styles.viewport}
        aria-label={t.ledger.app.toast.region}
      >
        {toasts.map((toast) => (
          <ToastItem key={toast.id} toast={toast} />
        ))}
      </Toast.Viewport>
    </Toast.Portal>
  );
}
