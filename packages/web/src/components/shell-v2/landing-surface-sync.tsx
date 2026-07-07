"use client";

/**
 * Invisible always-mounted refresher for the landing-surface hint.
 *
 * useOnboardingActivation persists the hint whenever its checklist
 * query resolves — but the hook is otherwise mounted only on /brain,
 * the chat empty state, and the /home resolver (which unmounts before
 * the query settles on the hinted fast path). A user who ACTIVATES
 * while living on the memories surface would keep hint="memories"
 * forever and never cold-land on /brain. Mounting the hook here, in
 * the authed shell, keeps the hint fresh on every surface; the
 * notifications query underneath is the same one the inbox badge
 * already keeps warm, so this adds no network traffic.
 */

import { useOnboardingActivation } from "@/hooks/use-onboarding-activation";

export function LandingSurfaceSync() {
  useOnboardingActivation();
  return null;
}
