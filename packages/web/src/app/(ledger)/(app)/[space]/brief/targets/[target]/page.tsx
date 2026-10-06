import type { Metadata } from "next";
import { en } from "@/i18n/locales/en";
import { TargetPlace } from "../../../../_places/target";

export const metadata: Metadata = { title: en.ledger.app.titles.brief };

// /[space]/brief/targets/[target] (TargetPreview.png): one compiled file,
// by its kind ("agents-md") or its id.
export default async function TargetPage({
  params,
}: {
  params: Promise<{ target: string }>;
}) {
  const { target } = await params;
  return <TargetPlace segment={decodeURIComponent(target)} />;
}
