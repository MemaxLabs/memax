"use client";

import { useSyncExternalStore } from "react";

const ARRIVAL_HOLD_MS = 2400;

const listeners = new Set<() => void>();
const arrivedIds = new Set<string>();
const timers = new Map<string, ReturnType<typeof setTimeout>>();
const renderKeys = new Map<string, string>();
let snapshot = new Set<string>();

function emit() {
  snapshot = new Set(arrivedIds);
  listeners.forEach((listener) => listener());
}

export function markRecentArrival(id: string) {
  if (!id) return;
  if (!renderKeys.has(id)) {
    renderKeys.set(id, id);
  }
  arrivedIds.add(id);

  const existingTimer = timers.get(id);
  if (existingTimer) {
    clearTimeout(existingTimer);
  }

  timers.set(
    id,
    setTimeout(() => {
      arrivedIds.delete(id);
      timers.delete(id);
      emit();
    }, ARRIVAL_HOLD_MS),
  );

  emit();
}

export function transferRecentArrival(fromId: string, toId: string) {
  if (!fromId || !toId || fromId === toId) return;
  const existingTimer = timers.get(fromId);
  const renderKey = renderKeys.get(fromId) ?? fromId;
  arrivedIds.delete(fromId);
  timers.delete(fromId);
  renderKeys.delete(fromId);

  if (existingTimer) {
    clearTimeout(existingTimer);
  }

  renderKeys.set(toId, renderKey);
  markRecentArrival(toId);
}

export function getRecentRenderKey(id: string) {
  return renderKeys.get(id) ?? id;
}

function subscribe(listener: () => void) {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

function getSnapshot() {
  return snapshot;
}

export function useRecentArrivalIds() {
  return useSyncExternalStore(subscribe, getSnapshot, getSnapshot);
}
