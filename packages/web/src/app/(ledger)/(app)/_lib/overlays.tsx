"use client";

import {
  createContext,
  use,
  useCallback,
  useMemo,
  useState,
  type ReactNode,
} from "react";
import type { CommandMode } from "@memaxlabs/ledger";

/**
 * The frame's layers: ⌘K (Ask or remember), the `?` sheet and Verify
 * (VerifyStale, V on a stale memory). Exactly one floats at a time
 * (design-system README, "Paper, not glass"), so opening one closes the
 * others.
 */
interface CommandState {
  open: boolean;
  mode: CommandMode;
  /** Text to start with, e.g. a question carried over from Ask. */
  query: string;
  /** Bumps on every open, so a fresh open resets the dialog. */
  session: number;
}

/** VerifyStale on one memory of the frame's space. */
interface VerifyState {
  ref: string;
  /** Bumps on every open, so a fresh open starts from the defaults. */
  session: number;
}

interface OverlayValue {
  command: CommandState;
  openCommand: (mode?: CommandMode, query?: string) => void;
  setCommandOpen: (open: boolean) => void;
  keysOpen: boolean;
  setKeysOpen: (open: boolean) => void;
  verify: VerifyState | null;
  openVerify: (ref: string) => void;
  closeVerify: () => void;
}

const OverlayContext = createContext<OverlayValue | null>(null);

export function OverlayProvider({ children }: { children: ReactNode }) {
  const [command, setCommand] = useState<CommandState>({
    open: false,
    mode: "ask",
    query: "",
    session: 0,
  });
  const [keysOpen, setKeysOpenState] = useState(false);
  const [verify, setVerify] = useState<VerifyState | null>(null);

  const openCommand = useCallback((mode: CommandMode = "ask", query = "") => {
    setKeysOpenState(false);
    setVerify(null);
    setCommand((prev) => ({
      open: true,
      mode,
      query,
      session: prev.session + 1,
    }));
  }, []);

  const setCommandOpen = useCallback((open: boolean) => {
    if (open) {
      setKeysOpenState(false);
      setVerify(null);
    }
    setCommand((prev) =>
      open
        ? {
            ...prev,
            open,
            session: prev.open ? prev.session : prev.session + 1,
          }
        : { ...prev, open },
    );
  }, []);

  const setKeysOpen = useCallback((open: boolean) => {
    if (open) {
      setCommand((prev) => ({ ...prev, open: false }));
      setVerify(null);
    }
    setKeysOpenState(open);
  }, []);

  const openVerify = useCallback((ref: string) => {
    setCommand((prev) => ({ ...prev, open: false }));
    setKeysOpenState(false);
    setVerify((prev) => ({ ref, session: (prev?.session ?? 0) + 1 }));
  }, []);

  const closeVerify = useCallback(() => setVerify(null), []);

  const value = useMemo(
    () => ({
      command,
      openCommand,
      setCommandOpen,
      keysOpen,
      setKeysOpen,
      verify,
      openVerify,
      closeVerify,
    }),
    [
      command,
      openCommand,
      setCommandOpen,
      keysOpen,
      setKeysOpen,
      verify,
      openVerify,
      closeVerify,
    ],
  );
  return <OverlayContext value={value}>{children}</OverlayContext>;
}

export function useOverlays(): OverlayValue {
  const value = use(OverlayContext);
  if (!value) throw new Error("useOverlays needs an OverlayProvider");
  return value;
}
