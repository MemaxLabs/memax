"use client";

export type MemaxDebugChannel = "recall" | "ai" | "remember" | "system";
export type MemaxDebugTone = "active" | "success" | "neutral" | "error";

export interface MemaxDebugEvent {
  id: string;
  at: number;
  channel: MemaxDebugChannel;
  tone: MemaxDebugTone;
  title: string;
  detail?: string;
  context?: string;
}

type MemaxDebugEventInput = Omit<MemaxDebugEvent, "id" | "at">;
type Listener = () => void;

const MAX_EVENTS = 150;

let events: MemaxDebugEvent[] = [];
const listeners = new Set<Listener>();

function emit() {
  listeners.forEach((listener) => listener());
}

function nextId() {
  if (typeof crypto === "object" && typeof crypto.randomUUID === "function") {
    return crypto.randomUUID();
  }
  return (
    "memax-debug-" + Date.now() + "-" + Math.random().toString(36).slice(2, 8)
  );
}

export function pushMemaxDebugEvent(event: MemaxDebugEventInput) {
  events = [{ ...event, id: nextId(), at: Date.now() }, ...events].slice(
    0,
    MAX_EVENTS,
  );
  emit();
}

export function clearMemaxDebugEvents() {
  events = [];
  emit();
}

export function getMemaxDebugEvents() {
  return events;
}

export function subscribeMemaxDebugEvents(listener: Listener) {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}
