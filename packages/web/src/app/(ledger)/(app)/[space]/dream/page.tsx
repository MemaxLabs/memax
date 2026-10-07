import type { Metadata } from "next";
import { en } from "@/i18n/locales/en";
import { DreamPlace } from "../../_places/dream";

export const metadata: Metadata = { title: en.ledger.app.titles.dream };

// /[space]/dream (DreamEdition.png): the space's latest edition.
export default function DreamPage() {
  return <DreamPlace editionRef="latest" />;
}
