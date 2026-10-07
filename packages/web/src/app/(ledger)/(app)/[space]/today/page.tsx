import type { Metadata } from "next";
import { en } from "@/i18n/locales/en";
import { TodayPlace } from "../../_places/today";

export const metadata: Metadata = { title: en.ledger.app.titles.today };

export default function TodayPage() {
  return <TodayPlace />;
}
