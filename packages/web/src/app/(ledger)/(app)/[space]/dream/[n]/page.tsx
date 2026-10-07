import type { Metadata } from "next";
import { en } from "@/i18n/locales/en";
import { DreamPlace } from "../../../_places/dream";

export const metadata: Metadata = { title: en.ledger.app.titles.dream };

// /[space]/dream/214 (DreamEdition.png): one edition, by its number (or
// its ID, D-0214).
export default async function DreamEditionPage({
  params,
}: {
  params: Promise<{ n: string }>;
}) {
  const { n } = await params;
  return <DreamPlace editionRef={decodeURIComponent(n)} />;
}
