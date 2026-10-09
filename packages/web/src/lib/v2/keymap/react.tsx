"use client";

import {
  createContext,
  use,
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
  useSyncExternalStore,
  type ReactNode,
} from "react";
import {
  ariaKeyshortcuts,
  detectPlatform,
  keycaps,
  type Platform,
} from "./keys";
import {
  createKeymap,
  type KeyHandler,
  type Keymap,
  type KeyScope,
} from "./keymap";
import { bindingFor, KEYMAP, type KeyActionId } from "./registry";

// React bindings for the keymap: one provider with the document
// listener, <KeyScope> for pages and layers, useHotkey for handlers,
// and the platform's keycaps for display.

const KeymapContext = createContext<Keymap | null>(null);
const ScopeContext = createContext<KeyScope | null>(null);

function subscribePlatform() {
  return () => {};
}

/**
 * ⌘ or Ctrl. The server renders the Mac caps (what the boards draw);
 * the client switches after hydration without a mismatch.
 */
export function usePlatform(): Platform {
  return useSyncExternalStore(
    subscribePlatform,
    () => detectPlatform(navigator),
    () => "mac",
  );
}

export function KeymapProvider({ children }: { children: ReactNode }) {
  const platform = usePlatform();
  const [keymap] = useState(() => createKeymap({ bindings: KEYMAP }));
  keymap.setPlatform(platform);

  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      keymap.handle(event);
    };
    document.addEventListener("keydown", onKeyDown);
    return () => document.removeEventListener("keydown", onKeyDown);
  }, [keymap]);

  return (
    <KeymapContext value={keymap}>
      <ScopeContext value={keymap.root}>{children}</ScopeContext>
    </KeymapContext>
  );
}

/**
 * A key scope for a page or a layer. Bindings registered below it win
 * over older scopes; a `modal` scope (a dialog, a menu) silences every
 * scope below it while mounted.
 */
export function KeyScopeBoundary({
  name,
  modal = false,
  children,
}: {
  name: string;
  modal?: boolean;
  children: ReactNode;
}) {
  const keymap = use(KeymapContext);
  const [scope, setScope] = useState<KeyScope | null>(null);
  useEffect(() => {
    if (!keymap) return;
    const next = keymap.pushScope(name, { modal });
    setScope(next);
    return () => {
      next.dispose();
      setScope(null);
    };
  }, [keymap, name, modal]);
  return <ScopeContext value={scope}>{children}</ScopeContext>;
}

/**
 * Handles a binding from the registry while the component is mounted
 * (and `enabled`). The latest handler always runs, so it can close over
 * fresh state without re-registering.
 */
export function useHotkey(
  id: KeyActionId,
  handler: KeyHandler,
  { enabled = true }: { enabled?: boolean } = {},
) {
  const scope = use(ScopeContext);
  const latest = useRef(handler);
  // At commit, not after it: a key that arrives once the DOM shows a
  // render, before its passive effects run, gets that render's handler.
  useLayoutEffect(() => {
    latest.current = handler;
  });
  useEffect(() => {
    if (!scope || !enabled) return;
    return scope.register(id, (event, match) => latest.current(event, match));
  }, [scope, id, enabled]);
}

/** Display caps for a binding on this platform: one list per alternative. */
export function useKeycaps(id: KeyActionId): string[][] {
  const platform = usePlatform();
  return (bindingFor(id).keys ?? []).map((spec) => keycaps(spec, platform));
}

/** The first alternative as one keycap string ("⌘K", "Ctrl+K"), for Kbd and Button kbd. */
export function useKeycap(id: KeyActionId): string {
  const caps = useKeycaps(id);
  return caps[0]?.join(" ") ?? "";
}

/** aria-keyshortcuts for a binding's first alternative, when it's a single chord. */
export function useAriaKeys(id: KeyActionId): string | undefined {
  const platform = usePlatform();
  const spec = bindingFor(id).keys?.[0];
  return spec ? ariaKeyshortcuts(spec, platform) : undefined;
}
