"use client";

/**
 * Brain view — the /brain page (and /home alias). Phase 3.7c
 * pivoted Brain from the prior memory-count badge surface into a
 * full agent chat experience. The chat surface owns its own
 * composer; the global bar is suppressed on
 * /brain via `isChatSurfaceRoute` so there's one input, one focus
 * (option A of the chat-integration plan).
 *
 * Composing into a thin route component keeps the page-level
 * surface narrow: this file mounts the experience, the components
 * under `features/chat/` own the actual UI.
 */

import { ChatBrainView } from "@/components/features/chat/chat-brain-view";

export function BrainView() {
  return <ChatBrainView />;
}
