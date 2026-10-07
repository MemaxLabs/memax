import type { Metadata } from "next";
import { en } from "@/i18n/locales/en";
import { ActivityPlace } from "../../_places/activity/activity-place";

export const metadata: Metadata = { title: en.ledger.app.titles.activity };

export default function ActivityPage() {
  return <ActivityPlace />;
}
