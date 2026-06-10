"use client";

/**
 * /brain — shell-v2's renamed home surface.
 *
 * Mounts <BrainView> for activated users; redirects non-activated
 * users to /h/personal/memories so they land on the surface where
 * seed memories AND the pinned onboarding checklist live.
 *
 * Industry pattern: Linear / Notion / Cursor never drop a brand-new
 * user on an empty corpus-y surface. The chat empty state at /brain
 * promises "memax has read your notes, your team's, your agents'
 * work" — a literal lie for a 0-memory, 0-agent user. Routing them
 * to /h/personal/memories (where they see the 4 plan-23 seed
 * memories + checklist + founder note) gives them an honest
 * starting point that has something to engage with.
 *
 * Activation gate: shared with <ChatEmptyState>, derived from the
 * onboarding checklist's `five_memories` + `connect_agent` item
 * completion (server materializer = owner-wide
 * CountMemoriesByOwnerExcludingSeeds + connected_agents excl. memax).
 *
 * Why client-side and not middleware: middleware runs in the Edge
 * runtime with no DB or API access, and would need to fetch the
 * notifications list on every request. Pushing the redirect into a
 * client effect is one cheap query that piggybacks on the React
 * Query cache the inbox and sidebar already keep warm.
 *
 * Why router.replace and not router.push: we don't want the empty
 * /brain to land in browser history, otherwise the back button from
 * /h/personal/memories returns the user to the empty state.
 */

import { useEffect } from "react";
import { useRouter } from "next/navigation";
import { BrainView } from "@/components/views/brain-view";
import { useOnboardingActivation } from "@/hooks/use-onboarding-activation";

export default function BrainPage() {
  const router = useRouter();
  const { isActivated, isLoading } = useOnboardingActivation();

  useEffect(() => {
    if (isLoading) return;
    if (isActivated) return;
    router.replace("/h/personal/memories");
  }, [isActivated, isLoading, router]);

  // During the activation query and during the redirect itself we
  // render nothing. Showing a partial chat empty state and then
  // navigating away would flash promotional copy ("memax has read
  // your notes…") at a user who has none. A blank frame is the
  // honest stand-in.
  if (isLoading || !isActivated) return null;

  return <BrainView />;
}
