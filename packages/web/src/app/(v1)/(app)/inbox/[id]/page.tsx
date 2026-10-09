/**
 * /inbox/<id> — retired deep-link route (plan 25 P4).
 *
 * The inbox is gone: its decisions now render as 等你 cards on the
 * pulse board and its receipts as the board's 最近 strip. This route
 * stays mounted purely as a redirect so old email CTAs
 * ("review the auto-merge proposal we just made") don't 404.
 *
 * The notification id is dropped rather than forwarded: the board has
 * no per-row deep-link anchor yet, and the pulse surface already shows
 * every pending decision the id could have pointed at.
 */

import { redirect } from "next/navigation";

export default function InboxDeepLinkRedirectPage() {
  redirect("/pulse");
}
