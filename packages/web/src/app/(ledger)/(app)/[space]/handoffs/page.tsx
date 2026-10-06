import type { Metadata } from "next";
import { en } from "@/i18n/locales/en";
import { HandoffsPlace } from "../../_places/places";

export const metadata: Metadata = { title: en.ledger.app.titles.handoffs };

export default function HandoffsPage() {
  return <HandoffsPlace />;
}
