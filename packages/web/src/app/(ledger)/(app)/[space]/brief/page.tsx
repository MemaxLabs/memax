import type { Metadata } from "next";
import { en } from "@/i18n/locales/en";
import { BriefPlace } from "../../_places/brief";

export const metadata: Metadata = { title: en.ledger.app.titles.brief };

export default function BriefPage() {
  return <BriefPlace />;
}
