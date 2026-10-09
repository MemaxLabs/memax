import type { Metadata } from "next";
import { en } from "@/i18n/locales/en";
import { ReviewPlace } from "../../_places/review";

export const metadata: Metadata = { title: en.ledger.app.titles.review };

export default function ReviewPage() {
  return <ReviewPlace />;
}
