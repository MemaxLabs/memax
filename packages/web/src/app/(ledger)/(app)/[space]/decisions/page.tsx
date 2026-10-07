import type { Metadata } from "next";
import { en } from "@/i18n/locales/en";
import { DecisionsPlace } from "../../_places/places";

export const metadata: Metadata = { title: en.ledger.app.titles.decisions };

export default function DecisionsPage() {
  return <DecisionsPlace />;
}
