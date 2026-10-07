/**
 * Hub-segment layout — adds the `@panel` parallel slot at the hub
 * level so memory-detail navigations from ANY child route (memories
 * list, topics, topic-detail, …) intercept into the right-side
 * slide-in panel (industry 2026 pattern: Linear / Notion side peek /
 * Vercel).
 *
 * Why at this level (not at /memories/): an intercept rooted in the
 * memories segment only catches soft-navs that originate from inside
 * /h/<slug>/memories/. From /h/<slug>/topics/<id>, clicking a memory
 * card builds /h/<slug>/memories/<id> via buildMemoryDetailPath, which
 * is a sibling-segment navigation — the memory-scoped intercept
 * wouldn't fire and the user would see a full-page reload. Hoisting
 * the slot to /h/<slug>/ makes the intercept match any soft-nav to a
 * memory URL from any route under the hub.
 *
 * Structure:
 *
 *   layout.tsx                                — this file ({children}{panel})
 *   @panel/default.tsx                         — null fallback (no panel)
 *   @panel/(.)memories/[id]/page.tsx           — intercept renders panel
 *   memories/[id]/page.tsx                     — canonical full-page
 *                                                (refresh, deep link,
 *                                                share, hard nav from
 *                                                "Open full page")
 */

import type { ReactNode } from "react";

export default function HubLayout({
  children,
  panel,
}: {
  children: ReactNode;
  panel: ReactNode;
}) {
  return (
    <>
      {children}
      {panel}
    </>
  );
}
