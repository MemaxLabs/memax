import type { Metadata } from "next";
import { en } from "@/i18n/locales/en";
import { DriftPlace } from "../../../../../_places/drift";

export const metadata: Metadata = { title: en.ledger.app.titles.brief };

// /[space]/brief/targets/[target]/drift (DriftResolve.png): a hand edit
// to a compiled file, and what should happen to it.
export default async function DriftPage({
  params,
}: {
  params: Promise<{ target: string }>;
}) {
  const { target } = await params;
  return <DriftPlace segment={decodeURIComponent(target)} />;
}
