import type { Metadata } from "next";
import { en } from "@/i18n/locales/en";
import { BriefHistoryPlace } from "../../../_places/brief-history";

export const metadata: Metadata = { title: en.ledger.app.titles.brief };

// /[space]/brief/history (BriefHistory.png): every version of the Brief,
// who changed it and why, and what changed.
export default function BriefHistoryPage() {
  return <BriefHistoryPlace />;
}
