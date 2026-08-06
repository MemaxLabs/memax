/**
 * /inbox — retired (plan 25 P4).
 *
 * The board absorbed the inbox: decisions render as 等你 cards at the
 * top of the pulse board, receipts as its 最近 strip. The route stays
 * mounted as a redirect so bookmarks and old email links keep working.
 */

import { redirect } from "next/navigation";

export default function InboxRedirectPage() {
  redirect("/pulse");
}
