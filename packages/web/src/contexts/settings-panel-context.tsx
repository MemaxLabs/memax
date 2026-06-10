"use client";

/**
 * SettingsPanelContext — controls visibility of the quick-menu
 * `<SettingsPanel>` (theme, language, signout, app info) so multiple
 * trigger surfaces can open it without each owning a local useState.
 *
 * Existing triggers:
 *   - v1 floating top-right avatar button (app-shell-client.tsx)
 *   - v2 LeftRail footer (left-rail.tsx)
 *
 * Distinct from `SettingsDialogContext`, which controls the FULL
 * settings dialog (account, agents, billing). The two surfaces are
 * intentionally separate — SettingsPanel is the always-on quick menu;
 * SettingsDialog is the deep settings surface reachable from inside
 * the quick menu's "All settings" link.
 */

import {
  createContext,
  useCallback,
  useContext,
  useMemo,
  useState,
  type ReactNode,
} from "react";

interface SettingsPanelContextValue {
  open: boolean;
  setOpen: (next: boolean) => void;
  toggle: () => void;
  close: () => void;
}

const SettingsPanelContext = createContext<SettingsPanelContextValue | null>(
  null,
);

export function SettingsPanelProvider({ children }: { children: ReactNode }) {
  const [open, setOpenState] = useState(false);
  const setOpen = useCallback((next: boolean) => setOpenState(next), []);
  const toggle = useCallback(() => setOpenState((prev) => !prev), []);
  const close = useCallback(() => setOpenState(false), []);

  const value = useMemo<SettingsPanelContextValue>(
    () => ({ open, setOpen, toggle, close }),
    [open, setOpen, toggle, close],
  );

  return (
    <SettingsPanelContext.Provider value={value}>
      {children}
    </SettingsPanelContext.Provider>
  );
}

export function useSettingsPanel(): SettingsPanelContextValue {
  const value = useContext(SettingsPanelContext);
  if (!value) {
    throw new Error(
      "useSettingsPanel must be used inside <SettingsPanelProvider> — " +
        "mount it at the app shell root alongside the other UI providers.",
    );
  }
  return value;
}
