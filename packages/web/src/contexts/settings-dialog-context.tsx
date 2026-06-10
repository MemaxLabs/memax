"use client";

import {
  createContext,
  useContext,
  useState,
  useCallback,
  useEffect,
} from "react";
import { acquireBodyScrollLock } from "@/lib/scroll-lock";

export type YouSection =
  | "profile"
  | "appearance"
  | "agents"
  | "api-keys"
  | "connected-accounts";

export type HubSection =
  | "general"
  | "appearance"
  | "intelligence"
  | "members"
  | "permissions"
  | "danger-zone";

export type SettingsScope =
  | { kind: "you"; section?: YouSection }
  | { kind: "hub"; hubId: string; section?: HubSection };

/** @deprecated Legacy tab type — use SettingsScope instead. */
type LegacyTab = "account" | "agents" | "intelligence" | "teams";

interface SettingsDialogContextValue {
  isOpen: boolean;
  defaultScope: SettingsScope | null;
  open: (scope?: SettingsScope | LegacyTab) => void;
  close: () => void;
}

/** Map old tab strings to new scope for backward compat during migration. */
function normalizeLegacy(
  input: SettingsScope | LegacyTab | undefined,
): SettingsScope | null {
  if (!input) return null;
  if (typeof input === "object") return input;
  switch (input) {
    case "account":
      return { kind: "you", section: "profile" };
    case "agents":
      return { kind: "you", section: "agents" };
    case "intelligence":
      // Intelligence moved from You to per-hub in the per-hub
      // intelligence release. Legacy callers land on the profile
      // page; the caller should switch scopes to the hub of
      // interest to actually see dream settings. Silent fallback
      // to `you/profile` rather than pinning to a specific hub —
      // we don't know which hub the legacy link was aimed at and
      // guessing activeHubId here would route callers to the
      // wrong hub in common multi-hub sessions.
      return { kind: "you", section: "profile" };
    case "teams":
      // Caller should pass hub-scoped scope; fallback to You for safety
      return { kind: "you", section: "profile" };
    default:
      return null;
  }
}

const SettingsDialogContext = createContext<SettingsDialogContextValue>({
  isOpen: false,
  defaultScope: null,
  open: () => {},
  close: () => {},
});

export function SettingsDialogProvider({
  children,
}: {
  children: React.ReactNode;
}) {
  const [isOpen, setIsOpen] = useState(false);
  const [defaultScope, setDefaultScope] = useState<SettingsScope | null>(null);
  const open = useCallback((scope?: SettingsScope | LegacyTab) => {
    setDefaultScope(normalizeLegacy(scope));
    setIsOpen(true);
  }, []);
  const close = useCallback(() => setIsOpen(false), []);

  // Lock body scroll when open — uses reference-counted lock
  // so nested overlays (e.g., hub create dialog) don't fight
  useEffect(() => {
    if (isOpen) {
      const release = acquireBodyScrollLock();
      return () => {
        release();
      };
    }
  }, [isOpen]);

  return (
    <SettingsDialogContext.Provider
      value={{ isOpen, defaultScope, open, close }}
    >
      {children}
    </SettingsDialogContext.Provider>
  );
}

export function useSettingsDialog() {
  return useContext(SettingsDialogContext);
}
