"use client";

import { useSyncExternalStore } from "react";

/**
 * Quick-start dialog open state — a tiny external store so any surface
 * (memories hero launcher, notification drawer, 入门与机制, first-run
 * auto-open) can open the same dialog without prop drilling.
 */
export type QuickStartStep =
  | "welcome"
  | "connect_agent"
  | "first_memory"
  | "first_ask"
  | "first_dream"
  | "first_hub_invite"
  | "use_cases";

interface QuickStartState {
  open: boolean;
  step: QuickStartStep | null;
}

let state: QuickStartState = { open: false, step: null };
const listeners = new Set<() => void>();

function emit() {
  listeners.forEach((l) => l());
}

export function openQuickStart(step?: QuickStartStep) {
  state = { open: true, step: step ?? null };
  emit();
}

export function closeQuickStart() {
  if (!state.open) return;
  state = { open: false, step: null };
  emit();
}

function subscribe(l: () => void) {
  listeners.add(l);
  return () => listeners.delete(l);
}

const getSnapshot = () => state;
const serverSnapshot: QuickStartState = { open: false, step: null };

export function useQuickStartState(): QuickStartState {
  return useSyncExternalStore(subscribe, getSnapshot, () => serverSnapshot);
}
