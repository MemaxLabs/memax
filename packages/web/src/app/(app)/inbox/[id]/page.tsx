"use client";

/**
 * /inbox/<id> — plan 24 §4.5 deep-link route.
 *
 * Mounts the same `<InboxView>` as /inbox but seeds the target
 * notification's id so InboxList expands + scrolls it into view on
 * first paint. Deep links from email CTAs ("review the auto-merge
 * proposal we just made") land here; if the notification has been
 * resolved or expired, InboxList silently ignores the missing id and
 * shows the regular pending list.
 *
 * Client component because InboxView's data path
 * (useNotifications + interactive resolve/dismiss) needs the
 * client-side TanStack cache.
 */

import { use } from "react";
import { InboxView } from "@/components/features/inbox/inbox-control";

export default function InboxDeepLinkPage({
  params,
}: {
  params: Promise<{ id: string }>;
}) {
  const { id } = use(params);
  return <InboxView initialExpandedId={id} />;
}
