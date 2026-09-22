"use client";

import { useSyncExternalStore } from "react";
import type { MechanismTab } from "@/components/features/onboarding-mechanism-modal";

/**
 * 入门与机制 modal open state — one external store so the left rail,
 * the mobile settings panel, the "?" key and the quick-start deck all
 * open the same modal on the tab they mean. `epoch` bumps on every
 * open so a second open while it is already up remounts it on the new
 * tab (initialTab is only read at mount).
 */
interface MechanismState {
  open: boolean;
  tab: MechanismTab;
  epoch: number;
}

let state: MechanismState = { open: false, tab: "quickstart", epoch: 0 };
const listeners = new Set<() => void>();
const emit = () => listeners.forEach((l) => l());

export function openMechanism(tab: MechanismTab = "quickstart") {
  state = { open: true, tab, epoch: state.epoch + 1 };
  emit();
}

export function closeMechanism() {
  if (!state.open) return;
  state = { ...state, open: false };
  emit();
}

function subscribe(l: () => void) {
  listeners.add(l);
  return () => listeners.delete(l);
}
const getSnapshot = () => state;
const serverSnapshot: MechanismState = {
  open: false,
  tab: "quickstart",
  epoch: 0,
};

export function useMechanismState(): MechanismState {
  return useSyncExternalStore(subscribe, getSnapshot, () => serverSnapshot);
}
