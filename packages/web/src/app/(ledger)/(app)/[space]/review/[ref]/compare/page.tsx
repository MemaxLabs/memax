import type { Metadata } from "next";
import { en } from "@/i18n/locales/en";
import { ComparePlace } from "../../../../_places/compare";

export const metadata: Metadata = { title: en.ledger.app.titles.review };

// /[space]/review/[ref]/compare (ReviewConflict.png): both sides of a
// conflict and one answer. Linked only from a conflict with a kept
// memory on the other side.
export default async function ComparePage({
  params,
}: {
  params: Promise<{ ref: string }>;
}) {
  const { ref } = await params;
  return <ComparePlace memoryRef={decodeURIComponent(ref)} />;
}
