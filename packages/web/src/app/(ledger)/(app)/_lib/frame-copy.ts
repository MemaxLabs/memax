"use client";

import { useCallback } from "react";
import { useLedger } from "@memaxlabs/ledger";
import { count } from "@/lib/v2/copy";

/** `(n) => "1 waiting on you" | "5 waiting on you"` for a key pair. */
export function useCount(one: string, other: string) {
  return useCallback((n: number) => count(one, other, n), [one, other]);
}

/** An agent's display name from the Ledger registry ("cursor" → "Cursor"). */
export function useAgentName() {
  const { agents } = useLedger();
  return useCallback((key: string) => agents[key]?.name ?? key, [agents]);
}
