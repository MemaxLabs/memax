"use client";

import { useCallback, type ReactNode } from "react";
import { Toast } from "@base-ui/react/toast";
import { Button, StateMark, type MarkState } from "@memaxlabs/ledger";
import { interpolate, useLocale } from "@/i18n";
import { useHotkey, useKeycap } from "@/lib/v2/keymap/react";
import styles from "./toasts.module.css";

// Toasts (plan §6.4, States2): bottom left, one at a time, with Undo
// where the command has an inverse. Base UI supplies the behaviour (a
// polite live region, pause on hover and focus, F6 to reach it, swipe
// to dismiss); the look is the States2 board's.

interface ToastData {
  state: MarkState;
  /** The display ID an Undo reverses, for the "Undone" line. */
  undoRef?: string;
  undo?: () => Promise<void>;
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

  const runUndo = useCallback(async () => {
    if (!data?.undo) return;
    manager.close(toast.id);
    const ref = data.undoRef ?? "";
    try {
      await data.undo();
      manager.add<ToastData>({
        title: interpolate(copy.undone, { ref }),
        data: { state: "off" },
      });
    } catch {
      manager.add<ToastData>({
        title: interpolate(copy.undoFailed, { ref }),
        data: { state: "proposed" },
      });
    }
  }, [data, manager, toast.id, copy]);

  // ⌘Z undoes the toast's command while it shows (registry: "undo").
  useHotkey("undo", () => void runUndo(), {
    enabled: Boolean(data?.undo) && toast.transitionStatus !== "ending",
  });

  return (
    <Toast.Root toast={toast} className={styles.toast} swipeDirection="left">
      <StateMark state={data?.state ?? "kept"} label={false} />
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
