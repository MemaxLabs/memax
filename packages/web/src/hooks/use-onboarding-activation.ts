"use client";

import { useEffect, useMemo } from "react";
import type { ChecklistPayload } from "memax-sdk";
import { useNotifications } from "@/hooks/use-notifications";
import { setLandingSurfaceHint } from "@/lib/landing-surface";

/**
 * useOnboardingActivation — shared "is this user past onboarding?" hook.
 *
 * Source of truth is the pending onboarding checklist row's item
 * completion state (server materializer at
 * internal/onboarding/materialize.go derives item completion from
 * owner-wide CountMemoriesByOwnerExcludingSeeds + connected_agents
 * excluding memax). Reading from the checklist guarantees consumers
 * see the same activation signal the server writes — no off-by-one
 * from hub-scoped useMemories() + useConnectedAgents() re-derivation.
 *
 * Activation = both `five_memories` AND `connect_agent` items have
 * `completed_at` set on the latest pending onboarding checklist. No
 * pending onboarding row (past-onboarding user) → activated. During
 * notifications query loading → not activated (safer transient state
 * for any consumer that branches UI on this — a new user briefly
 * seeing onboarding-flavored UI is benign; a veteran briefly seeing
 * it is the lesser surprise).
 *
 * Used by:
 *   - <ChatEmptyState>     — branches the quick-prompt chip set
 *   - /brain page         — redirects non-activated users to
 *                            /h/personal/memories so they land on
 *                            the surface where seed memories and the
 *                            pinned checklist live (industry pattern:
 *                            never show a useful UI surface to a user
 *                            who can't engage with it yet).
 */
export function useOnboardingActivation(): {
  isActivated: boolean;
  isLoading: boolean;
  /**
   * True when the checklist query errored — the activation signal is
   * UNKNOWN, not "activated". Without this distinction a transient
   * 500/offline made "no onboarding row found" indistinguishable from
   * "past onboarding", flipping isActivated to true and persisting a
   * wrong "brain" landing hint for a brand-new user.
   */
  isUnknown: boolean;
} {
  const notifications = useNotifications({
    kind: ["checklist"],
    status: "pending",
  });

  const isUnknown = notifications.isError && !notifications.data;

  const isActivated = useMemo(() => {
    if (isUnknown) return false;
    const rows = notifications.data?.notifications ?? [];
    const onboarding = rows.find(
      (n) =>
        n.kind === "checklist" &&
        (n as { source_kind?: string }).source_kind === "onboarding",
    );
    if (!onboarding) {
      if (notifications.isLoading || notifications.isPending) return false;
      return true;
    }
    const payload = (onboarding.payload ?? {}) as Partial<ChecklistPayload>;
    const items = payload.items ?? [];
    const fiveMemoriesDone = items.some(
      (it) => it.id === "five_memories" && Boolean(it.completed_at),
    );
    const connectAgentDone = items.some(
      (it) => it.id === "connect_agent" && Boolean(it.completed_at),
    );
    return fiveMemoriesDone && connectAgentDone;
  }, [
    isUnknown,
    notifications.data,
    notifications.isLoading,
    notifications.isPending,
  ]);

  const isLoading = notifications.isLoading || notifications.isPending;

  // Keep the landing-surface hint fresh wherever this hook is mounted
  // (brain page, chat empty state, /home resolver, app shell). The
  // hint lets the /home entry resolver route in a single hop on the
  // next cold entry without waiting for this query. Idempotent write —
  // and NEVER on an errored query: persisting a guess would poison
  // every subsequent cold entry until a successful fetch overwrote it.
  useEffect(() => {
    if (isLoading || isUnknown) return;
    setLandingSurfaceHint(isActivated ? "brain" : "memories");
  }, [isActivated, isLoading, isUnknown]);

  return {
    isActivated,
    isLoading,
    isUnknown,
  };
}
